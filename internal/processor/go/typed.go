package goparser

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"golang.org/x/tools/go/packages"
)

// loadMode is the set of information BuildTypedCallGraph needs from go/packages:
// syntax trees plus full type information for the loaded packages and their deps.
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
	packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps

// BuildOptions controls diagnostic instrumentation for the typed call-graph
// build. The zero value is silent and behaves exactly like the original build.
type BuildOptions struct {
	// Logf, when non-nil, receives phase-level progress and timing messages
	// (package load time, package counts, per-package walk timing, final tallies)
	// — useful for understanding why the build is slow on a large repo.
	Logf func(format string, args ...any)
	// PerPackage, when true, logs per-package walk timing and counts via Logf.
	PerPackage bool
	// TracePackages, when true, routes go/packages driver logging through Logf.
	// This surfaces every underlying `go list` invocation and its timing, which
	// is the most granular view of where the type-checking phase spends time.
	TracePackages bool
}

func (o BuildOptions) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// BuildTypedCallGraph loads the Go module rooted at dir and constructs a call
// graph using full type information. Unlike the AST-only path, callees are
// resolved to their declared functions/methods, so a call such as
// scanner.Scan() and the declared func Scan share a single node, and standard
// library / third-party calls are identified precisely rather than by name.
//
// It returns an error (so callers can fall back to the AST graph) when the
// module cannot be loaded or type-checked.
func BuildTypedCallGraph(dir string) (*CallGraph, error) {
	return BuildTypedCallGraphWithOptions(dir, BuildOptions{})
}

// BuildTypedCallGraphWithOptions is BuildTypedCallGraph with diagnostic
// instrumentation controlled by opts.
func BuildTypedCallGraphWithOptions(dir string, opts BuildOptions) (*CallGraph, error) {
	cfg := &packages.Config{Mode: loadMode, Dir: dir}
	if opts.TracePackages && opts.Logf != nil {
		// go/packages logs driver (go list) invocations and their timing here.
		cfg.Logf = func(format string, args ...any) {
			opts.Logf("packages: "+format, args...)
		}
	}

	opts.logf("loading packages from %s ...", dir)
	loadStart := time.Now()
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages found in %s", dir)
	}

	// Count every package reachable through imports — that's the full set the
	// type-checker had to process, and it usually explains a slow load far better
	// than the count of the module's own packages.
	typeChecked := 0
	packages.Visit(pkgs, func(*packages.Package) bool { typeChecked++; return true }, nil)
	opts.logf("loaded %d module package(s), %d total incl. dependencies, in %s",
		len(pkgs), typeChecked, time.Since(loadStart).Round(time.Millisecond))

	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("%d package load/type error(s)", n)
	}

	// inModule records the import paths that belong to the analyzed module so
	// calls into stdlib/third-party packages can be told apart from local ones.
	inModule := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		inModule[p.PkgPath] = true
	}

	b := &typedBuilder{
		cg:       &CallGraph{Functions: map[string]*FuncInfo{}, Edges: []Edge{}},
		inModule: inModule,
		edges:    map[string]struct{}{},
		extDeps:  map[string]map[string]bool{},
	}

	walkStart := time.Now()
	// Sort packages so per-package logging is deterministic and easy to scan.
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })
	for _, p := range pkgs {
		pkgStart := time.Now()
		edgesBefore, funcsBefore := len(b.cg.Edges), len(b.cg.Functions)
		for _, file := range p.Syntax {
			b.walkFile(p, file)
		}
		if opts.PerPackage {
			opts.logf("  walked %s: %d file(s), +%d edge(s), +%d func node(s) in %s",
				p.PkgPath, len(p.Syntax),
				len(b.cg.Edges)-edgesBefore, len(b.cg.Functions)-funcsBefore,
				time.Since(pkgStart).Round(time.Millisecond))
		}
	}
	b.finalizeExternalDeps()
	opts.logf("built call graph: %d func node(s), %d edge(s), %d external call(s) in %s",
		len(b.cg.Functions), len(b.cg.Edges), b.cg.ExternalCalls,
		time.Since(walkStart).Round(time.Millisecond))
	return b.cg, nil
}

type typedBuilder struct {
	cg       *CallGraph
	inModule map[string]bool
	edges    map[string]struct{}
	// extDeps records, per analyzed package path, the set of external package
	// paths it calls into.
	extDeps map[string]map[string]bool
}

