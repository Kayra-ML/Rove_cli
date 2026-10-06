package ctxmap

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Kayra-ML/rove/internal/codemap"
)

// Finder decides, without calling a model, which files in root are likely
// counterparts of a change. No result means the far agent is not run.
type Finder interface {
	Find(ctx context.Context, root string, files []string, diff string) []string
}

// DefaultFinder matches in three cheap passes: changed file names against
// file names in the far workspace (catches assets like logo.svg), then
// distinctive identifiers and literals from the diff via `git grep`, then —
// for non-git workspaces — exact symbol hits in the project map.
type DefaultFinder struct {
	Map      *codemap.Service
	MaxWalk  int
	MaxMatch int
}

// genericStems are file names too common to mean "same thing" on their own.
var genericStems = map[string]bool{
	"index": true, "main": true, "mod": true, "lib": true, "app": true, "utils": true, "util": true,
	"helpers": true, "types": true, "config": true, "constants": true, "styles": true, "style": true,
	"readme": true, "test": true, "tests": true, "init": true, "__init__": true, "package": true,
	"server": true, "client": true, "api": true, "common": true, "base": true, "core": true,
}

var walkSkip = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	"out": true, "coverage": true, ".next": true, ".venv": true, "venv": true, "__pycache__": true,
	"Pods": true, "DerivedData": true, "bin": true, "obj": true, ".cache": true,
}

func (f DefaultFinder) Find(ctx context.Context, root string, files []string, diff string) []string {
	if root == "" {
		return nil
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil
	}
	maxMatch := f.MaxMatch
	if maxMatch <= 0 {
		maxMatch = 12
	}
	var out []string
	seen := map[string]bool{}
	add := func(rel string) {
		if rel != "" && !seen[rel] && len(out) < maxMatch {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	for _, rel := range f.byName(root, stems(files)) {
		add(rel)
	}
	toks := Signals(diff, 10)
	if len(toks) > 0 && len(out) < maxMatch {
		if hits, ok := gitGrep(ctx, root, toks); ok {
			for _, rel := range hits {
				add(rel)
			}
		} else if f.Map != nil {
			_ = f.Map.With(root, func(ix *codemap.Index) error {
				for _, t := range toks {
					for _, h := range ix.Query(t, "symbol", 3, nil).Hits {
						for _, why := range h.Why {
							if strings.EqualFold(why, "symbol:"+t) {
								add(h.File)
							}
						}
					}
				}
				return nil
			})
		}
	}
	return out
}

// byName walks root (bounded) for files whose stem equals or contains one of
// the changed files' stems.
func (f DefaultFinder) byName(root string, want []string) []string {
	if len(want) == 0 {
		return nil
	}
	limit := f.MaxWalk
	if limit <= 0 {
		limit = 20000
	}
	var hits []string
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (walkSkip[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		n++
		if n > limit {
			return fs.SkipAll
		}
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		for _, w := range want {
			if stem == w || (len(w) >= 4 && strings.Contains(stem, w)) {
				rel, _ := filepath.Rel(root, p)
				hits = append(hits, filepath.ToSlash(rel))
				break
			}
		}
		return nil
	})
	sort.Strings(hits)
	return hits
}

func stems(files []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range files {
		base := filepath.Base(f)
		stem := strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))
		stem = strings.TrimSuffix(strings.TrimSuffix(stem, ".test"), ".spec")
		if len(stem) < 3 || genericStems[stem] || seen[stem] {
			continue
		}
		seen[stem] = true
		out = append(out, stem)
	}
	return out
}

var (
	identRx   = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{4,}`)
	literalRx = regexp.MustCompile(`"([^"\n]{4,60})"|'([^'\n]{4,60})'`)
	hexRx     = regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)
)

var commonWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`function return const import export default string number boolean
		class interface extends implements public private protected static async await false true
		undefined null console className props state value width height color style display
		children onClick handler render component module require package struct return error
		context should expect errors format println printf result params options config
		button input label title description content message response request status
		length append delete update create first index items`) {
		commonWords[strings.ToLower(w)] = true
	}
}

