package codemap

import (
	"bufio"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion    = 2
	DefaultMaxFiles  = 4000
	maxFileBytes     = 1_000_000 // skip generated dumps
	callEdgesPerFile = 24
)

var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "venv": true, ".venv": true, "env": true, ".env": true,
	"third_party": true, "thirdparty": true, "external": true,
	"__pycache__": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true,
	".tox": true, ".nox": true, "dist": true, "build": true, "target": true, "out": true, "coverage": true,
	".next": true, ".nuxt": true, ".turbo": true, ".cache": true, ".parcel-cache": true,
	"site-packages": true, ".eggs": true, "eggs": true,
	".idea": true, ".vscode": true, ".cursor": true,
	".hermes": true, ".rove": true,
	"Pods": true, "DerivedData": true,
	"bin": true, "obj": true,
}

var skipFileNames = map[string]bool{
	"GRAPH.md": true, "package-lock.json": true, "pnpm-lock.yaml": true, "yarn.lock": true,
	"Cargo.lock": true, "poetry.lock": true, "uv.lock": true, "composer.lock": true, "go.sum": true,
}

// FileRec is one indexed file. Parsed fields plus the stamp used to skip
// unchanged files on the next scan.
type FileRec struct {
	Parsed
	Size     int64    `json:"size"`
	MtimeNS  int64    `json:"mtimeNs"`
	SHA      string   `json:"sha"`
	Resolved []string `json:"resolved,omitempty"`
}

// Edge is a directed dependency. Kind is "import" (the file imports the
// target) or "call" (same-package call into a sibling file).
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type callRow struct {
	File   string `json:"file"`
	Caller string `json:"caller"`
	Line   int    `json:"line"`
}

// Index is the in-memory map of one project root. Only Files (and the scan
// metadata) are persisted; everything else is derived by rewire.
type Index struct {
	Schema    int                 `json:"schema"`
	Root      string              `json:"root"`
	UpdatedAt time.Time           `json:"updatedAt"`
	ScanMS    int64               `json:"scanMs"`
	Truncated bool                `json:"truncated"`
	GoMods    map[string]string   `json:"goMods,omitempty"` // module path → dir rel
	Files     map[string]*FileRec `json:"files"`

	edges     []Edge
	inbound   map[string][]string
	outbound  map[string][]string
	symbolIdx map[string][]string // lower(symbol) → files
	stemIdx   map[string][]string // lower(basename without ext) → files
	callIdx   map[string][]callRow
	rank      map[string]float64
	wired     bool
	checkedAt time.Time
}

// ScanStats reports what a scan did.
type ScanStats struct {
	Added     int      `json:"added"`
	Updated   int      `json:"updated"`
	Removed   int      `json:"removed"`
	Skipped   int      `json:"skipped"`
	Truncated bool     `json:"truncated"`
	Touched   []string `json:"touched,omitempty"`
	Files     int      `json:"files"`
	Edges     int      `json:"edges"`
	ScanMS    int64    `json:"scanMs"`
}

func newIndex(root string) *Index {
	return &Index{Schema: SchemaVersion, Root: root, Files: map[string]*FileRec{}, GoMods: map[string]string{}}
}

