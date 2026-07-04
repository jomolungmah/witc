package index

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The types below are the read-side view of the JSON schema written by the
// formatter. They are intentionally separate from the formatter's (unexported)
// writer types so the query layer depends only on the documented contract.

// Index is a parsed, queryable witc index.
type Index struct {
	SchemaVersion string     `json:"schemaVersion"`
	Root          string     `json:"root"`
	Packages      []Package  `json:"packages"`
	CallGraph     *CallGraph `json:"callGraph"`
}

// Package is one package's API surface.
type Package struct {
	ImportPath string      `json:"importPath"`
	Language   string      `json:"language"`
	Doc        string      `json:"doc"`
	Structs    []Struct    `json:"structs"`
	Interfaces []Interface `json:"interfaces"`
	Functions  []Function  `json:"functions"`
}

// Location is a symbol's declaration site.
type Location struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

func (l *Location) String() string {
	if l == nil {
		return "?"
	}
	return fmt.Sprintf("%s:%d", l.File, l.Line)
}

type Struct struct {
	Name     string    `json:"name"`
	Doc      string    `json:"doc"`
	Location *Location `json:"location"`
	Methods  []Method  `json:"methods"`
}

type Interface struct {
	Name     string    `json:"name"`
	Doc      string    `json:"doc"`
	Location *Location `json:"location"`
	Methods  []Method  `json:"methods"`
	// Alias is the right-hand side of a non-object type alias (TS only).
	Alias string `json:"alias"`
}

type Method struct {
	Receiver  string    `json:"receiver"`
	Name      string    `json:"name"`
	Doc       string    `json:"doc"`
	Location  *Location `json:"location"`
	Signature string    `json:"signature"`
}

type Function struct {
	Name      string    `json:"name"`
	Doc       string    `json:"doc"`
	Location  *Location `json:"location"`
	Signature string    `json:"signature"`
}

// CallGraph holds the resolved call relationships.
type CallGraph struct {
	Functions []GraphFunc `json:"functions"`
}

// GraphFunc is one node: a function/method with its in-module callers/callees.
type GraphFunc struct {
	Name    string   `json:"name"`
	Package string   `json:"package"`
	Callers []string `json:"callers"`
	Callees []string `json:"callees"`
}

// Parse reads index JSON into a queryable Index.
func Parse(data []byte) (*Index, error) {
	var ix Index
	if err := json.Unmarshal(data, &ix); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return &ix, nil
}

// Symbol is a flattened API-surface hit returned by Find.
type Symbol struct {
	Kind      string    `json:"kind"` // "func", "method", "struct", "interface"
	Package   string    `json:"package"`
	Language  string    `json:"language,omitempty"`
	Receiver  string    `json:"receiver,omitempty"`
	Name      string    `json:"name"`
	Signature string    `json:"signature,omitempty"`
	Doc       string    `json:"doc,omitempty"`
	Location  *Location `json:"location,omitempty"`
}

// Find returns the symbols matching query. A match is, in priority order:
// an exact name match; a "pkg.Name" qualified match (pkg matching the package's
// import path or its last segment); otherwise a case-insensitive substring
// match. Exact/qualified matches suppress substring fallback so a precise query
// is not drowned out by incidental substrings. Results are sorted by location.
func (ix *Index) Find(query string) []Symbol {
	pkgHint, name := splitQualifier(query)

	var exact, fuzzy []Symbol
	for _, p := range ix.Packages {
		if pkgHint != "" && !pkgMatches(p.ImportPath, pkgHint) {
			continue
		}
		for _, s := range p.Structs {
			collect(&exact, &fuzzy, name, Symbol{Kind: "struct", Package: p.ImportPath, Language: p.Language, Name: s.Name, Doc: s.Doc, Location: s.Location})
			for _, m := range s.Methods {
				collect(&exact, &fuzzy, name, Symbol{Kind: "method", Package: p.ImportPath, Language: p.Language, Receiver: m.Receiver, Name: m.Name, Signature: m.Signature, Doc: m.Doc, Location: m.Location})
			}
		}
		for _, in := range p.Interfaces {
			collect(&exact, &fuzzy, name, Symbol{Kind: "interface", Package: p.ImportPath, Language: p.Language, Name: in.Name, Signature: aliasSignature(in.Alias), Doc: in.Doc, Location: in.Location})
			for _, m := range in.Methods {
				collect(&exact, &fuzzy, name, Symbol{Kind: "method", Package: p.ImportPath, Language: p.Language, Receiver: in.Name, Name: m.Name, Signature: m.Signature, Doc: m.Doc, Location: m.Location})
			}
		}
		for _, f := range p.Functions {
			collect(&exact, &fuzzy, name, Symbol{Kind: "func", Package: p.ImportPath, Language: p.Language, Name: f.Name, Signature: f.Signature, Doc: f.Doc, Location: f.Location})
		}
	}

	hits := exact
	if len(hits) == 0 {
		hits = fuzzy
	}
	sortSymbols(hits)
	return hits
}

// aliasSignature renders a type alias's right-hand side as a signature-like
// string ("= string | number"), so alias hits show what the alias stands for.
func aliasSignature(alias string) string {
	if alias == "" {
		return ""
	}
	return "= " + alias
}

// collect appends sym to exact when its name equals the query, else to fuzzy
// when the query is a case-insensitive substring of the name.
func collect(exact, fuzzy *[]Symbol, name string, sym Symbol) {
	switch {
	case sym.Name == name:
		*exact = append(*exact, sym)
	case strings.Contains(strings.ToLower(sym.Name), strings.ToLower(name)):
		*fuzzy = append(*fuzzy, sym)
	}
}

// GraphFuncs returns call-graph nodes matching query by qualified name
// ("pkg.Func", "pkg.(*T).Method") or by the bare trailing identifier.
func (ix *Index) GraphFuncs(query string) []GraphFunc {
	if ix.CallGraph == nil {
		return nil
	}
	var out []GraphFunc
	for _, gf := range ix.CallGraph.Functions {
		if funcMatches(gf.Name, query) {
			out = append(out, gf)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Package returns the package whose import path equals path or ends in it
// (e.g. "scanner" matches "internal/scanner"), or nil.
func (ix *Index) Package(path string) *Package {
	for i := range ix.Packages {
		if pkgMatches(ix.Packages[i].ImportPath, path) {
			return &ix.Packages[i]
		}
	}
	return nil
}

func funcMatches(name, query string) bool {
	if name == query || strings.HasSuffix(name, "."+query) {
		return true
	}
	return lastSegment(name) == query
}

// lastSegment returns the identifier after the final ".", e.g. "Method" for
// "pkg.(*T).Method" and "Scan" for "scanner.Scan".
func lastSegment(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// splitQualifier splits "pkg.Name" into ("pkg", "Name"); an unqualified query
// returns ("", query). Receiver-qualified forms like "(*T).M" are treated as
// unqualified so the dot inside the receiver isn't mistaken for a package.
func splitQualifier(query string) (pkgHint, name string) {
	i := strings.LastIndexByte(query, '.')
	if i <= 0 || strings.ContainsAny(query[:i], "(*)") {
		return "", query
	}
	return query[:i], query[i+1:]
}

func pkgMatches(importPath, hint string) bool {
	return importPath == hint || lastPathSegment(importPath) == hint || strings.HasSuffix(importPath, "/"+hint)
}

func lastPathSegment(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func sortSymbols(syms []Symbol) {
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].Package != syms[j].Package {
			return syms[i].Package < syms[j].Package
		}
		return syms[i].Location.String() < syms[j].Location.String()
	})
}
