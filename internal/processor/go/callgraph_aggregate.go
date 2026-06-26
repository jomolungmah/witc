package goparser

import (
	"fmt"

	"github.com/ai-suite/witc/internal/processor"
)

// CallGraph represents a unified call graph across multiple files.
type CallGraph struct {
	Functions map[string]*FuncInfo `json:"functions"`
	Edges     []Edge               `json:"edges"`
	// ExternalCalls counts resolved calls to functions outside the analyzed
	// module (standard library and third-party). Only the type-checked builder
	// populates this; the AST aggregate leaves it zero.
	ExternalCalls int `json:"externalCalls,omitempty"`
	// ExternalDeps maps each analyzed package's import path to the sorted set
	// of external packages it calls into. Populated by the typed builder only.
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

// addFileIfNew adds a file to the list if not already present.
func (fi *FuncInfo) addFileIfNew(file string) {
	if file == "" {
		return
	}
	for _, f := range fi.Files {
		if f == file {
			return
		}
	}
	fi.Files = append(fi.Files, file)
}

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
