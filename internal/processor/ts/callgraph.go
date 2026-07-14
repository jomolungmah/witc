//go:build cgo

package tsparser

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jomolungmah/witc/internal/callgraph"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// BuildCallGraph builds the module-wide TS/JS call graph for the given files
// (paths relative to root). It is the precise tier for TypeScript: imports are
// resolved per file (relative specifiers, barrel re-exports, tsconfig
// baseUrl/paths aliases), so calls, new-expressions, and JSX component renders
// connect to the declaration they actually reference, and calls into npm
// packages are counted as external dependencies — mirroring what the
// go/packages builder provides for Go, minus type inference: only identifier
// calls, this.method() calls, and member calls on imported names resolve;
// calls through arbitrary local variables do not.
func BuildCallGraph(root string, relPaths []string) (*callgraph.CallGraph, error) {
	m := &module{
		root:  root,
		res:   newResolver(root, relPaths),
		files: make(map[string]*moduleFile, len(relPaths)),
		cg:    &callgraph.CallGraph{Functions: map[string]*callgraph.FuncInfo{}, Edges: []callgraph.Edge{}},
		edges: map[string]struct{}{},
	}

	// Pass 1: parse every file and collect its declarations, imports, and
	// re-exports, so pass 2 can resolve names across files in any order.
	for _, p := range relPaths {
		if err := m.load(filepath.ToSlash(p)); err != nil {
			return nil, err
		}
	}
	// Pass 2: walk function bodies and connect resolved references.
	for _, p := range relPaths {
		f := m.files[filepath.ToSlash(p)]
		m.connect(f, f.tree.RootNode(), "", "")
		f.tree.Close()
		f.tree = nil
	}

	for dir, pkgs := range m.externalDeps {
		if m.cg.ExternalDeps == nil {
			m.cg.ExternalDeps = map[string][]string{}
		}
		m.cg.ExternalDeps[dir] = sortedKeys(pkgs)
	}
	return m.cg, nil
}

// module accumulates cross-file state while building the graph.
type module struct {
	root         string
	res          *resolver
	files        map[string]*moduleFile
	cg           *callgraph.CallGraph
	edges        map[string]struct{}        // dedupe key caller->callee:file:line
	externalDeps map[string]map[string]bool // display dir -> npm packages
}

// moduleFile is the pass-1 view of one source file.
type moduleFile struct {
	path string // slash-separated, relative to root
	dir  string // display dir ("." for root files), matches summary package keys
	pkg  string // node-name prefix: last dir segment ("" for root files)
	src  []byte
	tree *sitter.Tree

	decls       map[string]*decl     // top-level name -> declaration
	imports     map[string]importRef // local name -> import
	reExports   map[string]reExport  // exported name -> `export { x } from "spec"`
	starExports []string             // specs of `export * from "spec"`
	defaultName string               // local name behind `export default`
}

type decl struct {
	// isClass marks declarations with member methods: classes, and object
	// consts with function-valued members (API clients, service objects).
	isClass bool
	methods map[string]bool // member methods; for classes includes "constructor"
	// lazy declarations (consts holding non-function values, e.g. zustand
	// stores) are valid call targets but are not pre-registered as nodes, so
	// plain data constants don't show up as functions.
	lazy bool
}

type importRef struct {
	spec      string // the import specifier as written
	orig      string // exported name at the source ("default" for default imports)
	namespace bool   // import * as name
}

type reExport struct {
	spec string
	orig string
}

