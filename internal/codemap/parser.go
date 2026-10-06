// Package codemap is the Go port of CodeMap: a small, incremental map of a
// repository (files, symbols, imports, calls) that agents query instead of
// walking the tree, and that the desktop renders as a graph.
//
// Stdlib only. No tree-sitter, no LSP, no embeddings — regex per language,
// PageRank for ranking.
package codemap

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var langByExt = map[string]string{
	".py": "py", ".pyi": "py",
	".js": "js", ".mjs": "js", ".cjs": "js", ".jsx": "js", ".vue": "js", ".svelte": "js",
	".ts": "ts", ".tsx": "ts",
	".go":   "go",
	".rs":   "rs",
	".java": "java", ".kt": "kt", ".kts": "kt",
	".c": "c", ".h": "c",
	".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp",
	".cs":    "cs",
	".rb":    "rb",
	".php":   "php",
	".swift": "swift",
}

// LangFor returns the language tag for a file name, or "" when unsupported.
func LangFor(name string) string {
	return langByExt[strings.ToLower(path.Ext(name))]
}

type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Line int    `json:"line"`
	Sig  string `json:"sig,omitempty"`
	Refs int    `json:"refs,omitempty"`
}

type Call struct {
	Caller string `json:"caller"`
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Local  bool   `json:"local,omitempty"`
}

// Parsed is what a single file contributes before cross-file resolution.
type Parsed struct {
	Lang    string   `json:"lang"`
	LOC     int      `json:"loc"`
	Imports []string `json:"imports,omitempty"`
	Symbols []Symbol `json:"symbols,omitempty"`
	Exports []string `json:"exports,omitempty"`
	Calls   []Call   `json:"calls,omitempty"`
	// Go only: import alias → import path, and `alias.Name` selector refs.
	GoAliases map[string]string `json:"goAliases,omitempty"`
	GoRefs    []string          `json:"goRefs,omitempty"`
	GoPkg     string            `json:"goPkg,omitempty"`
	// Go only: names this file uses without a package prefix and does not
	// define itself — what it may take from sibling files of its package.
	GoUses []string `json:"goUses,omitempty"`
}

const (
	maxSymbols = 400
	maxExports = 200
	maxCalls   = 800
)

