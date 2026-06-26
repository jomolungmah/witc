package goparser

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"

	"golang.org/x/tools/go/packages"
)

// loadMode is the set of information BuildTypedCallGraph needs from go/packages:
// syntax trees plus full type information for the loaded packages and their deps.
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
	packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps

// BuildTypedCallGraph loads the Go module rooted at dir and constructs a call
// graph using full type information. Unlike the AST-only path, callees are
// resolved to their declared functions/methods, so a call such as
// scanner.Scan() and the declared func Scan share a single node, and standard
// library / third-party calls are identified precisely rather than by name.
//
// It returns an error (so callers can fall back to the AST graph) when the
// module cannot be loaded or type-checked.
func BuildTypedCallGraph(dir string) (*CallGraph, error) {
	cfg := &packages.Config{Mode: loadMode, Dir: dir}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages found in %s", dir)
	}
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

	for _, p := range pkgs {
		for _, file := range p.Syntax {
			b.walkFile(p, file)
		}
	}
	b.finalizeExternalDeps()
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