// load parses one file and collects its declarations and import surface.
func (m *module) load(relPath string) error {
	src, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(relPath)))
	if err != nil {
		return fmt.Errorf("read %s: %w", relPath, err)
	}
	lang := tstypescript.LanguageTSX()
	if strings.HasSuffix(relPath, ".ts") {
		lang = tstypescript.LanguageTypescript()
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		return fmt.Errorf("set language for %s: %w", relPath, err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return fmt.Errorf("parse %s: no tree produced", relPath)
	}

	f := &moduleFile{
		path:    relPath,
		dir:     path.Dir(relPath),
		src:     src,
		tree:    tree,
		decls:   map[string]*decl{},
		imports: map[string]importRef{},
	}
	if f.dir != "." {
		f.pkg = f.dir[strings.LastIndexByte(f.dir, '/')+1:]
	}
	m.files[relPath] = f

	root := tree.RootNode()
	for i := uint(0); i < root.NamedChildCount(); i++ {
		f.surface(root.NamedChild(i))
	}
	// Register every declared function and method as a node up front, so
	// uncalled functions still appear in the graph (like Go's typed builder).
	// Lazy declarations become nodes only if something references them.
	for name, d := range f.decls {
		if d.isClass {
			for meth := range d.methods {
				m.node(f.nodeName(name+"."+meth), f.dir).AddFile(f.path)
			}
			continue
		}
		if !d.lazy {
			m.node(f.nodeName(name), f.dir).AddFile(f.path)
		}
	}
	return nil
}

// nodeName qualifies a declaration name with the file's package prefix, the
// same "pkg.Fn" shape the summary layer derives from Result.Package.
func (f *moduleFile) nodeName(name string) string {
	if f.pkg == "" {
		return name
	}
	return f.pkg + "." + name
}

func (f *moduleFile) text(n *sitter.Node) string { return n.Utf8Text(f.src) }

// surface collects one top-level statement's contribution to the file's
// declaration and import/export tables.
func (f *moduleFile) surface(n *sitter.Node) {
	switch n.Kind() {
	case "import_statement":
		f.importDecl(n)
	case "export_statement":
		f.exportDecl(n)
	case "ambient_declaration":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			f.declare(n.NamedChild(i))
		}
	default:
		f.declare(n)
	}
}

func (f *moduleFile) importDecl(n *sitter.Node) {
	source := n.ChildByFieldName("source")
	if source == nil {
		return
	}
	spec := stringValue(f, source)
	for i := uint(0); i < n.NamedChildCount(); i++ {
		clause := n.NamedChild(i)
		if clause.Kind() != "import_clause" {
			continue
		}
		for j := uint(0); j < clause.NamedChildCount(); j++ {
			c := clause.NamedChild(j)
			switch c.Kind() {
			case "identifier": // default import
				f.imports[f.text(c)] = importRef{spec: spec, orig: "default"}
			case "namespace_import": // * as name
				for k := uint(0); k < c.NamedChildCount(); k++ {
					if id := c.NamedChild(k); id.Kind() == "identifier" {
						f.imports[f.text(id)] = importRef{spec: spec, namespace: true}
					}
				}
			case "named_imports":
				for k := uint(0); k < c.NamedChildCount(); k++ {
					is := c.NamedChild(k)
					if is.Kind() != "import_specifier" {
						continue
					}
					name := is.ChildByFieldName("name")
					if name == nil {
						continue
					}
					local := name
					if alias := is.ChildByFieldName("alias"); alias != nil {
						local = alias
					}
					f.imports[f.text(local)] = importRef{spec: spec, orig: f.text(name)}
				}
			}
		}
	}
}

func (f *moduleFile) exportDecl(n *sitter.Node) {
	if decl := n.ChildByFieldName("declaration"); decl != nil {
		name := f.declare(decl)
		if name != "" && hasKeyword(n, "default") {
			f.defaultName = name
		}
		return
	}
	if v := n.ChildByFieldName("value"); v != nil && v.Kind() == "identifier" {
		f.defaultName = f.text(v) // export default Name;
		return
	}

	source := n.ChildByFieldName("source")
	spec := ""
	if source != nil {
		spec = stringValue(f, source)
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c.Kind() != "export_clause" || spec == "" {
			continue
		}
		// export { A, B as C } from "spec"
		for j := uint(0); j < c.NamedChildCount(); j++ {
			es := c.NamedChild(j)
			name := es.ChildByFieldName("name")
			if name == nil {
				continue
			}
			exported := name
			if alias := es.ChildByFieldName("alias"); alias != nil {
				exported = alias
			}
			if f.reExports == nil {
				f.reExports = map[string]reExport{}
			}
			f.reExports[f.text(exported)] = reExport{spec: spec, orig: f.text(name)}
		}
	}
	if spec != "" && hasKeyword(n, "*") {
		f.starExports = append(f.starExports, spec) // export * from "spec"
	}
}

