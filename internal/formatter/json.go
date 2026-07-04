package formatter

import (
	"encoding/json"
	"sort"

	"github.com/jomolungmah/witc/internal/callgraph"
	"github.com/jomolungmah/witc/internal/processor"
)

// jsonLoc converts a symbol location to its JSON form, returning nil when the
// location is unknown (zero line) so the field is omitted.
func jsonLoc(l processor.Location) *jsonLocation {
	if l.Line == 0 {
		return nil
	}
	return &jsonLocation{File: l.File, Line: l.Line}
}

// SchemaVersion identifies the JSON output schema. It is bumped when the shape
// changes so consumers can detect compatibility. See docs/json-schema.md.
const SchemaVersion = "1.2"

// The json* types below define an explicit, stable output contract that is
// intentionally decoupled from the internal parser/call-graph structs, so those
// can be refactored without breaking consumers.

type jsonOutput struct {
	SchemaVersion string         `json:"schemaVersion"`
	Root          string         `json:"root"`
	Packages      []jsonPackage  `json:"packages"`
	CallGraph     *jsonCallGraph `json:"callGraph,omitempty"`
	Metrics       *jsonMetrics   `json:"metrics,omitempty"`
	Architecture  *jsonArch      `json:"architecture,omitempty"`
}

type jsonPackage struct {
	ImportPath string          `json:"importPath"`
	Language   string          `json:"language,omitempty"`
	Doc        string          `json:"doc,omitempty"`
	Structs    []jsonStruct    `json:"structs,omitempty"`
	Interfaces []jsonInterface `json:"interfaces,omitempty"`
	Functions  []jsonFunction  `json:"functions,omitempty"`
}

type jsonStruct struct {
	Name     string        `json:"name"`
	Exported bool          `json:"exported"`
	Doc      string        `json:"doc,omitempty"`
	Location *jsonLocation `json:"location,omitempty"`
	Fields   []jsonField   `json:"fields,omitempty"`
	Methods  []jsonMethod  `json:"methods,omitempty"`
}

type jsonField struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type"`
}

type jsonLocation struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type jsonMethod struct {
	Receiver  string        `json:"receiver,omitempty"`
	Name      string        `json:"name"`
	Exported  bool          `json:"exported"`
	Doc       string        `json:"doc,omitempty"`
	Location  *jsonLocation `json:"location,omitempty"`
	Signature string        `json:"signature"`
}

type jsonInterface struct {
	Name     string        `json:"name"`
	Exported bool          `json:"exported"`
	Doc      string        `json:"doc,omitempty"`
	Location *jsonLocation `json:"location,omitempty"`
	Fields   []jsonField   `json:"fields,omitempty"`
	Methods  []jsonMethod  `json:"methods,omitempty"`
	Alias    string        `json:"alias,omitempty"`
}

type jsonFunction struct {
	Name      string        `json:"name"`
	Exported  bool          `json:"exported"`
	Doc       string        `json:"doc,omitempty"`
	Location  *jsonLocation `json:"location,omitempty"`
	Signature string        `json:"signature"`
}

type jsonCallGraph struct {
	Functions     []jsonGraphFunc `json:"functions"`
	ExternalCalls int             `json:"externalCalls"`
}

type jsonGraphFunc struct {
	Name    string   `json:"name"`
	Package string   `json:"package,omitempty"`
	Callers []string `json:"callers,omitempty"`
	Callees []string `json:"callees,omitempty"`
}

type jsonMetrics struct {
	TotalFunctions        int      `json:"totalFunctions"`
	TotalCalls            int      `json:"totalCalls"`
	InternalCalls         int      `json:"internalCalls"`
	ExternalCalls         int      `json:"externalCalls"`
	AvgCalleesPerFunction float64  `json:"avgCalleesPerFunction"`
	MaxFanInFunction      string   `json:"maxFanInFunction,omitempty"`
	MaxFanInCount         int      `json:"maxFanInCount"`
	MaxFanOutFunction     string   `json:"maxFanOutFunction,omitempty"`
	MaxFanOutCount        int      `json:"maxFanOutCount"`
	MaxCallDepth          int      `json:"maxCallDepth"`
	HighCouplingFunctions []string `json:"highCouplingFunctions,omitempty"`
}

type jsonArch struct {
	EntryPoints          []string            `json:"entryPoints,omitempty"`
	PackageDependencies  map[string][]string `json:"packageDependencies,omitempty"`
	ExternalDependencies map[string][]string `json:"externalDependencies,omitempty"`
}

// JSON formats the summary as JSON using the versioned output schema.
func JSON(sum *Summary) (string, error) {
	out := jsonOutput{
		SchemaVersion: SchemaVersion,
		Root:          sum.Root,
		Packages:      jsonPackages(sum),
	}
	if sum.CallGraph != nil && len(sum.CallGraph.Functions) > 0 {
		out.CallGraph = jsonCallGraphOf(sum.CallGraph)
		out.Metrics = jsonMetricsOf(sum.CallGraph)
	}
	out.Architecture = jsonArchOf(sum)

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func jsonPackages(sum *Summary) []jsonPackage {
	keys := make([]string, 0, len(sum.Packages))
	for k := range sum.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pkgs := make([]jsonPackage, 0, len(keys))
	for _, k := range keys {
		r := sum.Packages[k]
		if r == nil {
			continue
		}
		p := jsonPackage{ImportPath: k, Language: r.Language, Doc: r.Doc}
		for _, s := range r.Structs {
			js := jsonStruct{Name: s.Name, Exported: s.Exported, Doc: s.Doc, Location: jsonLoc(s.Loc)}
			for _, f := range s.Fields {
				js.Fields = append(js.Fields, jsonField{Name: f.Name, Type: f.Type})
			}
			for _, m := range s.Methods {
				js.Methods = append(js.Methods, jsonMethod{Receiver: m.Receiver, Name: m.Name, Exported: m.Exported, Doc: m.Doc, Location: jsonLoc(m.Loc), Signature: m.Signature})
			}
			p.Structs = append(p.Structs, js)
		}
		for _, iface := range r.Interfaces {
			ji := jsonInterface{Name: iface.Name, Exported: iface.Exported, Doc: iface.Doc, Location: jsonLoc(iface.Loc), Alias: iface.Alias}
			for _, f := range iface.Fields {
				ji.Fields = append(ji.Fields, jsonField{Name: f.Name, Type: f.Type})
			}
			for _, m := range iface.Methods {
				ji.Methods = append(ji.Methods, jsonMethod{Name: m.Name, Exported: m.Exported, Doc: m.Doc, Location: jsonLoc(m.Loc), Signature: m.Signature})
			}
			p.Interfaces = append(p.Interfaces, ji)
		}
		for _, fn := range r.Functions {
			p.Functions = append(p.Functions, jsonFunction{Name: fn.Name, Exported: fn.Exported, Doc: fn.Doc, Location: jsonLoc(fn.Loc), Signature: fn.Signature})
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func jsonCallGraphOf(cg *callgraph.CallGraph) *jsonCallGraph {
	names := make([]string, 0, len(cg.Functions))
	for n := range cg.Functions {
		names = append(names, n)
	}
	sort.Strings(names)

	out := &jsonCallGraph{ExternalCalls: cg.ExternalCalls}
	for _, n := range names {
		info := cg.Functions[n]
		out.Functions = append(out.Functions, jsonGraphFunc{
			Name:    n,
			Package: info.Package,
			Callers: uniqueCallerNames(info),
			Callees: uniqueCalleeNames(info),
		})
	}
	return out
}

func jsonMetricsOf(cg *callgraph.CallGraph) *jsonMetrics {
	m := callgraph.CalculateMetrics(cg)
	return &jsonMetrics{
		TotalFunctions:        m.TotalFunctions,
		TotalCalls:            m.TotalCalls,
		InternalCalls:         m.InternalCalls,
		ExternalCalls:         m.ExternalCalls,
		AvgCalleesPerFunction: m.AvgCalleesPerFunc,
		MaxFanInFunction:      m.MaxFanIn,
		MaxFanInCount:         m.MaxFanInCount,
		MaxFanOutFunction:     m.MaxFanOut,
		MaxFanOutCount:        m.MaxFanOutCount,
		MaxCallDepth:          m.MaxCallDepth,
		HighCouplingFunctions: m.HighCouplingFuncs,
	}
}

func jsonArchOf(sum *Summary) *jsonArch {
	cg := sum.CallGraph
	if cg == nil || len(cg.Functions) == 0 {
		return nil
	}

	relKeys := make([]string, 0, len(sum.Packages))
	for k := range sum.Packages {
		relKeys = append(relKeys, k)
	}
	fullToRel := pkgPathMap(cg, relKeys)

	arch := &jsonArch{EntryPoints: entryPointNames(cg)}

	// Internal package -> package dependencies, keyed and valued by display path.
	for from, tos := range internalPackageDeps(cg) {
		fromRel := fullToRel[from]
		if fromRel == "" {
			continue
		}
		var deps []string
		for to := range tos {
			if toRel := fullToRel[to]; toRel != "" {
				deps = append(deps, toRel)
			}
		}
		if len(deps) > 0 {
			sort.Strings(deps)
			if arch.PackageDependencies == nil {
				arch.PackageDependencies = map[string][]string{}
			}
			arch.PackageDependencies[fromRel] = deps
		}
	}

	// External dependencies, re-keyed to display paths for consistency.
	for full, deps := range cg.ExternalDeps {
		rel := fullToRel[full]
		if rel == "" {
			rel = full
		}
		if arch.ExternalDependencies == nil {
			arch.ExternalDependencies = map[string][]string{}
		}
		arch.ExternalDependencies[rel] = deps
	}

	return arch
}