var (
	pyFrom   = regexp.MustCompile(`(?m)^\s*from\s+(\.*[\w.]*)\s+import\s+(.+)$`)
	pyImport = regexp.MustCompile(`(?m)^\s*import\s+([\w.,\s]+)$`)
	pyDef    = regexp.MustCompile(`(?m)^([ \t]*)(?:async\s+)?def\s+(\w+)\s*\(`)
	pyClass  = regexp.MustCompile(`(?m)^([ \t]*)class\s+(\w+)\s*[:(]`)

	jsFrom      = regexp.MustCompile(`(?:import|export)\s+(?:type\s+)?(?:[\w*\s{},]+)\s+from\s+['"]([^'"]+)['"]`)
	jsSideEff   = regexp.MustCompile(`(?m)^\s*import\s+['"]([^'"]+)['"]`)
	jsRequire   = regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]\s*\)`)
	jsDynImport = regexp.MustCompile(`import\(\s*['"]([^'"]+)['"]\s*\)`)
	jsExportFn  = regexp.MustCompile(`(?m)^\s*export\s+(?:async\s+)?(?:default\s+)?(?:async\s+)?function\s+(\w+)`)
	jsExportCls = regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?class\s+(\w+)`)
	jsExportVar = regexp.MustCompile(`(?m)^\s*export\s+(?:const|let|var|type|interface|enum)\s+(\w+)`)
	jsFn        = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[(<]`)
	jsClass     = regexp.MustCompile(`(?m)^\s*(?:export\s+)?class\s+(\w+)\b`)
	jsConstFn   = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)\s*(?::[^=]+)?=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z_]\w*)\s*(?::[^=]+)?=>`)

	goImportBlock = regexp.MustCompile(`(?s)import\s*\((.*?)\)`)
	goImportOne   = regexp.MustCompile(`(?m)^import\s+([\w.]+\s+)?"([^"]+)"`)
	goImportLine  = regexp.MustCompile(`^\s*([\w.]+\s+)?"([^"]+)"`)
	goPackage     = regexp.MustCompile(`(?m)^package\s+(\w+)`)
	goFunc        = regexp.MustCompile(`(?m)^func\s+(?:\([^)]+\)\s+)?(\w+)\s*[(\[]`)
	goType        = regexp.MustCompile(`(?m)^type\s+(\w+)\s+`)
	goTypeBlock   = regexp.MustCompile(`(?ms)^type\s*\((.*?)\n\)`)
	goValue       = regexp.MustCompile(`(?m)^(const|var)\s+(\w+)`)
	goValueBlock  = regexp.MustCompile(`(?ms)^(const|var)\s*\((.*?)\n\)`)
	goIdent       = regexp.MustCompile(`[A-Za-z_]\w*`)
	goSelector    = regexp.MustCompile(`\b([a-z_]\w*)\.([A-Z]\w*)`)

	rsUse  = regexp.MustCompile(`(?m)^\s*use\s+([^;]+);`)
	rsFn   = regexp.MustCompile(`(?m)^\s*(?:pub(?:\([^)]+\))?\s+)?(?:async\s+)?fn\s+(\w+)\s*[<(]`)
	rsType = regexp.MustCompile(`(?m)^\s*(?:pub(?:\([^)]+\))?\s+)?(?:struct|enum|trait|type|union)\s+(\w+)`)

	javaPkg    = regexp.MustCompile(`(?m)^\s*package\s+([\w.]+)\s*;`)
	javaImport = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.*]+)\s*;`)
	javaType   = regexp.MustCompile(`(?m)^\s*(?:public|private|protected|abstract|final|static|data|sealed|open|internal|\s)*(?:class|interface|enum|record|object)\s+(\w+)`)
	javaFn     = regexp.MustCompile(`(?m)^\s*(?:public|private|protected|static|final|abstract|synchronized|override|suspend|\s)*(?:fun\s+|[\w<>\[\],.?]+\s+)(\w+)\s*\([^;{]*\)\s*(?:throws\s+[\w.,\s]+)?[{:=]`)

	cInclude = regexp.MustCompile(`(?m)^\s*#\s*include\s+[<"]([^>"]+)[>"]`)
	cFn      = regexp.MustCompile(`(?m)^[A-Za-z_][\w\s\*&:<>,]*?\b(\w+)\s*\([^;{]*\)\s*(?:const\s*)?\{`)

	csUsing = regexp.MustCompile(`(?m)^\s*using\s+([\w.]+)\s*;`)
	csType  = regexp.MustCompile(`(?m)^\s*(?:public|private|protected|internal|abstract|sealed|static|partial|\s)*(?:class|interface|struct|enum|record)\s+(\w+)`)

	rbRequire = regexp.MustCompile(`(?m)^\s*require(?:_relative)?\s+['"]([^'"]+)['"]`)
	rbDef     = regexp.MustCompile(`(?m)^\s*(?:class|module)\s+(\w+)`)
	rbFn      = regexp.MustCompile(`(?m)^\s*def\s+(?:self\.)?(\w+)`)

	phpUse  = regexp.MustCompile(`(?m)^\s*use\s+([\w\\]+)`)
	phpType = regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait|enum)\s+(\w+)`)
	phpFn   = regexp.MustCompile(`(?m)^\s*(?:public|private|protected|static|\s)*function\s+(\w+)`)

	swiftImport = regexp.MustCompile(`(?m)^\s*import\s+(\w+)`)
	swiftType   = regexp.MustCompile(`(?m)^\s*(?:public|internal|private|fileprivate|open|final|\s)*(?:class|struct|enum|protocol|actor|extension)\s+(\w+)`)
	swiftFn     = regexp.MustCompile(`(?m)^\s*(?:public|internal|private|fileprivate|open|static|override|mutating|\s)*func\s+(\w+)`)

	// No lookbehind in RE2: the caller checks the byte before the match.
	callRx = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*\(`)
)