// declare records a top-level declaration and returns its name.
func (f *moduleFile) declare(n *sitter.Node) string {
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration":
		if name := n.ChildByFieldName("name"); name != nil {
			f.decls[f.text(name)] = &decl{}
			return f.text(name)
		}
	case "class_declaration", "abstract_class_declaration":
		name := n.ChildByFieldName("name")
		if name == nil {
			return ""
		}
		d := &decl{isClass: true, methods: map[string]bool{}}
		if body := n.ChildByFieldName("body"); body != nil {
			for i := uint(0); i < body.NamedChildCount(); i++ {
				if meth := body.NamedChild(i); meth.Kind() == "method_definition" {
					if mn := meth.ChildByFieldName("name"); mn != nil {
						d.methods[f.text(mn)] = true
					}
				}
			}
		}
		f.decls[f.text(name)] = d
		return f.text(name)
	case "lexical_declaration", "variable_declaration":
		last := ""
		for i := uint(0); i < n.NamedChildCount(); i++ {
			dcl := n.NamedChild(i)
			if dcl.Kind() != "variable_declarator" {
				continue
			}
			name := dcl.ChildByFieldName("name")
			value := dcl.ChildByFieldName("value")
			if name == nil || value == nil || name.Kind() != "identifier" {
				continue
			}
			switch {
			case isFunctionValue(value):
				f.decls[f.text(name)] = &decl{}
			case value.Kind() == "object":
				// An object const with function members acts like a class.
				if methods := f.objectMethodNames(value); len(methods) > 0 {
					f.decls[f.text(name)] = &decl{isClass: true, methods: methods}
				} else {
					f.decls[f.text(name)] = &decl{lazy: true}
				}
			default:
				// Stores, factories, plain values: a call target, not a node.
				f.decls[f.text(name)] = &decl{lazy: true}
			}
			last = f.text(name)
		}
		return last
	}
	return ""
}

// objectMethodNames collects the names of an object literal's function-valued
// members, both `get: (id) => ...` pairs and shorthand `get(id) {...}`.
func (f *moduleFile) objectMethodNames(obj *sitter.Node) map[string]bool {
	var methods map[string]bool
	for i := uint(0); i < obj.NamedChildCount(); i++ {
		member := obj.NamedChild(i)
		var key *sitter.Node
		switch member.Kind() {
		case "pair":
			if v := member.ChildByFieldName("value"); v == nil || !isFunctionValue(v) {
				continue
			}
			key = member.ChildByFieldName("key")
		case "method_definition":
			key = member.ChildByFieldName("name")
		}
		if key != nil {
			if methods == nil {
				methods = map[string]bool{}
			}
			methods[f.text(key)] = true
		}
	}
	return methods
}

// resolveExport finds the node name behind an exported name of a file,
// following default exports, barrel re-exports, and star re-exports.
func (m *module) resolveExport(filePath, name string, depth int) (node, dir string) {
	f := m.files[filePath]
	if f == nil || depth > 8 {
		return "", ""
	}
	if name == "default" {
		if f.defaultName == "" {
			return "", ""
		}
		name = f.defaultName
	}
	if _, ok := f.decls[name]; ok {
		return f.nodeName(name), f.dir
	}
	if re, ok := f.reExports[name]; ok {
		if target, _ := m.res.resolve(f.dir, re.spec); target != "" {
			return m.resolveExport(target, re.orig, depth+1)
		}
		return "", ""
	}
	for _, spec := range f.starExports {
		if target, _ := m.res.resolve(f.dir, spec); target != "" {
			if n, d := m.resolveExport(target, name, depth+1); n != "" {
				return n, d
			}
		}
	}
	return "", ""
}

