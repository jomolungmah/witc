package goparser

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"

	"github.com/jomolungmah/witc/internal/callgraph"
)

// loadMode is the set of information BuildTypedCallGraph needs from go/packages:
// syntax trees and full type information for the analyzed module's own packages.
//
// Crucially it omits NeedDeps: we only ever inspect the syntax of the module's
// packages, never a dependency's. Without NeedDeps, imported packages are
// resolved from compiled export data rather than re-type-checked from source,
// which is dramatically faster on large dependency trees (and on slow
// filesystems such as a Windows drive mounted under WSL). Calls into imported
// packages still resolve to a *types.Func with a correct package path, which is
// all the call graph needs.
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
	packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports

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
func BuildTypedCallGraph(dir string) (*callgraph.CallGraph, error) {
	return BuildTypedCallGraphWithOptions(dir, BuildOptions{})
}

// moduleSkipDirs are not descended into when searching for go.mod files.
var moduleSkipDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true, "testdata": true,
}

// findModuleDirs returns the root-relative directories under root that contain
// a go.mod file ("." for root itself), sorted shallowest-first.
func findModuleDirs(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip rather than abort discovery
		}
		if d.IsDir() {
			if p != root && moduleSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			rel, err := filepath.Rel(root, filepath.Dir(p))
			if err == nil {
				dirs = append(dirs, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs
}

// BuildTypedCallGraphForModules builds the typed call graph for every Go
// module found under root and merges them, so monorepos where go.mod lives in
// a subdirectory (e.g. backend/) still get the type-checked tier. Nested
// modules have their package paths remapped to root-relative display paths
// ("backend/internal/svc") so they align with the summary's package keys; a
// single module at the root keeps full import paths, preserving the original
// single-module output. Modules that fail to load are skipped (logged via
// opts.Logf); an error is returned only when no module builds.
func BuildTypedCallGraphForModules(root string, opts BuildOptions) (*callgraph.CallGraph, error) {
	dirs := findModuleDirs(root)
	if len(dirs) == 0 || (len(dirs) == 1 && dirs[0] == ".") {
		// No go.mod below root: root may itself be inside a module (witc run
		// on a subdirectory), which go/packages resolves by searching upward.
		return BuildTypedCallGraphWithOptions(root, opts)
	}

	var graphs []*callgraph.CallGraph
	var firstErr error
	for _, dir := range dirs {
		g, err := BuildTypedCallGraphWithOptions(filepath.Join(root, filepath.FromSlash(dir)), opts)
		if err != nil {
			opts.logf("skipping module %s: %v", dir, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("module %s: %w", dir, err)
			}
			continue
		}
		if dir != "." {
			remapModulePackages(g, modulePathOf(root, dir), dir)
		}
		graphs = append(graphs, g)
	}
	if len(graphs) == 0 {
		return nil, firstErr
	}
	return callgraph.Merge(graphs...), nil
}

// modulePathOf reads the module path from dir's go.mod ("" when unreadable).
func modulePathOf(root, dir string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), "go.mod"))
	if err != nil {
		return ""
	}
	return modfile.ModulePath(data)
}

// remapModulePackages rewrites a nested module's package paths from full
// import paths to root-relative display paths, on both function nodes and the
// external-dependency keys.
func remapModulePackages(g *callgraph.CallGraph, modulePath, dir string) {
	if modulePath == "" {
		return
	}
	remap := func(pkg string) string {
		if pkg == modulePath {
			return dir
		}
		if rest, ok := strings.CutPrefix(pkg, modulePath+"/"); ok {
			return path.Join(dir, rest)
		}
		return pkg
	}
	for _, fi := range g.Functions {
		fi.Package = remap(fi.Package)
	}
	if len(g.ExternalDeps) > 0 {
		deps := make(map[string][]string, len(g.ExternalDeps))
		for pkg, list := range g.ExternalDeps {
			deps[remap(pkg)] = list
		}
		g.ExternalDeps = deps
	}
}

// BuildTypedCallGraphWithOptions is BuildTypedCallGraph with diagnostic
// instrumentation controlled by opts.
func BuildTypedCallGraphWithOptions(dir string, opts BuildOptions) (*callgraph.CallGraph, error) {
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

	// Count every package reachable through imports. These are now loaded from
	// export data rather than type-checked from source, but the size of the
	// import graph still indicates the scale of the load.
	inGraph := 0
	packages.Visit(pkgs, func(*packages.Package) bool { inGraph++; return true }, nil)
	opts.logf("loaded %d module package(s) (%d in import graph) in %s",
		len(pkgs), inGraph, time.Since(loadStart).Round(time.Millisecond))

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
		cg:       &callgraph.CallGraph{Functions: map[string]*callgraph.FuncInfo{}, Edges: []callgraph.Edge{}},
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
	cg       *callgraph.CallGraph
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

func (b *typedBuilder) node(name, pkgPath, file string) *callgraph.FuncInfo {
	fi := b.cg.Functions[name]
	if fi == nil {
		fi = &callgraph.FuncInfo{Name: name, Package: pkgPath, Files: []string{}, Callers: []callgraph.Caller{}, Callees: []callgraph.Callee{}}
		b.cg.Functions[name] = fi
	}
	fi.AddFile(file)
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

	callerInfo.Callees = append(callerInfo.Callees, callgraph.Callee{Name: calleeName, File: base, Line: pos.Line, ParentFunc: callerName})
	calleeInfo.Callers = append(calleeInfo.Callers, callgraph.Caller{Name: callerName, File: base, Line: pos.Line, ParentFunc: callerName})
	b.cg.Edges = append(b.cg.Edges, callgraph.Edge{Caller: callerName, Callee: calleeName, File: base, Line: pos.Line})
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