var callSkip = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`if for while switch catch function return typeof await new super import require
		func go defer select range make len cap append copy delete panic recover print println
		string byte int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 uintptr float32 float64
		bool rune error any close complex real imag min max clear
		def class lambda not and or in is elif with assert yield del except raise
		sizeof void`) {
		callSkip[w] = true
	}
}

// ParseFile extracts symbols, imports and calls from one source file.
func ParseFile(name, src string) Parsed {
	lang := LangFor(name)
	if lang == "" {
		lang = "txt"
	}
	lines := newLineIndex(src)
	p := Parsed{Lang: lang, LOC: countLines(src)}
	switch lang {
	case "py":
		parsePython(src, lines, &p)
	case "js", "ts":
		parseJS(src, lines, &p)
	case "go":
		parseGo(src, lines, &p)
	case "rs":
		p.Imports = groups(rsUse, src, 1)
		p.Symbols = append(symbolsFrom(rsType, src, lines, "type"), symbolsFrom(rsFn, src, lines, "fn")...)
	case "java", "kt":
		p.Imports = groups(javaImport, src, 1)
		if m := javaPkg.FindStringSubmatch(src); m != nil {
			p.Imports = append(p.Imports, m[1])
		}
		p.Symbols = append(symbolsFrom(javaType, src, lines, "class"), symbolsFrom(javaFn, src, lines, "fn")...)
	case "c", "cpp":
		p.Imports = groups(cInclude, src, 1)
		p.Symbols = symbolsFrom(cFn, src, lines, "fn")
	case "cs":
		p.Imports = groups(csUsing, src, 1)
		p.Symbols = symbolsFrom(csType, src, lines, "class")
	case "rb":
		p.Imports = groups(rbRequire, src, 1)
		p.Symbols = append(symbolsFrom(rbDef, src, lines, "class"), symbolsFrom(rbFn, src, lines, "fn")...)
	case "php":
		p.Imports = groups(phpUse, src, 1)
		p.Symbols = append(symbolsFrom(phpType, src, lines, "class"), symbolsFrom(phpFn, src, lines, "fn")...)
	case "swift":
		p.Imports = groups(swiftImport, src, 1)
		p.Symbols = append(symbolsFrom(swiftType, src, lines, "type"), symbolsFrom(swiftFn, src, lines, "fn")...)
	}
	p.Symbols = dedupeSymbols(p.Symbols)
	sort.SliceStable(p.Symbols, func(i, j int) bool { return p.Symbols[i].Line < p.Symbols[j].Line })
	for i := range p.Symbols {
		if p.Symbols[i].Sig == "" {
			p.Symbols[i].Sig = p.Symbols[i].Kind + " " + p.Symbols[i].Name
		}
	}
	if len(p.Exports) == 0 {
		for _, s := range p.Symbols {
			p.Exports = append(p.Exports, s.Name)
		}
	}
	if p.Calls == nil && (lang == "rs" || lang == "java" || lang == "kt" || lang == "c" || lang == "cpp" || lang == "rb" || lang == "php" || lang == "swift") {
		p.Calls = regexCalls(src, lines, p.Symbols)
	}
	p.Imports = uniq(p.Imports)
	p.Exports = uniq(p.Exports)
	if len(p.Symbols) > maxSymbols {
		p.Symbols = p.Symbols[:maxSymbols]
	}
	if len(p.Exports) > maxExports {
		p.Exports = p.Exports[:maxExports]
	}
	if len(p.Calls) > maxCalls {
		p.Calls = p.Calls[:maxCalls]
	}
	return p
}