// connect walks a file's tree adding edges for every reference that resolves.
// parent is the enclosing declaration's node name; class is the enclosing
// class's local name.
func (m *module) connect(f *moduleFile, n *sitter.Node, parent, class string) {
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration":
		if name := n.ChildByFieldName("name"); name != nil {
			parent = f.nodeName(f.text(name))
		}
	case "class_declaration", "abstract_class_declaration":
		if name := n.ChildByFieldName("name"); name != nil {
			class = f.text(name)
		}
	case "method_definition":
		if name := n.ChildByFieldName("name"); name != nil && class != "" {
			parent = f.nodeName(class + "." + f.text(name))
		}
	case "variable_declarator":
		name := n.ChildByFieldName("name")
		if value := n.ChildByFieldName("value"); name != nil && value != nil && name.Kind() == "identifier" {
			switch {
			case isFunctionValue(value):
				parent = f.nodeName(f.text(name))
			case value.Kind() == "object" && f.decls[f.text(name)] != nil:
				// Top-level object const: its function members are attributed
				// like methods ("api.spaceApi.get"), via the pair case below.
				class = f.text(name)
			}
		}
	case "pair":
		if key := n.ChildByFieldName("key"); key != nil && class != "" {
			if v := n.ChildByFieldName("value"); v != nil && isFunctionValue(v) {
				parent = f.nodeName(class + "." + f.text(key))
			}
		}
	case "call_expression":
		if fn := n.ChildByFieldName("function"); fn != nil {
			switch fn.Kind() {
			case "identifier":
				m.reference(f, parent, f.text(fn), fn)
			case "member_expression":
				m.memberCall(f, parent, class, fn)
			}
		}
	case "new_expression":
		if c := n.ChildByFieldName("constructor"); c != nil && c.Kind() == "identifier" {
			m.reference(f, parent, f.text(c), c)
		}
	case "jsx_opening_element", "jsx_self_closing_element":
		if name := n.ChildByFieldName("name"); name != nil && name.Kind() == "identifier" {
			if t := f.text(name); isComponentName(t) {
				m.reference(f, parent, t, name)
			}
		}
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		m.connect(f, n.NamedChild(i), parent, class)
	}
}

// reference resolves a bare identifier used as a call/new/JSX target: a
// declaration in this file, an imported name, or otherwise an external
// (npm package or runtime global) call.
func (m *module) reference(f *moduleFile, parent, name string, at *sitter.Node) {
	if parent == "" {
		return // module-level statement, not part of any function
	}
	if _, ok := f.decls[name]; ok {
		m.edge(f, parent, f.nodeName(name), f.dir, at)
		return
	}
	if imp, ok := f.imports[name]; ok {
		target, pkg := m.res.resolve(f.dir, imp.spec)
		if target != "" && !imp.namespace {
			if node, dir := m.resolveExport(target, imp.orig, 0); node != "" {
				m.edge(f, parent, node, dir, at)
				return
			}
		}
		m.external(f, pkg)
		return
	}
	m.external(f, "") // unimported: a runtime global like fetch
}

