package tsparser

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jomolungmah/witc/internal/callgraph"
)

//go:embed analyze.js
var analyzeJS []byte

// sidecarTimeout bounds one node invocation; type-checking even large
// frontends finishes well inside this.
const sidecarTimeout = 120 * time.Second

// BuildTypedCallGraph builds the TS/JS call graph with the TypeScript type
// checker, run as a node sidecar against the analyzed project's own
// typescript package. It is the top tier for TypeScript: unlike the
// import-resolving builder it infers types, so member calls through local
// variables ("const c = createClient(); c.getUser()") resolve too. It fails
// (and the caller falls back to BuildCallGraph) when node or a
// node_modules/typescript installation is not available.
func BuildTypedCallGraph(root string, relPaths []string, logf func(string, ...any)) (*callgraph.CallGraph, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("node not in PATH")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	groups := groupByTSConfig(absRoot, relPaths)
	tslib := findTypeScript(absRoot, groups)
	if tslib == "" {
		return nil, fmt.Errorf("no node_modules/typescript found under %s", root)
	}
	logf("typescript sidecar: %s, %d tsconfig group(s)", tslib, len(groups))

	// The script runs from a temp file so node reports usable stack traces;
	// the request comes in on stdin.
	script, err := os.CreateTemp("", "witc-analyze-*.js")
	if err != nil {
		return nil, err
	}
	defer os.Remove(script.Name())
	if _, err := script.Write(analyzeJS); err != nil {
		script.Close()
		return nil, err
	}
	if err := script.Close(); err != nil {
		return nil, err
	}

	req, err := json.Marshal(sidecarRequest{Root: absRoot, Groups: groups})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sidecarTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script.Name())
	cmd.Stdin = bytes.NewReader(req)
	cmd.Env = append(os.Environ(), "WITC_TSLIB="+tslib)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("typescript sidecar: %w: %s", err, firstLine(stderr.String()))
	}

	var resp sidecarResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("typescript sidecar: bad response: %w", err)
	}
	return graphFromSidecar(&resp), nil
}

// sidecarRequest is the stdin payload for analyze.js.
type sidecarRequest struct {
	Root   string         `json:"root"`
	Groups []sidecarGroup `json:"groups"`
}

// sidecarGroup is one compilation unit: the files governed by one tsconfig
// (or none), compiled as a single ts.Program.
type sidecarGroup struct {
	TSConfig string   `json:"tsconfig"` // root-relative path, "" when absent
	Files    []string `json:"files"`
}

// sidecarResponse mirrors analyze.js's stdout: declarations and resolved
// references with unqualified witc names; qualification with the package
// prefix happens here.
type sidecarResponse struct {
	Decls []struct {
		File string `json:"file"`
		Func string `json:"func"`
	} `json:"decls"`
	Edges []struct {
		FromFile string `json:"fromFile"`
		FromFunc string `json:"fromFunc"`
		ToFile   string `json:"toFile"`
		ToFunc   string `json:"toFunc"`
		Line     int    `json:"line"`
	} `json:"edges"`
	Externals []struct {
		FromFile string `json:"fromFile"`
		Module   string `json:"module"` // npm package, "" for lib globals
	} `json:"externals"`
}