func parsePython(src string, lines lineIndex, p *Parsed) {
	for _, m := range pyFrom.FindAllStringSubmatch(src, -1) {
		mod := m[1]
		if strings.Trim(mod, ".") == "" {
			// from . import a, b → each name is a sibling module
			for _, n := range strings.Split(strings.Trim(m[2], "() "), ",") {
				n = strings.TrimSpace(strings.Fields(n + " ")[0])
				if n != "" && n != "*" {
					p.Imports = append(p.Imports, mod+n)
				}
			}
			continue
		}
		p.Imports = append(p.Imports, mod)
	}
	for _, m := range pyImport.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			f := strings.Fields(part)
			if len(f) > 0 {
				p.Imports = append(p.Imports, f[0])
			}
		}
	}
	for _, m := range pyClass.FindAllStringSubmatchIndex(src, -1) {
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		p.Symbols = append(p.Symbols, Symbol{Name: name, Kind: "class", Line: lines.at(m[0])})
		if indent == "" && !strings.HasPrefix(name, "_") {
			p.Exports = append(p.Exports, name)
		}
	}
	for _, m := range pyDef.FindAllStringSubmatchIndex(src, -1) {
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		kind := "fn"
		if indent != "" {
			kind = "method"
		}
		p.Symbols = append(p.Symbols, Symbol{Name: name, Kind: kind, Line: lines.at(m[0])})
		if indent == "" && !strings.HasPrefix(name, "_") {
			p.Exports = append(p.Exports, name)
		}
	}
	p.Symbols = dedupeSymbols(p.Symbols)
	sort.SliceStable(p.Symbols, func(i, j int) bool { return p.Symbols[i].Line < p.Symbols[j].Line })
	p.Calls = regexCalls(src, lines, p.Symbols)
}

func parseJS(src string, lines lineIndex, p *Parsed) {
	p.Imports = append(p.Imports, groups(jsFrom, src, 1)...)
	p.Imports = append(p.Imports, groups(jsSideEff, src, 1)...)
	p.Imports = append(p.Imports, groups(jsRequire, src, 1)...)
	p.Imports = append(p.Imports, groups(jsDynImport, src, 1)...)
	p.Exports = append(p.Exports, groups(jsExportCls, src, 1)...)
	p.Exports = append(p.Exports, groups(jsExportFn, src, 1)...)
	p.Exports = append(p.Exports, groups(jsExportVar, src, 1)...)
	p.Symbols = append(p.Symbols, symbolsFrom(jsClass, src, lines, "class")...)
	p.Symbols = append(p.Symbols, symbolsFrom(jsFn, src, lines, "fn")...)
	p.Symbols = append(p.Symbols, symbolsFrom(jsConstFn, src, lines, "fn")...)
	p.Symbols = dedupeSymbols(p.Symbols)
	sort.SliceStable(p.Symbols, func(i, j int) bool { return p.Symbols[i].Line < p.Symbols[j].Line })
	p.Calls = regexCalls(src, lines, p.Symbols)
}