// memberCall resolves obj.method() where the receiver is statically known:
// this.m() inside a class, ns.f() on a namespace import, or Class.m() on an
// imported or local class (static-style calls).
func (m *module) memberCall(f *moduleFile, parent, class string, expr *sitter.Node) {
	if parent == "" {
		return
	}
	obj := expr.ChildByFieldName("object")
	prop := expr.ChildByFieldName("property")
	if obj == nil || prop == nil {
		return
	}
	method := f.text(prop)

	if obj.Kind() == "this" && class != "" {
		if d := f.decls[class]; d != nil && d.methods[method] {
			m.edge(f, parent, f.nodeName(class+"."+method), f.dir, prop)
		}
		return
	}
	if obj.Kind() != "identifier" {
		return
	}
	name := f.text(obj)
	if d, ok := f.decls[name]; ok && d.isClass && d.methods[method] {
		m.edge(f, parent, f.nodeName(name+"."+method), f.dir, prop)
		return
	}
	if imp, ok := f.imports[name]; ok {
		target, pkg := m.res.resolve(f.dir, imp.spec)
		switch {
		case target == "":
			m.external(f, pkg) // e.g. React.useState() on a default react import
		case imp.namespace:
			// ns.f() on `import * as ns`: the member is the exported name.
			if node, dir := m.resolveExport(target, method, 0); node != "" {
				m.edge(f, parent, node, dir, prop)
			}
		default:
			// Class.m() on an imported class: a static-style method call.
			if node, dir := m.classMethod(target, imp.orig, method); node != "" {
				m.edge(f, parent, node, dir, prop)
			}
		}
	}
	// Receiver is a local variable: unresolvable without type inference.
}

// classMethod resolves Class.method against a class exported by target.
func (m *module) classMethod(target, class, method string) (node, dir string) {
	f := m.files[target]
	if f == nil {
		return "", ""
	}
	name := class
	if name == "default" {
		name = f.defaultName
	}
	if d := f.decls[name]; d != nil && d.isClass && d.methods[method] {
		return f.nodeName(name + "." + method), f.dir
	}
	return "", ""
}

// edge records caller -> callee, creating nodes as needed and deduplicating
// repeated references from the same site.
func (m *module) edge(f *moduleFile, caller, callee, calleeDir string, at *sitter.Node) {
	pos := at.StartPosition()
	line := int(pos.Row) + 1
	key := fmt.Sprintf("%s->%s:%s:%d", caller, callee, f.path, line)
	if _, seen := m.edges[key]; seen {
		return
	}
	m.edges[key] = struct{}{}

	callerNode := m.node(caller, f.dir)
	callerNode.AddFile(f.path)
	callerNode.Callees = append(callerNode.Callees, callgraph.Callee{
		Name: callee, File: f.path, Line: line, ParentFunc: caller,
	})
	calleeNode := m.node(callee, calleeDir)
	calleeNode.Callers = append(calleeNode.Callers, callgraph.Caller{
		Name: caller, File: f.path, Line: line, ParentFunc: caller,
	})
	m.cg.Edges = append(m.cg.Edges, callgraph.Edge{Caller: caller, Callee: callee, File: f.path, Line: line})
}

func (m *module) node(name, dir string) *callgraph.FuncInfo {
	if info := m.cg.Functions[name]; info != nil {
		return info
	}
	info := &callgraph.FuncInfo{Name: name, Package: dir, Files: []string{}}
	m.cg.Functions[name] = info
	return info
}

// external counts a call that leaves the module; pkg is the npm package
// ("" for runtime globals, which are counted but not listed as dependencies).
func (m *module) external(f *moduleFile, pkg string) {
	m.cg.ExternalCalls++
	if pkg == "" {
		return
	}
	if m.externalDeps == nil {
		m.externalDeps = map[string]map[string]bool{}
	}
	if m.externalDeps[f.dir] == nil {
		m.externalDeps[f.dir] = map[string]bool{}
	}
	m.externalDeps[f.dir][pkg] = true
}

// stringValue returns the contents of a string literal node.
func stringValue(f *moduleFile, n *sitter.Node) string {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == "string_fragment" {
			return f.text(c)
		}
	}
	return strings.Trim(f.text(n), `"'`)
}

// hasKeyword reports whether n has an anonymous child token with this text.
func hasKeyword(n *sitter.Node, kw string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		if n.Child(i).Kind() == kw {
			return true
		}
	}
	return false
}