// graphFromSidecar maps the sidecar's file-level output onto witc's graph
// model with the same conventions as the import-resolving builder: nodes named
// "pkg.Fn" (pkg = directory base, absent for root files), edges deduplicated
// per call site, external packages grouped by display directory.
func graphFromSidecar(resp *sidecarResponse) *callgraph.CallGraph {
	g := &callgraph.CallGraph{Functions: map[string]*callgraph.FuncInfo{}, Edges: []callgraph.Edge{}}
	node := func(name, dir string) *callgraph.FuncInfo {
		if info := g.Functions[name]; info != nil {
			return info
		}
		info := &callgraph.FuncInfo{Name: name, Package: dir, Files: []string{}}
		g.Functions[name] = info
		return info
	}

	for _, d := range resp.Decls {
		node(qualifyTS(d.File, d.Func), path.Dir(d.File)).AddFile(d.File)
	}

	seen := map[string]struct{}{}
	for _, e := range resp.Edges {
		caller := qualifyTS(e.FromFile, e.FromFunc)
		callee := qualifyTS(e.ToFile, e.ToFunc)
		key := fmt.Sprintf("%s->%s:%s:%d", caller, callee, e.FromFile, e.Line)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		callerNode := node(caller, path.Dir(e.FromFile))
		callerNode.AddFile(e.FromFile)
		callerNode.Callees = append(callerNode.Callees, callgraph.Callee{
			Name: callee, File: e.FromFile, Line: e.Line, ParentFunc: caller,
		})
		calleeNode := node(callee, path.Dir(e.ToFile))
		calleeNode.Callers = append(calleeNode.Callers, callgraph.Caller{
			Name: caller, File: e.FromFile, Line: e.Line, ParentFunc: caller,
		})
		g.Edges = append(g.Edges, callgraph.Edge{Caller: caller, Callee: callee, File: e.FromFile, Line: e.Line})
	}

	deps := map[string]map[string]bool{}
	for _, x := range resp.Externals {
		g.ExternalCalls++
		if x.Module == "" {
			continue
		}
		dir := path.Dir(x.FromFile)
		if deps[dir] == nil {
			deps[dir] = map[string]bool{}
		}
		deps[dir][x.Module] = true
	}
	for dir, pkgs := range deps {
		if g.ExternalDeps == nil {
			g.ExternalDeps = map[string][]string{}
		}
		g.ExternalDeps[dir] = sortedKeys(pkgs)
	}
	return g
}

// qualifyTS prefixes a declaration name with its file's package (directory
// base, "" for root files), the same shape moduleFile.nodeName produces.
func qualifyTS(relFile, name string) string {
	dir := path.Dir(relFile)
	if dir == "." {
		return name
	}
	return dir[strings.LastIndexByte(dir, '/')+1:] + "." + name
}

// groupByTSConfig partitions the files by the nearest tsconfig.json at or
// above each file's directory, so every ts.Program compiles with the options
// that actually govern its files (a monorepo's frontend/tsconfig.json applies
// to frontend/**). Files with no tsconfig anywhere above them share the ""
// group and get default options.
func groupByTSConfig(absRoot string, relPaths []string) []sidecarGroup {
	cache := map[string]string{} // dir -> tsconfig rel path ("" when none)
	var nearest func(dir string) string
	nearest = func(dir string) string {
		if cfg, ok := cache[dir]; ok {
			return cfg
		}
		cfg := ""
		if fi, err := os.Stat(filepath.Join(absRoot, filepath.FromSlash(dir), "tsconfig.json")); err == nil && !fi.IsDir() {
			cfg = path.Join(dir, "tsconfig.json")
		} else if dir != "." {
			cfg = nearest(path.Dir(dir))
		}
		cache[dir] = cfg
		return cfg
	}

	byCfg := map[string][]string{}
	for _, p := range relPaths {
		p = filepath.ToSlash(p)
		byCfg[nearest(path.Dir(p))] = append(byCfg[nearest(path.Dir(p))], p)
	}
	groups := make([]sidecarGroup, 0, len(byCfg))
	for _, cfg := range slices.Sorted(maps.Keys(byCfg)) {
		groups = append(groups, sidecarGroup{TSConfig: cfg, Files: byCfg[cfg]})
	}
	return groups
}

// findTypeScript locates the typescript package to run the checker with,
// preferring the analyzed project's own installation so the checked language
// version matches what the project compiles with: each tsconfig group's
// directory and its ancestors are searched, then the root. The WITC_TSLIB
// environment variable overrides the search (used by tests).
func findTypeScript(absRoot string, groups []sidecarGroup) string {
	if env := os.Getenv("WITC_TSLIB"); env != "" {
		return env
	}
	seen := map[string]bool{}
	var dirs []string
	addAncestry := func(dir string) {
		for {
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
			if dir == "." {
				return
			}
			dir = path.Dir(dir)
		}
	}
	for _, gr := range groups {
		if gr.TSConfig != "" {
			addAncestry(path.Dir(gr.TSConfig))
		}
	}
	addAncestry(".")
	for _, dir := range dirs {
		p := filepath.Join(absRoot, filepath.FromSlash(dir), "node_modules", "typescript")
		if fi, err := os.Stat(filepath.Join(p, "package.json")); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// firstLine trims a stderr dump to its first non-empty line for error text.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "(no stderr)"
}