func parseGo(src string, lines lineIndex, p *Parsed) {
	p.GoAliases = map[string]string{}
	addImport := func(alias, imp string) {
		alias = strings.TrimSpace(alias)
		p.Imports = append(p.Imports, imp)
		if alias == "_" || alias == "." {
			return
		}
		if alias == "" {
			alias = goDefaultAlias(imp)
		}
		p.GoAliases[alias] = imp
	}
	for _, b := range goImportBlock.FindAllStringSubmatch(src, -1) {
		for _, ln := range strings.Split(b[1], "\n") {
			if m := goImportLine.FindStringSubmatch(ln); m != nil {
				addImport(m[1], m[2])
			}
		}
	}
	for _, m := range goImportOne.FindAllStringSubmatch(src, -1) {
		addImport(m[1], m[2])
	}
	if m := goPackage.FindStringSubmatch(src); m != nil {
		p.GoPkg = m[1]
	}
	p.Symbols = append(p.Symbols, symbolsFrom(goType, src, lines, "type")...)
	for _, blk := range goTypeBlock.FindAllStringSubmatchIndex(src, -1) {
		body := src[blk[2]:blk[3]]
		for _, ln := range strings.Split(body, "\n") {
			f := strings.Fields(ln)
			if len(f) >= 2 && isIdent(f[0]) && !strings.HasPrefix(f[0], "//") {
				p.Symbols = append(p.Symbols, Symbol{Name: f[0], Kind: "type", Line: lines.at(blk[2] + strings.Index(body, ln))})
			}
		}
	}
	p.Symbols = append(p.Symbols, symbolsFrom(goFunc, src, lines, "fn")...)
	// package-level constants and variables, alone or in a ( ... ) block
	for _, m := range goValue.FindAllStringSubmatchIndex(src, -1) {
		if name := src[m[4]:m[5]]; name != "_" {
			p.Symbols = append(p.Symbols, Symbol{Name: name, Kind: src[m[2]:m[3]], Line: lines.at(m[0])})
		}
	}
	for _, blk := range goValueBlock.FindAllStringSubmatchIndex(src, -1) {
		kind, body := src[blk[2]:blk[3]], src[blk[4]:blk[5]]
		off := blk[4]
		for _, ln := range strings.SplitAfter(body, "\n") {
			// one tab deep is an entry; deeper lines continue a value
			if strings.HasPrefix(ln, "\t") && !strings.HasPrefix(ln, "\t\t") {
				for _, name := range goValueNames(ln) {
					p.Symbols = append(p.Symbols, Symbol{Name: name, Kind: kind, Line: lines.at(off)})
				}
			}
			off += len(ln)
		}
	}
	p.Symbols = dedupeSymbols(p.Symbols)
	sort.SliceStable(p.Symbols, func(i, j int) bool { return p.Symbols[i].Line < p.Symbols[j].Line })
	own := map[string]bool{}
	for _, s := range p.Symbols {
		own[s.Name] = true
		if isExported(s.Name) {
			p.Exports = append(p.Exports, s.Name)
		}
	}
	p.GoUses = goUses(src, own, p.GoAliases)
	seen := map[string]bool{}
	for _, m := range goSelector.FindAllStringSubmatch(src, -1) {
		if _, ok := p.GoAliases[m[1]]; !ok {
			continue
		}
		key := m[1] + "." + m[2]
		if !seen[key] {
			seen[key] = true
			p.GoRefs = append(p.GoRefs, key)
		}
	}
	p.Calls = regexCalls(src, lines, p.Symbols)
}

// goValueNames reads the names a const/var block entry declares:
// "A, B int = 1, 2" → A, B.
func goValueNames(ln string) []string {
	ln = strings.TrimSpace(ln)
	if i := strings.Index(ln, "//"); i >= 0 {
		ln = ln[:i]
	}
	if i := strings.IndexByte(ln, '='); i >= 0 {
		ln = ln[:i]
	}
	var out []string
	for _, part := range strings.Split(ln, ",") {
		f := strings.Fields(part)
		if len(f) > 0 && isIdent(f[0]) && f[0] != "_" {
			out = append(out, f[0])
		}
	}
	return out
}

const maxGoUses = 600

var goKeywords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`break case chan const continue default defer else fallthrough for func go goto if
		import interface map package range return select struct switch type var nil true false iota`) {
		goKeywords[w] = true
	}
}

// goUses lists the bare identifiers a file names that it does not define:
// with comments and literals stripped, and selectors (x.Name) left out,
// those are what can only come from its own package.
func goUses(src string, own map[string]bool, aliases map[string]string) []string {
	code := stripGoNoise(src)
	seen := map[string]bool{}
	var out []string
	for _, m := range goIdent.FindAllStringIndex(code, -1) {
		if m[0] > 0 && (code[m[0]-1] == '.' || isAlnum(code[m[0]-1])) {
			continue
		}
		name := code[m[0]:m[1]]
		if seen[name] || own[name] || goKeywords[name] || callSkip[name] {
			continue
		}
		if _, ok := aliases[name]; ok {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= maxGoUses {
			break
		}
	}
	return out
}

// stripGoNoise blanks comments and string/rune literals, keeping offsets.
func stripGoNoise(src string) string {
	b := []byte(src)
	blank := func(from, to int) {
		for i := from; i < to && i < len(b); i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			j := i
			for j < len(b) && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			j := strings.Index(string(b[i+2:]), "*/")
			end := len(b)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			blank(i, end)
			i = end - 1
		case b[i] == '`':
			j := strings.IndexByte(string(b[i+1:]), '`')
			end := len(b)
			if j >= 0 {
				end = i + 1 + j + 1
			}
			blank(i, end)
			i = end - 1
		case b[i] == '"' || b[i] == '\'':
			q := b[i]
			j := i + 1
			for j < len(b) && b[j] != q && b[j] != '\n' {
				if b[j] == '\\' {
					j++
				}
				j++
			}
			blank(i, j+1)
			i = j
		}
	}
	return string(b)
}

