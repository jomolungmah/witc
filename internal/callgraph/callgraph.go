// Package callgraph defines the language-neutral call-graph model shared by
// all language processors, plus the generic aggregation and metrics that
// operate on it. Language packages (e.g. processor/go) build these graphs;
// the formatter and query layers consume them without knowing the language.
package callgraph

import (
	"fmt"
	"slices"

	"github.com/jomolungmah/witc/internal/processor"
)

// CallGraph represents a unified call graph across multiple files.
type CallGraph struct {
	Functions map[string]*FuncInfo `json:"functions"`
	Edges     []Edge               `json:"edges"`
	// ExternalCalls counts resolved calls to functions outside the analyzed
	// module (standard library and third-party). Only a language's precise
	// builder populates this; the AST aggregate leaves it zero.
	ExternalCalls int `json:"externalCalls,omitempty"`
	// ExternalDeps maps each analyzed package's import path to the sorted set
	// of external packages it calls into. Populated by precise builders only.
	ExternalDeps map[string][]string `json:"externalDeps,omitempty"`
}

// FuncInfo contains information about a function in the call graph.
// Callers are functions that call this function; Callees are functions this one calls.
type FuncInfo struct {
	Name    string   `json:"name"`
	Package string   `json:"package"`
	Files   []string `json:"files"`
	Callers []Caller `json:"callers,omitempty"`
	Callees []Callee `json:"callees,omitempty"`
}

// Caller represents a caller relationship.
type Caller struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	ParentFunc string `json:"parent_func,omitempty"`
}

// Callee represents a callee relationship.
type Callee struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	ParentFunc string `json:"parent_func,omitempty"`
}

// Edge represents a caller->callee relationship.
type Edge struct {
	Caller string `json:"caller"`
	Callee string `json:"callee"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

// Aggregate builds a cross-file call graph from multiple processor results.
// It is the language-independent fallback: it only needs the per-file call
// records a processor collects, not type information.
func Aggregate(results []*processor.Result) *CallGraph {
	cg := &CallGraph{
		Functions: make(map[string]*FuncInfo),
		Edges:     []Edge{},
	}

	edgeSet := make(map[string]struct{})

	for _, r := range results {
		if r == nil || r.CallGraph == nil {
			continue
		}

		for calleeName, callers := range r.CallGraph {
			if _, ok := cg.Functions[calleeName]; !ok {
				cg.Functions[calleeName] = &FuncInfo{
					Name:    calleeName,
					Package: r.ImportPath,
					Files:   []string{},
					Callers: []Caller{},
					Callees: []Callee{},
				}
			}

			for _, caller := range callers {
				// The calling function is ParentFunc (the function whose body
				// contains the call). CallerName holds the call expression text,
				// which equals the callee for plain identifiers and must not be
				// used as the caller node.
				callingFn := caller.ParentFunc
				if callingFn == "" {
					callingFn = caller.CallerName
				}

				cg.Functions[calleeName].addFileIfNew(caller.File)

				cg.Functions[calleeName].Callers = append(cg.Functions[calleeName].Callers, Caller{
					Name:       callingFn,
					File:       caller.File,
					Line:       caller.Line,
					ParentFunc: callingFn,
				})

				edgeKey := fmt.Sprintf("%s->%s:%s:%d", callingFn, calleeName, caller.File, caller.Line)
				if _, exists := edgeSet[edgeKey]; !exists {
					edgeSet[edgeKey] = struct{}{}
					cg.Edges = append(cg.Edges, Edge{
						Caller: callingFn,
						Callee: calleeName,
						File:   caller.File,
						Line:   caller.Line,
					})
				}

				if _, ok := cg.Functions[callingFn]; !ok {
					cg.Functions[callingFn] = &FuncInfo{
						Name:    callingFn,
						Package: r.ImportPath,
						Files:   []string{},
						Callers: []Caller{},
						Callees: []Callee{},
					}
				}

				cg.Functions[callingFn].addFileIfNew(caller.File)
				cg.Functions[callingFn].Callees = append(cg.Functions[callingFn].Callees, Callee{
					Name:       calleeName,
					File:       caller.File,
					Line:       caller.Line,
					ParentFunc: callingFn,
				})
			}
		}
	}

	return cg
}

// Merge combines per-language call graphs into one. Function nodes are keyed
// by name, so languages with distinct naming schemes (e.g. "pkg.Fn" for Go)
// coexist without collisions; external counts and dependency maps are unioned.
func Merge(graphs ...*CallGraph) *CallGraph {
	merged := &CallGraph{Functions: map[string]*FuncInfo{}, Edges: []Edge{}}
	for _, g := range graphs {
		if g == nil {
			continue
		}
		for name, info := range g.Functions {
			if existing, ok := merged.Functions[name]; ok {
				for _, f := range info.Files {
					existing.addFileIfNew(f)
				}
				existing.Callers = append(existing.Callers, info.Callers...)
				existing.Callees = append(existing.Callees, info.Callees...)
			} else {
				merged.Functions[name] = info
			}
		}
		merged.Edges = append(merged.Edges, g.Edges...)
		merged.ExternalCalls += g.ExternalCalls
		for pkg, deps := range g.ExternalDeps {
			if merged.ExternalDeps == nil {
				merged.ExternalDeps = map[string][]string{}
			}
			merged.ExternalDeps[pkg] = append(merged.ExternalDeps[pkg], deps...)
		}
	}
	return merged
}

// addFileIfNew adds a file to the list if not already present.
func (fi *FuncInfo) addFileIfNew(file string) {
	if file == "" || slices.Contains(fi.Files, file) {
		return
	}
	fi.Files = append(fi.Files, file)
}

// AddFile records a file for this function, skipping duplicates. It is the
// exported form used by language builders in other packages.
func (fi *FuncInfo) AddFile(file string) { fi.addFileIfNew(file) }

// GetFunction returns function info by name, or nil if not found.
func (cg *CallGraph) GetFunction(name string) *FuncInfo {
	if cg == nil || cg.Functions == nil {
		return nil
	}
	return cg.Functions[name]
}

// GetAllFunctions returns all function names in the call graph.
func (cg *CallGraph) GetAllFunctions() []string {
	if cg == nil || cg.Functions == nil {
		return []string{}
	}

	names := make([]string, 0, len(cg.Functions))
	for name := range cg.Functions {
		names = append(names, name)
	}
	return names
}

// GetEdges returns all edges in the call graph.
func (cg *CallGraph) GetEdges() []Edge {
	if cg == nil || cg.Edges == nil {
		return []Edge{}
	}
	return cg.Edges
}