// finalizeExternalDeps converts the accumulated dependency sets into the sorted
// slices exposed on the call graph.
func (b *typedBuilder) finalizeExternalDeps() {
	if len(b.extDeps) == 0 {
		return
	}
	deps := make(map[string][]string, len(b.extDeps))
	for pkg, set := range b.extDeps {
		paths := make([]string, 0, len(set))
		for p := range set {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		deps[pkg] = paths
	}
	b.cg.ExternalDeps = deps
}

func (b *typedBuilder) walkFile(p *packages.Package, file *ast.File) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		caller, _ := p.TypesInfo.Defs[fd.Name].(*types.Func)
		if caller == nil {
			continue
		}
		callerName := funcDisplayName(caller)
		callerFile := filepath.Base(p.Fset.Position(fd.Pos()).Filename)
		// Ensure every declared function is a node, even leaves and uncalled ones.
		b.node(callerName, p.PkgPath, callerFile)

		if fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee := calleeFunc(call, p.TypesInfo)
			if callee == nil {
				// Builtins, type conversions, and func-valued variables don't
				// resolve to a declared *types.Func; skip them.
				return true
			}
			calleePkg := ""
			if callee.Pkg() != nil {
				calleePkg = callee.Pkg().Path()
			}
			if !b.inModule[calleePkg] {
				// Standard library or third-party call: out of scope for the
				// internal call graph, but counted and tracked for metrics and
				// the dependency map.
				b.cg.ExternalCalls++
				if calleePkg != "" {
					if b.extDeps[p.PkgPath] == nil {
						b.extDeps[p.PkgPath] = map[string]bool{}
					}
					b.extDeps[p.PkgPath][calleePkg] = true
				}
				return true
			}
			b.addEdge(callerName, p.PkgPath, callee, p.Fset.Position(call.Pos()))
			return true
		})
	}
}

func (b *typedBuilder) node(name, pkgPath, file string) *FuncInfo {
	fi := b.cg.Functions[name]
	if fi == nil {
		fi = &FuncInfo{Name: name, Package: pkgPath, Files: []string{}, Callers: []Caller{}, Callees: []Callee{}}
		b.cg.Functions[name] = fi
	}
	fi.addFileIfNew(file)
	return fi
}

func (b *typedBuilder) addEdge(callerName, callerPkg string, callee *types.Func, pos token.Position) {
	calleeName := funcDisplayName(callee)
	calleePkg := callee.Pkg().Path()
	base := filepath.Base(pos.Filename)

	key := callerName + "->" + calleeName + ":" + pos.Filename + ":" + strconv.Itoa(pos.Line)
	if _, dup := b.edges[key]; dup {
		return
	}
	b.edges[key] = struct{}{}

	callerInfo := b.node(callerName, callerPkg, base)
	calleeInfo := b.node(calleeName, calleePkg, base)

	callerInfo.Callees = append(callerInfo.Callees, Callee{Name: calleeName, File: base, Line: pos.Line, ParentFunc: callerName})
	calleeInfo.Callers = append(calleeInfo.Callers, Caller{Name: callerName, File: base, Line: pos.Line, ParentFunc: callerName})
	b.cg.Edges = append(b.cg.Edges, Edge{Caller: callerName, Callee: calleeName, File: base, Line: pos.Line})
}

// calleeFunc resolves the function or method a call expression invokes, or nil
// when it is not a declared function (builtin, conversion, or func value).
func calleeFunc(call *ast.CallExpr, info *types.Info) *types.Func {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		id = fn
	case *ast.SelectorExpr:
		id = fn.Sel
	default:
		return nil
	}
	if obj, ok := info.Uses[id]; ok {
		if fn, ok := obj.(*types.Func); ok {
			return fn
		}
	}
	return nil
}

// funcDisplayName produces a readable, reasonably unique name for a function or
// method: "pkg.Func" for package functions and "pkg.(*T).Method" for methods.
func funcDisplayName(fn *types.Func) string {
	pkgName := ""
	if fn.Pkg() != nil {
		pkgName = fn.Pkg().Name()
	}

	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		// Render the receiver type without package qualification, e.g. "*T".
		recv := types.TypeString(sig.Recv().Type(), func(*types.Package) string { return "" })
		if pkgName != "" {
			return pkgName + ".(" + recv + ")." + fn.Name()
		}
		return "(" + recv + ")." + fn.Name()
	}

	if pkgName != "" {
		return pkgName + "." + fn.Name()
	}
	return fn.Name()
}