// goDefaultAlias guesses the package name from an import path: last element,
// minus a major-version suffix and a "go-" prefix.
func goDefaultAlias(imp string) string {
	parts := strings.Split(imp, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && len(last) >= 2 && last[0] == 'v' && isDigits(last[1:]) {
		last = parts[len(parts)-2]
	}
	last = strings.TrimPrefix(last, "go-")
	if i := strings.LastIndexAny(last, "-."); i >= 0 {
		last = last[i+1:]
	}
	return last
}

// regexCalls records line-level calls. The owner is the last function symbol
// at or above the call line — coarse, but enough to say "who calls X".
func regexCalls(src string, lines lineIndex, symbols []Symbol) []Call {
	var fns []Symbol
	defined := map[string]bool{}
	for _, s := range symbols {
		defined[s.Name] = true
		if s.Kind == "fn" || s.Kind == "method" {
			fns = append(fns, s)
		}
	}
	if len(fns) == 0 {
		return nil
	}
	var out []Call
	seen := map[[3]string]bool{}
	for _, m := range callRx.FindAllStringSubmatchIndex(src, -1) {
		if m[0] > 0 {
			prev := src[m[0]-1]
			if prev == '.' || prev == '_' || prev == '$' || isAlnum(prev) {
				continue
			}
		}
		name := src[m[2]:m[3]]
		if callSkip[name] {
			continue
		}
		line := lines.at(m[0])
		i := sort.Search(len(fns), func(i int) bool { return fns[i].Line > line })
		if i == 0 {
			continue
		}
		owner := fns[i-1].Name
		if owner == name {
			continue
		}
		key := [3]string{owner, name, strconv.Itoa(line)}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Call{Caller: owner, Name: name, Line: line, Local: defined[name]})
		if len(out) >= 400 {
			break
		}
	}
	return out
}

func symbolsFrom(rx *regexp.Regexp, src string, lines lineIndex, kind string) []Symbol {
	var out []Symbol
	for _, m := range rx.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if callSkip[name] {
			continue
		}
		out = append(out, Symbol{Name: name, Kind: kind, Line: lines.at(m[0])})
	}
	return out
}

func groups(rx *regexp.Regexp, src string, g int) []string {
	var out []string
	for _, m := range rx.FindAllStringSubmatch(src, -1) {
		if s := strings.TrimSpace(m[g]); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func dedupeSymbols(in []Symbol) []Symbol {
	type key struct {
		n, k string
		l    int
	}
	seen := map[key]bool{}
	byNameLine := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		k := key{s.Name, s.Kind, s.Line}
		nl := s.Name + "@" + strconv.Itoa(s.Line)
		if seen[k] || byNameLine[nl] {
			continue
		}
		seen[k] = true
		byNameLine[nl] = true
		out = append(out, s)
	}
	return out
}

func uniq(items []string) []string {
	seen := map[string]bool{}
	out := items[:0:0]
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" || seen[it] {
			continue
		}
		seen[it] = true
		out = append(out, it)
	}
	return out
}

type lineIndex []int

func newLineIndex(src string) lineIndex {
	idx := lineIndex{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			idx = append(idx, i+1)
		}
	}
	return idx
}

// at returns the 1-based line of a byte offset.
func (l lineIndex) at(off int) int {
	return sort.Search(len(l), func(i int) bool { return l[i] > off })
}

// countLines is what an editor shows: a trailing newline ends the last
// line rather than opening another.
func countLines(src string) int {
	n := strings.Count(src, "\n")
	if src != "" && !strings.HasSuffix(src, "\n") {
		n++
	}
	return n
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || isAlnum(c)) || (i == 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func isExported(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }
