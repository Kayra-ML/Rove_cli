package codemap

import (
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
)

type Hit struct {
	File    string   `json:"file"`
	Lang    string   `json:"lang"`
	LOC     int      `json:"loc"`
	Score   float64  `json:"score"`
	Why     []string `json:"why,omitempty"`
	Symbols []string `json:"symbols,omitempty"`
	Imports []string `json:"imports,omitempty"`
}

type QueryResult struct {
	Query string `json:"query"`
	Count int    `json:"count"`
	Hits  []Hit  `json:"hits"`
}

// Query ranks files by path and symbol match. kind is "all", "file" or
// "symbol". focus personalizes ranking toward files already in play.
func (ix *Index) Query(q, kind string, limit int, focus []string) QueryResult {
	needle := strings.ToLower(strings.TrimSpace(q))
	res := QueryResult{Query: q, Hits: []Hit{}}
	if needle == "" {
		return res
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if kind == "" {
		kind = "all"
	}
	wantFile := kind == "all" || kind == "file" || kind == "path"
	wantSym := kind == "all" || kind == "symbol" || kind == "fn" || kind == "class" || kind == "type"

	var cands []string
	seen := map[string]bool{}
	add := func(rel string) {
		if !seen[rel] {
			if _, ok := ix.Files[rel]; ok {
				seen[rel] = true
				cands = append(cands, rel)
			}
		}
	}
	if wantSym {
		for _, rel := range ix.symbolPostings(needle, 48) {
			add(rel)
		}
	}
	if wantFile {
		if rows, ok := ix.stemIdx[needle]; ok {
			for _, rel := range rows {
				add(rel)
			}
		} else if len(needle) >= 3 {
			for _, rel := range ix.sortedRels() {
				if strings.Contains(strings.ToLower(rel), needle) {
					add(rel)
				}
				if len(cands) > 200 {
					break
				}
			}
		}
	}
	for _, rel := range cands {
		rec := ix.Files[rel]
		score, why := scoreFile(rel, rec, needle, wantFile, wantSym)
		if score == 0 {
			continue
		}
		h := Hit{File: rel, Lang: rec.Lang, LOC: rec.LOC, Score: score, Why: why}
		for i, s := range rec.Symbols {
			if i >= 12 {
				break
			}
			h.Symbols = append(h.Symbols, s.Name)
		}
		h.Imports = headStrings(rec.Resolved, 12)
		res.Hits = append(res.Hits, h)
	}
	res.Count = len(res.Hits)
	sortHits(res.Hits)
	if len(res.Hits) > limit {
		res.Hits = res.Hits[:limit]
	}
	if len(focus) > 0 && len(res.Hits) > 0 {
		fs := map[string]bool{}
		for _, f := range focus {
			if rel := ix.ResolveTarget(f); rel != "" {
				fs[rel] = true
			}
		}
		if len(fs) > 0 {
			pr := pagerank(ix.sortedRels(), ix.edges, fs, 12)
			for i := range res.Hits {
				if v := pr[res.Hits[i].File]; v > 0 {
					res.Hits[i].Score = float64(int((res.Hits[i].Score+v*1000)*1000)) / 1000
					res.Hits[i].Why = uniq(append(res.Hits[i].Why, "pagerank"))
				}
			}
			sortHits(res.Hits)
		}
	}
	return res
}

func sortHits(h []Hit) {
	sort.SliceStable(h, func(i, j int) bool {
		if h[i].Score != h[j].Score {
			return h[i].Score > h[j].Score
		}
		return h[i].File < h[j].File
	})
}

// symbolPostings caps a name defined in thousands of files: callers first,
// then a stable sample, so `get` cannot drown the result.
func (ix *Index) symbolPostings(needle string, limit int) []string {
	exact := ix.symbolIdx[needle]
	if len(exact) > limit {
		hot := map[string]int{}
		for name, rows := range ix.callIdx {
			if strings.ToLower(name) != needle {
				continue
			}
			for _, r := range rows {
				hot[r.File]++
			}
		}
		have := map[string]bool{}
		for _, r := range exact {
			have[r] = true
		}
		var ranked []string
		for f := range hot {
			if have[f] {
				ranked = append(ranked, f)
			}
		}
		sort.Slice(ranked, func(i, j int) bool {
			if hot[ranked[i]] != hot[ranked[j]] {
				return hot[ranked[i]] > hot[ranked[j]]
			}
			return ranked[i] < ranked[j]
		})
		out := headStrings(ranked, limit)
		if len(out) < limit {
			step := len(exact) / (limit - len(out))
			if step < 1 {
				step = 1
			}
			in := map[string]bool{}
			for _, r := range out {
				in[r] = true
			}
			for i := 0; i < len(exact) && len(out) < limit; i += step {
				if !in[exact[i]] {
					out = append(out, exact[i])
				}
			}
		}
		return out
	}
	if len(exact) > 0 || len(needle) < 4 {
		return exact
	}
	var hits []string
	seen := map[string]bool{}
	names := make([]string, 0, len(ix.symbolIdx))
	for n := range ix.symbolIdx {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.Contains(name, needle) {
			continue
		}
		for _, rel := range ix.symbolIdx[name] {
			if !seen[rel] {
				seen[rel] = true
				hits = append(hits, rel)
			}
		}
		if len(hits) >= limit {
			break
		}
	}
	return hits
}

func scoreFile(rel string, rec *FileRec, needle string, wantFile, wantSym bool) (float64, []string) {
	score := 0.0
	var why []string
	low := strings.ToLower(rel)
	if wantFile && strings.Contains(low, needle) {
		if strings.HasSuffix(low, needle) || strings.ToLower(trimExt(path.Base(rel))) == needle {
			score += 8
		} else {
			score += 4
		}
		why = append(why, "path")
	}
	if wantSym {
		for _, s := range rec.Symbols {
			sl := strings.ToLower(s.Name)
			switch {
			case sl == needle:
				bump := 8 + min(len(s.Name), 48)
				if strings.HasPrefix(s.Name, "_") {
					bump = max(4, bump/3)
				}
				bump += min(24, s.Refs)
				score += float64(bump)
				why = append(why, "symbol:"+s.Name)
			case len(needle) >= 4 && strings.Contains(sl, needle):
				score += float64(4 + min(len(needle), 12)/2)
				why = append(why, "symbol:"+s.Name)
			}
		}
		for _, e := range rec.Exports {
			if strings.ToLower(e) == needle {
				score += 6
				why = append(why, "export:"+e)
			}
		}
	}
	why = uniq(why)
	if len(why) > 8 {
		why = why[:8]
	}
	return score, why
}

type CallerRef struct {
	Symbol string `json:"symbol"`
	File   string `json:"file"`
	Caller string `json:"caller"`
	Line   int    `json:"line"`
}

type NeighborsResult struct {
	File       string      `json:"file"`
	Lang       string      `json:"lang"`
	LOC        int         `json:"loc"`
	Symbols    []Symbol    `json:"symbols"`
	Exports    []string    `json:"exports,omitempty"`
	ImportedBy []string    `json:"importedBy,omitempty"`
	Imports    []string    `json:"imports,omitempty"`
	CalledBy   []CallerRef `json:"calledBy,omitempty"`
}

// Neighbors returns who depends on target and what it depends on.
func (ix *Index) Neighbors(target, direction string, limit int) (NeighborsResult, bool) {
	rel := ix.ResolveTarget(target)
	if rel == "" {
		return NeighborsResult{File: target}, false
	}
	if limit <= 0 {
		limit = 40
	}
	rec := ix.Files[rel]
	out := NeighborsResult{File: rel, Lang: rec.Lang, LOC: rec.LOC, Symbols: rec.Symbols, Exports: rec.Exports}
	if direction == "" {
		direction = "both"
	}
	if direction == "in" || direction == "both" {
		out.ImportedBy = headStrings(ix.inbound[rel], limit)
		out.CalledBy = ix.callersOf(rel, limit)
	}
	if direction == "out" || direction == "both" {
		out.Imports = headStrings(ix.outbound[rel], limit)
	}
	return out, true
}

func (ix *Index) callersOf(rel string, limit int) []CallerRef {
	rec := ix.Files[rel]
	if rec == nil {
		return nil
	}
	var out []CallerRef
	seen := map[[3]string]bool{}
	fam := langFamily(rec.Lang)
	for _, s := range rec.Symbols {
		for _, row := range ix.callIdx[s.Name] {
			if row.File == rel {
				continue
			}
			// Calls resolve by name only; a TS method named like a Go func is
			// not a caller.
			if other := ix.Files[row.File]; other == nil || langFamily(other.Lang) != fam {
				continue
			}
			// An unqualified Go call can only reach its own package;
			// cross-package calls are pkg.Name and show up as imports.
			if fam == "go" && path.Dir(row.File) != path.Dir(rel) {
				continue
			}
			k := [3]string{row.File, row.Caller, s.Name}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, CallerRef{Symbol: s.Name, File: row.File, Caller: row.Caller, Line: row.Line})
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// Path is the shortest undirected dependency path between two files.
func (ix *Index) Path(src, dst string) ([]string, bool) {
	a, b := ix.ResolveTarget(src), ix.ResolveTarget(dst)
	if a == "" || b == "" {
		return nil, false
	}
	if a == b {
		return []string{a}, true
	}
	adj := map[string][]string{}
	for _, e := range ix.edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	prev := map[string]string{a: ""}
	q := []string{a}
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		for _, nx := range adj[n] {
			if _, ok := prev[nx]; ok {
				continue
			}
			prev[nx] = n
			if nx == b {
				var trail []string
				for c := b; c != ""; c = prev[c] {
					trail = append([]string{c}, trail...)
				}
				return trail, true
			}
			q = append(q, nx)
		}
	}
	return nil, false
}

// Impact returns files that transitively depend on any of rels, up to depth
// hops, nearest first. This is "what else does this change touch".
func (ix *Index) Impact(rels []string, depth int) []string {
	if depth <= 0 {
		depth = 2
	}
	seen := map[string]bool{}
	var frontier []string
	for _, r := range rels {
		if rel := ix.ResolveTarget(r); rel != "" && !seen[rel] {
			seen[rel] = true
			frontier = append(frontier, rel)
		}
	}
	var out []string
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []string
		for _, n := range frontier {
			for _, up := range ix.inbound[n] {
				if !seen[up] {
					seen[up] = true
					out = append(out, up)
					next = append(next, up)
				}
			}
		}
		frontier = next
	}
	return out
}

type ChangedRow struct {
	File       string      `json:"file"`
	InGraph    bool        `json:"inGraph"`
	Lang       string      `json:"lang,omitempty"`
	ImportedBy []string    `json:"importedBy,omitempty"`
	Imports    []string    `json:"imports,omitempty"`
	CalledBy   []CallerRef `json:"calledBy,omitempty"`
}

// Changed intersects `git diff --name-only base` plus untracked files with
// the graph. Fails open (empty) when git is missing.
func (ix *Index) Changed(base string, limit int) []ChangedRow {
	if base == "" {
		base = "HEAD"
	}
	if limit <= 0 {
		limit = 40
	}
	var names []string
	for _, args := range [][]string{
		{"diff", "--name-only", base},
		{"ls-files", "--others", "--exclude-standard"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = ix.Root
		b, err := cmd.Output()
		if err != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n") {
			if ln = strings.TrimSpace(ln); ln != "" {
				names = append(names, ln)
			}
		}
	}
	var out []ChangedRow
	for _, rel := range uniq(names) {
		rec, ok := ix.Files[rel]
		row := ChangedRow{File: rel, InGraph: ok}
		if ok {
			row.Lang = rec.Lang
			row.Imports = headStrings(rec.Resolved, 12)
		}
		row.ImportedBy = headStrings(ix.inbound[rel], limit)
		row.CalledBy = ix.callersOf(rel, limit)
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out
}

type GraphNode struct {
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	Dir     string  `json:"dir"`
	Group   string  `json:"group"`
	Lang    string  `json:"lang"`
	LOC     int     `json:"loc"`
	Rank    float64 `json:"rank"`
	In      int     `json:"in"`
	Out     int     `json:"out"`
	Symbols int     `json:"symbols"`
}

type Graph struct {
	Root      string      `json:"root"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []Edge      `json:"edges"`
	Total     int         `json:"total"`
	TotalEdge int         `json:"totalEdges"`
	Truncated bool        `json:"truncated"`
}

// Graph exports the top `limit` files by rank (plus degree) and the edges
// among them, for rendering. dir scopes to a subtree.
func (ix *Index) Graph(limit int, dir string) Graph {
	if limit <= 0 {
		limit = 400
	}
	dir = strings.Trim(dir, "/")
	var rels []string
	for _, rel := range ix.sortedRels() {
		if dir == "" || rel == dir || strings.HasPrefix(rel, dir+"/") {
			rels = append(rels, rel)
		}
	}
	score := func(r string) float64 {
		return ix.rank[r]*1000 + float64(len(ix.inbound[r])+len(ix.outbound[r]))*0.05
	}
	sort.SliceStable(rels, func(i, j int) bool { return score(rels[i]) > score(rels[j]) })
	g := Graph{Root: ix.Root, Total: len(rels), TotalEdge: len(ix.edges), Truncated: len(rels) > limit || ix.Truncated}
	if len(rels) > limit {
		rels = rels[:limit]
	}
	in := map[string]bool{}
	for _, rel := range rels {
		in[rel] = true
		rec := ix.Files[rel]
		d := path.Dir(rel)
		if d == "." {
			d = ""
		}
		g.Nodes = append(g.Nodes, GraphNode{
			ID: rel, Label: path.Base(rel), Dir: d, Group: groupOf(rel, dir), Lang: rec.Lang, LOC: rec.LOC,
			Rank: ix.rank[rel], In: len(ix.inbound[rel]), Out: len(ix.outbound[rel]), Symbols: len(rec.Symbols),
		})
	}
	for _, e := range ix.edges {
		if in[e.From] && in[e.To] {
			g.Edges = append(g.Edges, e)
		}
	}
	if g.Nodes == nil {
		g.Nodes = []GraphNode{}
	}
	if g.Edges == nil {
		g.Edges = []Edge{}
	}
	return g
}

// groupOf is the cluster label used for coloring: the last two directory
// segments under scope ("internal/agent", "src/app"), "." at the top.
func groupOf(rel, scope string) string {
	r := strings.TrimPrefix(strings.TrimPrefix(rel, scope), "/")
	d := path.Dir(r)
	if d == "." {
		return "."
	}
	parts := strings.Split(d, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

// ResolveTarget accepts a path, a path suffix or a symbol name.
func (ix *Index) ResolveTarget(target string) string {
	t := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(target), `\`, "/"), "./")
	if t == "" {
		return ""
	}
	if _, ok := ix.Files[t]; ok {
		return t
	}
	if rel, ok := relInside(ix.Root, t); ok {
		if _, ok := ix.Files[rel]; ok {
			return rel
		}
	}
	var hits []string
	for rel := range ix.Files {
		if strings.HasSuffix(rel, "/"+t) {
			hits = append(hits, rel)
		}
	}
	if len(hits) > 0 {
		sort.Slice(hits, func(i, j int) bool {
			if len(hits[i]) != len(hits[j]) {
				return len(hits[i]) < len(hits[j])
			}
			return hits[i] < hits[j]
		})
		return hits[0]
	}
	if rows := ix.symbolIdx[strings.ToLower(t)]; len(rows) > 0 {
		return rows[0]
	}
	return ""
}

// Brief is a short text block describing files, for handing a slice of the
// map to an agent or another session.
func (ix *Index) Brief(rels []string, maxFiles int) string {
	if maxFiles <= 0 {
		maxFiles = 12
	}
	var b strings.Builder
	n := 0
	for _, r := range rels {
		rel := ix.ResolveTarget(r)
		if rel == "" {
			continue
		}
		rec := ix.Files[rel]
		// one line per file: renders as a flat list and costs fewer tokens
		b.WriteString("- `" + rel + "`")
		if rec.Lang != "" {
			b.WriteString(" (" + rec.Lang + ", " + strconv.Itoa(rec.LOC) + " lines)")
		}
		var syms []string
		for i, s := range rec.Symbols {
			if i >= 8 {
				break
			}
			syms = append(syms, s.Name)
		}
		if len(syms) > 0 {
			b.WriteString(" — defines: " + strings.Join(syms, ", "))
		}
		if deps := headStrings(ix.outbound[rel], 6); len(deps) > 0 {
			b.WriteString("; uses: " + strings.Join(deps, ", "))
		}
		if ups := headStrings(ix.inbound[rel], 6); len(ups) > 0 {
			b.WriteString("; used by: " + strings.Join(ups, ", "))
		}
		b.WriteString("\n")
		n++
		if n >= maxFiles {
			break
		}
	}
	return b.String()
}

func langFamily(lang string) string {
	switch lang {
	case "js", "ts":
		return "js"
	case "c", "cpp":
		return "c"
	case "java", "kt":
		return "jvm"
	}
	return lang
}

func headStrings(in []string, n int) []string {
	if len(in) <= n {
		return append([]string(nil), in...)
	}
	return append([]string(nil), in[:n]...)
}