// scan walks root (or only paths, when given) and reparses files whose
// size/mtime/sha changed. A targeted scan trusts only the sha, because two
// writes inside one mtime tick with the same length would otherwise be missed.
func (ix *Index) scan(paths []string, maxFiles int) ScanStats {
	start := time.Now()
	var st ScanStats
	root := ix.Root
	type target struct{ rel, abs string }
	var targets []target
	if len(paths) > 0 {
		for _, raw := range paths {
			rel, ok := relInside(root, raw)
			if !ok {
				continue
			}
			abs := filepath.Join(root, filepath.FromSlash(rel))
			if fi, err := os.Stat(abs); err != nil || !fi.Mode().IsRegular() {
				if _, had := ix.Files[rel]; had {
					delete(ix.Files, rel)
					st.Removed++
					st.Touched = append(st.Touched, rel)
				}
				continue
			}
			if LangFor(rel) == "" {
				if path.Base(rel) == "go.mod" {
					ix.readGoMod(rel)
				}
				continue
			}
			targets = append(targets, target{rel, abs})
		}
	} else {
		ix.GoMods = map[string]string{}
		live := map[string]bool{}
		walkSource(root, maxFiles, func(rel, abs string) {
			live[rel] = true
			targets = append(targets, target{rel, abs})
		}, func(rel string) { ix.readGoMod(rel) })
		for rel := range ix.Files {
			if !live[rel] {
				delete(ix.Files, rel)
				st.Removed++
			}
		}
		st.Truncated = len(targets) >= maxFiles
		ix.Truncated = st.Truncated
	}
	for _, t := range targets {
		fi, err := os.Stat(t.abs)
		if err != nil {
			st.Skipped++
			continue
		}
		if fi.Size() > maxFileBytes {
			st.Skipped++
			continue
		}
		prev := ix.Files[t.rel]
		mt := fi.ModTime().UnixNano()
		if prev != nil && len(paths) == 0 && prev.Size == fi.Size() && prev.MtimeNS == mt {
			continue
		}
		b, err := os.ReadFile(t.abs)
		if err != nil {
			st.Skipped++
			continue
		}
		sum := sha1.Sum(b)
		digest := hex.EncodeToString(sum[:])
		if prev != nil && prev.SHA == digest {
			prev.Size, prev.MtimeNS = fi.Size(), mt
			continue
		}
		ix.Files[t.rel] = &FileRec{Parsed: ParseFile(t.rel, string(b)), Size: fi.Size(), MtimeNS: mt, SHA: digest}
		if prev != nil {
			st.Updated++
		} else {
			st.Added++
		}
		st.Touched = append(st.Touched, t.rel)
	}
	if st.Added+st.Updated+st.Removed > 0 || !ix.wired {
		ix.rewire()
	}
	ix.UpdatedAt = time.Now().UTC()
	ix.checkedAt = time.Now()
	st.ScanMS = time.Since(start).Milliseconds()
	ix.ScanMS = st.ScanMS
	st.Files = len(ix.Files)
	st.Edges = len(ix.edges)
	return st
}