// Signals picks distinctive tokens from a diff's +/- lines: identifiers
// with case changes, digits or underscores, quoted literals and hex colors.
// Plain English words and keywords are dropped; longer tokens rank first.
func Signals(diff string, max int) []string {
	type cand struct {
		s     string
		score int
	}
	seen := map[string]bool{}
	var cands []cand
	push := func(s string, score int) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] || commonWords[strings.ToLower(s)] {
			return
		}
		seen[s] = true
		cands = append(cands, cand{s, score})
	}
	sc := bufio.NewScanner(strings.NewReader(diff))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		ln := sc.Text()
		if len(ln) < 2 || (ln[0] != '+' && ln[0] != '-') || strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---") {
			continue
		}
		body := ln[1:]
		for _, m := range hexRx.FindAllString(body, -1) {
			push(strings.ToLower(m), 60)
		}
		for _, m := range literalRx.FindAllStringSubmatch(body, -1) {
			lit := m[1]
			if lit == "" {
				lit = m[2]
			}
			if strings.ContainsAny(lit, "{}$`\\") {
				continue
			}
			push(lit, 40+len(lit))
		}
		for _, m := range identRx.FindAllString(body, -1) {
			score := len(m)
			if strings.ContainsAny(m, "_0123456789") || hasInnerUpper(m) {
				score += 20
			} else {
				continue // plain lowercase words are too common to mean anything
			}
			push(m, score)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	var out []string
	for _, c := range cands {
		if len(out) >= max {
			break
		}
		out = append(out, c.s)
	}
	return out
}

func hasInnerUpper(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}

// gitGrep lists files in root containing any token. ok is false when root is
// not a git work tree (the caller falls back to the project map).
func gitGrep(ctx context.Context, root string, toks []string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	check := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	check.Dir = root
	if out, err := check.Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil, false
	}
	args := []string{"grep", "-l", "-I", "-F", "--untracked"}
	for _, t := range toks {
		args = append(args, "-e", t)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	out, _ := cmd.Output() // exit 1 means no match
	var hits []string
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			hits = append(hits, ln)
		}
		if len(hits) >= 20 {
			break
		}
	}
	return hits, true
}

// sourceDiff is the full diff of files against HEAD in root. Untracked files
// (new ones) contribute their first lines as additions so their names and
// literals still count as signals.
func sourceDiff(ctx context.Context, root string, files []string) string {
	if root == "" || len(files) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := append([]string{"diff", "--no-color", "-U0", "HEAD", "--"}, files...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	out, _ := cmd.Output()
	diff := string(out)
	var b strings.Builder
	b.WriteString(diff)
	for _, f := range files {
		if strings.Contains(diff, " b/"+f) {
			continue
		}
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, f)
		}
		fi, err := os.Stat(p)
		if err != nil || fi.Size() > 200_000 {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil || isBinary(data) {
			continue
		}
		b.WriteString("diff --git a/" + f + " b/" + f + "\n")
		for i, ln := range strings.SplitN(string(data), "\n", 61) {
			if i >= 60 {
				break
			}
			b.WriteString("+" + ln + "\n")
		}
	}
	return b.String()
}

// compactDiff keeps only file headers and changed lines, within max runes.
func compactDiff(diff string, max int) string {
	var b strings.Builder
	used := 0
	for _, ln := range strings.Split(diff, "\n") {
		var keep string
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			if i := strings.LastIndex(ln, " b/"); i >= 0 {
				keep = "# " + ln[i+3:]
			}
		case strings.HasPrefix(ln, "+++"), strings.HasPrefix(ln, "---"), strings.HasPrefix(ln, "@@"), strings.HasPrefix(ln, "index "):
			continue
		case strings.HasPrefix(ln, "+"), strings.HasPrefix(ln, "-"):
			if strings.TrimSpace(ln[1:]) == "" {
				continue
			}
			keep = clip(ln, 160)
		default:
			continue
		}
		n := len([]rune(keep)) + 1
		if used+n > max {
			b.WriteString("… (kısaltıldı)")
			break
		}
		b.WriteString(keep + "\n")
		used += n
	}
	return strings.TrimRight(b.String(), "\n")
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}