func (ix *Index) readGoMod(rel string) {
	f, err := os.Open(filepath.Join(ix.Root, filepath.FromSlash(rel)))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			mod := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module")), `"`)
			dir := path.Dir(rel)
			if dir == "." {
				dir = ""
			}
			if ix.GoMods == nil {
				ix.GoMods = map[string]string{}
			}
			ix.GoMods[mod] = dir
			return
		}
	}
}

// rewire rebuilds every derived table from Files. It is cheap next to the
// parse (maps only), so incremental writes rewire in full rather than patch.
func (ix *Index) rewire() {
	ix.symbolIdx = map[string][]string{}
	ix.stemIdx = map[string][]string{}
	ix.callIdx = map[string][]callRow{}
	dirSyms := map[string]map[string][]string{} // go dir → symbol → files
	rels := ix.sortedRels()
	for _, rel := range rels {
		rec := ix.Files[rel]
		stem := strings.ToLower(trimExt(path.Base(rel)))
		ix.stemIdx[stem] = append(ix.stemIdx[stem], rel)
		seen := map[string]bool{}
		for _, s := range rec.Symbols {
			k := strings.ToLower(s.Name)
			if !seen[k] {
				seen[k] = true
				ix.symbolIdx[k] = append(ix.symbolIdx[k], rel)
			}
		}
		for _, c := range rec.Calls {
			if len(ix.callIdx[c.Name]) < 80 {
				ix.callIdx[c.Name] = append(ix.callIdx[c.Name], callRow{File: rel, Caller: c.Caller, Line: c.Line})
			}
		}
		if rec.Lang == "go" && !strings.HasSuffix(rel, "_test.go") {
			d := path.Dir(rel)
			if dirSyms[d] == nil {
				dirSyms[d] = map[string][]string{}
			}
			for _, s := range rec.Symbols {
				dirSyms[d][s.Name] = append(dirSyms[d][s.Name], rel)
			}
		}
	}
	// refs: how many other files call a symbol — lets query prefer the
	// definition everyone uses over the 400th local helper with the same name.
	for rel, rec := range ix.Files {
		for i := range rec.Symbols {
			n := 0
			for _, row := range ix.callIdx[rec.Symbols[i].Name] {
				if row.File != rel {
					n++
				}
			}
			rec.Symbols[i].Refs = n
		}
	}

	var edges []Edge
	seen := map[[2]string]bool{}
	add := func(from, to, kind string) {
		if from == to || to == "" {
			return
		}
		k := [2]string{from, to}
		if seen[k] {
			return
		}
		seen[k] = true
		edges = append(edges, Edge{From: from, To: to, Kind: kind})
	}
	for _, rel := range rels {
		rec := ix.Files[rel]
		rec.Resolved = rec.Resolved[:0]
		if rec.Lang == "go" {
			for _, dst := range ix.resolveGo(rel, rec, dirSyms) {
				rec.Resolved = append(rec.Resolved, dst)
				add(rel, dst, "import")
			}
			// same-package uses of sibling files: calls, and the types,
			// constants and variables a file names without a package prefix
			if syms := dirSyms[path.Dir(rel)]; syms != nil {
				n := 0
				link := func(name string) {
					for _, dst := range syms[name] {
						if dst != rel && n < callEdgesPerFile {
							add(rel, dst, "call")
							n++
						}
					}
				}
				for _, c := range rec.Calls {
					if !c.Local {
						link(c.Name)
					}
				}
				for _, name := range rec.GoUses {
					link(name)
				}
			}
			continue
		}
		for _, raw := range rec.Imports {
			if dst := ix.resolveOne(rel, raw, rec.Lang); dst != "" {
				rec.Resolved = append(rec.Resolved, dst)
				add(rel, dst, "import")
			}
		}
		rec.Resolved = uniq(rec.Resolved)
	}
	ix.edges = edges
	ix.inbound = map[string][]string{}
	ix.outbound = map[string][]string{}
	for _, e := range edges {
		ix.outbound[e.From] = append(ix.outbound[e.From], e.To)
		ix.inbound[e.To] = append(ix.inbound[e.To], e.From)
	}
	ix.rank = pagerank(rels, edges, nil, 20)
	ix.wired = true
}

func (ix *Index) resolveGo(rel string, rec *FileRec, dirSyms map[string]map[string][]string) []string {
	var out []string
	byAlias := map[string]string{} // alias → dir
	for alias, imp := range rec.GoAliases {
		if d, ok := ix.goImportDir(imp); ok {
			byAlias[alias] = d
		}
	}
	for _, ref := range rec.GoRefs {
		dot := strings.IndexByte(ref, '.')
		alias, name := ref[:dot], ref[dot+1:]
		d, ok := byAlias[alias]
		if !ok {
			continue
		}
		out = append(out, dirSyms[d][name]...)
	}
	return uniq(out)
}

func (ix *Index) goImportDir(imp string) (string, bool) {
	best, bestDir := "", ""
	for mod, dir := range ix.GoMods {
		if (imp == mod || strings.HasPrefix(imp, mod+"/")) && len(mod) > len(best) {
			best, bestDir = mod, dir
		}
	}
	if best == "" {
		return "", false
	}
	d := path.Join(bestDir, strings.TrimPrefix(strings.TrimPrefix(imp, best), "/"))
	if d == "" {
		d = "."
	}
	return d, true
}

var indexStems = []string{"index", "__init__", "mod", "lib", "main"}

// resolveOne maps a raw import string onto an indexed file, or "".
func (ix *Index) resolveOne(src, raw, lang string) string {
	raw = strings.TrimSpace(strings.Trim(raw, ";"))
	if raw == "" {
		return ""
	}
	for _, p := range []string{"http://", "https://", "node:", "std:", "golang.org/", "bun:"} {
		if strings.HasPrefix(raw, p) {
			return ""
		}
	}
	dir := path.Dir(src)
	switch {
	case strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || raw == "." || raw == "..":
		return ix.existing(path.Join(dir, raw))
	case lang == "py" && strings.HasPrefix(raw, "."):
		dots := len(raw) - len(strings.TrimLeft(raw, "."))
		base := dir
		for i := 1; i < dots; i++ {
			base = path.Dir(base)
		}
		rest := strings.ReplaceAll(raw[dots:], ".", "/")
		return ix.existing(path.Join(base, rest))
	case lang == "rb":
		if hit := ix.existing(path.Join(dir, raw)); hit != "" {
			return hit
		}
	case lang == "c" || lang == "cpp":
		if hit := ix.existing(path.Join(dir, raw)); hit != "" {
			return hit
		}
	}
	tail := raw
	for _, alias := range []string{"~/", "@/", "#/", "$lib/", "src/"} {
		if strings.HasPrefix(tail, alias) {
			tail = strings.TrimPrefix(tail, alias)
			break
		}
	}
	switch lang {
	case "py", "java", "kt", "cs":
		if !strings.Contains(tail, "/") {
			tail = strings.ReplaceAll(tail, ".", "/")
		}
	case "rs":
		tail = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(tail, "crate::"), "super::"), "self::")
		if i := strings.Index(tail, "::{"); i >= 0 {
			tail = tail[:i]
		}
		tail = strings.ReplaceAll(tail, "::", "/")
	case "php":
		tail = strings.ReplaceAll(tail, `\`, "/")
	}
	tail = strings.Trim(tail, "/*")
	if tail == "" {
		return ""
	}
	// Try the full tail, then drop trailing segments (Python `a.b.Class`,
	// Java `pkg.Type.method`).
	parts := strings.Split(tail, "/")
	for n := len(parts); n >= 1 && n >= len(parts)-2; n-- {
		if hit := ix.bySuffix(src, strings.Join(parts[:n], "/")); hit != "" {
			return hit
		}
	}
	return ""
}

// existing resolves a root-relative stem to an indexed file, trying the
// usual extensions and index files.
func (ix *Index) existing(rel string) string {
	rel = strings.TrimPrefix(path.Clean(rel), "./")
	if rel == "." || rel == "" || strings.HasPrefix(rel, "../") {
		return ""
	}
	if _, ok := ix.Files[rel]; ok {
		return rel
	}
	return ix.bySuffixExact(rel)
}

func (ix *Index) bySuffixExact(stemPath string) string {
	base := strings.ToLower(path.Base(stemPath))
	for _, rel := range ix.stemIdx[base] {
		if trimExt(rel) == stemPath {
			return rel
		}
	}
	for _, s := range indexStems {
		for _, rel := range ix.stemIdx[s] {
			if trimExt(rel) == stemPath+"/"+s {
				return rel
			}
		}
	}
	return ""
}

// bySuffix finds a file whose path (without extension) ends with tail. Ties
// go to the candidate sharing the longest directory prefix with src.
func (ix *Index) bySuffix(src, tail string) string {
	base := strings.ToLower(path.Base(tail))
	var cands []string
	match := func(rel, want string) bool {
		t := trimExt(rel)
		return t == want || strings.HasSuffix(t, "/"+want)
	}
	for _, rel := range ix.stemIdx[base] {
		if match(rel, tail) {
			cands = append(cands, rel)
		}
	}
	if len(cands) == 0 {
		for _, s := range indexStems {
			for _, rel := range ix.stemIdx[s] {
				if match(rel, tail+"/"+s) {
					cands = append(cands, rel)
				}
			}
		}
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool {
		ci, cj := commonPrefix(src, cands[i]), commonPrefix(src, cands[j])
		if ci != cj {
			return ci > cj
		}
		if len(cands[i]) != len(cands[j]) {
			return len(cands[i]) < len(cands[j])
		}
		return cands[i] < cands[j]
	})
	return cands[0]
}

func (ix *Index) sortedRels() []string {
	rels := make([]string, 0, len(ix.Files))
	for rel := range ix.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return rels
}

// pagerank is a stdlib power iteration over edges. With focus it is
// personalized (Aider's trick: chat files ×50); without, it is global.
func pagerank(nodes []string, edges []Edge, focus map[string]bool, iters int) map[string]float64 {
	if len(nodes) == 0 {
		return map[string]float64{}
	}
	out := map[string][]Edge{}
	for _, e := range edges {
		out[e.From] = append(out[e.From], e)
	}
	personal := map[string]float64{}
	if len(focus) > 0 {
		for n := range focus {
			personal[n] = 1 / float64(len(focus))
		}
	} else {
		for _, n := range nodes {
			personal[n] = 1 / float64(len(nodes))
		}
	}
	rank := map[string]float64{}
	for k, v := range personal {
		rank[k] = v
	}
	const d = 0.85
	for it := 0; it < iters; it++ {
		next := make(map[string]float64, len(nodes))
		dangling := 0.0
		for _, n := range nodes {
			if len(out[n]) == 0 {
				dangling += rank[n]
			}
		}
		for _, n := range nodes {
			next[n] = (1-d)*personal[n] + d*dangling*personal[n]
		}
		for src, es := range out {
			total := 0.0
			for _, e := range es {
				total += edgeWeight(e, focus)
			}
			if total == 0 {
				continue
			}
			share := d * rank[src] / total
			for _, e := range es {
				next[e.To] += share * edgeWeight(e, focus)
			}
		}
		rank = next
	}
	return rank
}

func edgeWeight(e Edge, focus map[string]bool) float64 {
	w := 1.0
	if e.Kind == "call" {
		w = 0.5
	}
	if focus[e.From] {
		w *= 50
	}
	return w
}

// walkSource yields indexable files under root, skipping dependency and build
// directories and dotfiles. go.mod files go to onMod so Go imports resolve.
func walkSource(root string, maxFiles int, onFile func(rel, abs string), onMod func(rel string)) {
	count := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		name := d.Name()
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if skipDirs[name] || strings.HasPrefix(name, ".") || isDepDir(path.Dir(rel), name) {
				return fs.SkipDir
			}
			return nil
		}
		if name == "go.mod" {
			onMod(rel)
			return nil
		}
		if skipFileNames[name] || strings.HasPrefix(name, ".") || LangFor(name) == "" {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if count >= maxFiles {
			return fs.SkipAll
		}
		count++
		onFile(rel, p)
		return nil
	})
}

func isDepDir(parent, name string) bool {
	if name == "mod" && strings.HasSuffix(parent, "pkg") {
		return true
	}
	if name == "pkg" && strings.HasSuffix(parent, "go") {
		return true
	}
	return false
}

func relInside(root, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	abs := raw
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		// root is symlink-resolved (/var → /private/var); resolve the path
		// too, via its parent when the file itself was just deleted.
		real, rerr := filepath.EvalSymlinks(abs)
		if rerr != nil {
			if dir, derr := filepath.EvalSymlinks(filepath.Dir(abs)); derr == nil {
				real, rerr = filepath.Join(dir, filepath.Base(abs)), nil
			}
		}
		if rerr != nil {
			return "", false
		}
		rel, err = filepath.Rel(root, real)
	}
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func trimExt(rel string) string {
	if e := path.Ext(rel); e != "" {
		return rel[:len(rel)-len(e)]
	}
	return rel
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// ── persistence ─────────────────────────────────────────────────────────────

func loadIndexFile(file string) (*Index, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var ix Index
	if err := json.NewDecoder(zr).Decode(&ix); err != nil {
		return nil, err
	}
	if ix.Files == nil {
		ix.Files = map[string]*FileRec{}
	}
	return &ix, nil
}

func saveIndexFile(file string, ix *Index) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if err := json.NewEncoder(zw).Encode(ix); err != nil {
		zw.Close()
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
