//go:build cgo

// Package tsparser implements processor.Processor for TypeScript and
// JavaScript source files (.ts, .tsx, .js, .jsx) using tree-sitter.
//
// Mapping onto the language-neutral model: classes become Structs, interfaces
// and type aliases and enums become Interfaces (aliases carry their right-hand
// side in Alias), and both function declarations and consts initialized with a
// function become Functions. Exported means the symbol carries an `export`
// modifier or appears in an `export { ... }` / `export default` clause.
//
// The per-file call records (Result.CallGraph) feed callgraph.Aggregate, the
// AST-only fallback tier: plain identifier calls, `new` expressions, and JSX
// elements with capitalized names (component renders) are recorded; member
// calls like api.log() are not, since without type information they are mostly
// external noise.
package tsparser

import (
	"context"
	"fmt"
	"go/doc"
	"path/filepath"
	"strings"

	"github.com/jomolungmah/witc/internal/processor"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Processor implements processor.Processor for TypeScript/JavaScript files.
type Processor struct{}

var supportedExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}

// Supports reports whether the file extension is handled by this processor.
func (p *Processor) Supports(ext string) bool { return supportedExts[ext] }

// Process parses one file and extracts its API surface and call records.
// Plain .ts uses the typescript grammar (where `<T>expr` casts are legal);
// .tsx, .jsx, and .js use the tsx grammar, which accepts JSX.
func (p *Processor) Process(ctx context.Context, path string, src []byte) (*processor.Result, error) {
	lang := tstypescript.LanguageTSX()
	if strings.HasSuffix(path, ".ts") {
		lang = tstypescript.LanguageTypescript()
	}

	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		return nil, fmt.Errorf("set language for %s: %w", path, err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s: no tree produced", path)
	}
	defer tree.Close()

	f := &file{path: filepath.ToSlash(path), src: src}
	result := &processor.Result{
		Package:   pkgNameFor(path),
		Language:  "typescript",
		CallGraph: map[string][]processor.CallInfo{},
	}

	root := tree.RootNode()
	for i := uint(0); i < root.NamedChildCount(); i++ {
		f.statement(result, root.NamedChild(i))
	}
	f.markExported(result)
	f.calls(result, root, "", "")
	return result, nil
}

// pkgNameFor derives a package-like display name from the file's directory,
// the closest TS/JS analogue to Go's package clause.
func pkgNameFor(path string) string {
	dir := filepath.Dir(filepath.ToSlash(path))
	if dir == "." {
		return ""
	}
	return dir[strings.LastIndexByte(dir, '/')+1:]
}

// file carries per-file parse state.
type file struct {
	path string
	src  []byte
	// exportedNames collects identifiers from `export { A, B }` and
	// `export default Name` clauses; symbols matching them are marked exported
	// in a post-pass, since the clause may precede or follow the declaration.
	exportedNames []string
}

func (f *file) text(n *sitter.Node) string { return n.Utf8Text(f.src) }

func (f *file) loc(n *sitter.Node) processor.Location {
	p := n.StartPosition()
	return processor.Location{File: f.path, Line: int(p.Row) + 1, Column: int(p.Column) + 1}
}

// statement dispatches one top-level statement. Doc comments attach to the
// outermost node (the export_statement when a declaration is exported).
func (f *file) statement(result *processor.Result, n *sitter.Node) {
	switch n.Kind() {
	case "export_statement":
		if decl := n.ChildByFieldName("declaration"); decl != nil {
			f.declaration(result, decl, true, f.docFor(n))
			return
		}
		// `export default Name;` re-exports an earlier declaration.
		if v := n.ChildByFieldName("value"); v != nil && v.Kind() == "identifier" {
			f.exportedNames = append(f.exportedNames, f.text(v))
			return
		}
		// `export { A, B as C }`.
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if c := n.NamedChild(i); c.Kind() == "export_clause" {
				for j := uint(0); j < c.NamedChildCount(); j++ {
					spec := c.NamedChild(j)
					if name := spec.ChildByFieldName("name"); name != nil {
						f.exportedNames = append(f.exportedNames, f.text(name))
					}
				}
			}
		}
	case "ambient_declaration": // `declare ...`
		for i := uint(0); i < n.NamedChildCount(); i++ {
			f.declaration(result, n.NamedChild(i), false, f.docFor(n))
		}
	default:
		f.declaration(result, n, false, f.docFor(n))
	}
}

// declaration extracts one declaration node into the result.
func (f *file) declaration(result *processor.Result, n *sitter.Node, exported bool, docText string) {
	switch n.Kind() {
	case "interface_declaration":
		result.Interfaces = append(result.Interfaces, f.interfaceDecl(n, exported, docText))
	case "type_alias_declaration":
		result.Interfaces = append(result.Interfaces, f.typeAlias(n, exported, docText))
	case "enum_declaration":
		result.Interfaces = append(result.Interfaces, f.enumDecl(n, exported, docText))
	case "class_declaration", "abstract_class_declaration":
		result.Structs = append(result.Structs, f.classDecl(n, exported, docText))
	case "function_declaration", "generator_function_declaration":
		if fn, ok := f.functionDecl(n, exported, docText); ok {
			result.Functions = append(result.Functions, fn)
		}
	case "lexical_declaration", "variable_declaration":
		f.constDecls(result, n, exported, docText)
	}
}

// markExported flips the Exported flag on symbols whose names appeared in an
// export clause of this file.
func (f *file) markExported(result *processor.Result) {
	if len(f.exportedNames) == 0 {
		return
	}
	names := make(map[string]bool, len(f.exportedNames))
	for _, n := range f.exportedNames {
		names[n] = true
	}
	for i := range result.Structs {
		if names[result.Structs[i].Name] {
			result.Structs[i].Exported = true
		}
	}
	for i := range result.Interfaces {
		if names[result.Interfaces[i].Name] {
			result.Interfaces[i].Exported = true
		}
	}
	for i := range result.Functions {
		if names[result.Functions[i].Name] {
			result.Functions[i].Exported = true
		}
	}
}

func (f *file) interfaceDecl(n *sitter.Node, exported bool, docText string) processor.Interface {
	iface := processor.Interface{Exported: exported, Doc: docText}
	if name := n.ChildByFieldName("name"); name != nil {
		iface.Name = f.text(name)
		iface.Loc = f.loc(name)
	}
	if body := n.ChildByFieldName("body"); body != nil {
		iface.Fields, iface.Methods = f.objectMembers(body, exported)
	}
	return iface
}

// typeAlias maps `type X = ...`: object types get member Fields like an
// interface; anything else keeps its right-hand side verbatim in Alias.
func (f *file) typeAlias(n *sitter.Node, exported bool, docText string) processor.Interface {
	iface := processor.Interface{Exported: exported, Doc: docText}
	if name := n.ChildByFieldName("name"); name != nil {
		iface.Name = f.text(name)
		iface.Loc = f.loc(name)
	}
	if value := n.ChildByFieldName("value"); value != nil {
		if value.Kind() == "object_type" {
			iface.Fields, iface.Methods = f.objectMembers(value, exported)
		} else {
			iface.Alias = normalizeWS(f.text(value))
		}
	}
	return iface
}

func (f *file) enumDecl(n *sitter.Node, exported bool, docText string) processor.Interface {
	iface := processor.Interface{Exported: exported, Doc: docText}
	if name := n.ChildByFieldName("name"); name != nil {
		iface.Name = f.text(name)
		iface.Loc = f.loc(name)
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		return iface
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		m := body.NamedChild(i)
		switch m.Kind() {
		case "enum_assignment":
			field := processor.Field{}
			if name := m.ChildByFieldName("name"); name != nil {
				field.Name = f.text(name)
			}
			if v := m.ChildByFieldName("value"); v != nil {
				field.Type = "= " + normalizeWS(f.text(v))
			}
			iface.Fields = append(iface.Fields, field)
		case "property_identifier", "string":
			iface.Fields = append(iface.Fields, processor.Field{Name: f.text(m)})
		}
	}
	return iface
}

// objectMembers extracts fields and methods from an interface_body or
// object_type node. Properties (including function-typed ones) become Fields;
// method signatures become Methods.
func (f *file) objectMembers(body *sitter.Node, exported bool) ([]processor.Field, []processor.Method) {
	var fields []processor.Field
	var methods []processor.Method
	for i := uint(0); i < body.NamedChildCount(); i++ {
		m := body.NamedChild(i)
		switch m.Kind() {
		case "property_signature":
			field := processor.Field{}
			if name := m.ChildByFieldName("name"); name != nil {
				field.Name = f.text(name) + optionalMark(m)
			}
			if t := m.ChildByFieldName("type"); t != nil {
				field.Type = typeText(f, t)
			}
			fields = append(fields, field)
		case "method_signature":
			meth := processor.Method{Exported: exported, Doc: f.docFor(m)}
			if name := m.ChildByFieldName("name"); name != nil {
				meth.Name = f.text(name)
				meth.Loc = f.loc(name)
			}
			meth.Signature = f.signature(m)
			methods = append(methods, meth)
		}
	}
	return fields, methods
}

func (f *file) classDecl(n *sitter.Node, exported bool, docText string) processor.Struct {
	s := processor.Struct{Exported: exported, Doc: docText}
	if name := n.ChildByFieldName("name"); name != nil {
		s.Name = f.text(name)
		s.Loc = f.loc(name)
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		return s
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		m := body.NamedChild(i)
		switch m.Kind() {
		case "method_definition":
			meth := processor.Method{
				Receiver: s.Name,
				Exported: exported && !isPrivateMember(f, m),
				Doc:      f.docFor(m),
			}
			if name := m.ChildByFieldName("name"); name != nil {
				meth.Name = f.text(name)
				meth.Loc = f.loc(name)
			}
			meth.Signature = f.signature(m)
			s.Methods = append(s.Methods, meth)
		case "public_field_definition", "field_definition":
			field := processor.Field{}
			if name := m.ChildByFieldName("name"); name != nil {
				field.Name = f.text(name) + optionalMark(m)
			}
			if t := m.ChildByFieldName("type"); t != nil {
				field.Type = typeText(f, t)
			}
			s.Fields = append(s.Fields, field)
		}
	}
	return s
}

func (f *file) functionDecl(n *sitter.Node, exported bool, docText string) (processor.Function, bool) {
	name := n.ChildByFieldName("name")
	if name == nil {
		return processor.Function{}, false
	}
	return processor.Function{
		Name:      f.text(name),
		Exported:  exported,
		Doc:       docText,
		Loc:       f.loc(name),
		Signature: f.signature(n),
	}, true
}

// constDecls extracts top-level const/let declarators: function values become
// Functions, and object literals with function members (API clients, service
// objects) become class-like Structs. Other const values are not part of the
// model yet.
func (f *file) constDecls(result *processor.Result, n *sitter.Node, exported bool, docText string) {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		d := n.NamedChild(i)
		if d.Kind() != "variable_declarator" {
			continue
		}
		name := d.ChildByFieldName("name")
		value := d.ChildByFieldName("value")
		if name == nil || value == nil || name.Kind() != "identifier" {
			continue
		}
		switch {
		case isFunctionValue(value):
			result.Functions = append(result.Functions, processor.Function{
				Name:      f.text(name),
				Exported:  exported,
				Doc:       docText,
				Loc:       f.loc(name),
				Signature: f.signature(value),
			})
		case value.Kind() == "object":
			if s, ok := f.objectConst(f.text(name), name, value, exported, docText); ok {
				result.Structs = append(result.Structs, s)
			}
		case value.Kind() == "call_expression":
			// A factory-result const (zustand's create(), React's memo(),
			// createBrowserRouter(), ...) is API surface: stores and wrapped
			// components are imported and called all over an app. The empty
			// signature renders it as "const Name" rather than a function.
			result.Functions = append(result.Functions, processor.Function{
				Name:     f.text(name),
				Exported: exported,
				Doc:      docText,
				Loc:      f.loc(name),
			})
		}
	}
}

// objectConst maps `const api = { get(id) {...}, retries: 3 }` onto the Struct
// model: function-valued members become methods, literal members become typed
// fields. Objects with no function members are plain data, not API surface.
func (f *file) objectConst(name string, nameNode, obj *sitter.Node, exported bool, docText string) (processor.Struct, bool) {
	s := processor.Struct{Name: name, Exported: exported, Doc: docText, Loc: f.loc(nameNode)}
	for i := uint(0); i < obj.NamedChildCount(); i++ {
		member := obj.NamedChild(i)
		switch member.Kind() {
		case "pair":
			key := member.ChildByFieldName("key")
			value := member.ChildByFieldName("value")
			if key == nil || value == nil {
				continue
			}
			if isFunctionValue(value) {
				s.Methods = append(s.Methods, processor.Method{
					Receiver:  name,
					Name:      f.text(key),
					Exported:  exported,
					Doc:       f.docFor(member),
					Loc:       f.loc(key),
					Signature: f.signature(value),
				})
			} else if t := literalType(value); t != "" {
				s.Fields = append(s.Fields, processor.Field{Name: f.text(key), Type: t})
			}
		case "method_definition": // shorthand `get(id) {...}`
			if key := member.ChildByFieldName("name"); key != nil {
				s.Methods = append(s.Methods, processor.Method{
					Receiver:  name,
					Name:      f.text(key),
					Exported:  exported,
					Doc:       f.docFor(member),
					Loc:       f.loc(key),
					Signature: f.signature(member),
				})
			}
		}
	}
	return s, len(s.Methods) > 0
}

// literalType names the primitive type of a literal member value, or "".
func literalType(n *sitter.Node) string {
	switch n.Kind() {
	case "string", "template_string":
		return "string"
	case "number":
		return "number"
	case "true", "false":
		return "boolean"
	}
	return ""
}

func isFunctionValue(n *sitter.Node) bool {
	switch n.Kind() {
	case "arrow_function", "function_expression", "generator_function":
		return true
	}
	return false
}

// signature renders a function-ish node's parameters and return type in
// TypeScript syntax, e.g. "(name: string, age?: number): void".
func (f *file) signature(n *sitter.Node) string {
	params := "()"
	if p := n.ChildByFieldName("parameters"); p != nil {
		params = normalizeWS(f.text(p))
	} else if p := n.ChildByFieldName("parameter"); p != nil { // `x => ...`
		params = "(" + normalizeWS(f.text(p)) + ")"
	}
	if ret := n.ChildByFieldName("return_type"); ret != nil {
		return params + normalizeWS(f.text(ret))
	}
	return params
}

// isPrivateMember reports whether a class member is private or protected, via
// an accessibility modifier or a #name.
func isPrivateMember(f *file, m *sitter.Node) bool {
	for i := uint(0); i < m.NamedChildCount(); i++ {
		c := m.NamedChild(i)
		if c.Kind() == "accessibility_modifier" {
			t := f.text(c)
			return t == "private" || t == "protected"
		}
	}
	if name := m.ChildByFieldName("name"); name != nil && name.Kind() == "private_property_identifier" {
		return true
	}
	return false
}

// optionalMark returns "?" when the member carries an optional marker.
func optionalMark(m *sitter.Node) string {
	for i := uint(0); i < m.ChildCount(); i++ {
		if m.Child(i).Kind() == "?" {
			return "?"
		}
	}
	return ""
}

// typeText returns a type_annotation's type without the leading colon.
func typeText(f *file, annotation *sitter.Node) string {
	return normalizeWS(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(f.text(annotation)), ":")))
}

// docFor extracts the doc synopsis from a comment node immediately preceding n
// (ending on the line just above it, or further up through a comment block).
func (f *file) docFor(n *sitter.Node) string {
	prev := n.PrevNamedSibling()
	if prev == nil || prev.Kind() != "comment" {
		return ""
	}
	if int(prev.EndPosition().Row)+1 < int(n.StartPosition().Row) {
		return "" // blank line between comment and declaration: not a doc comment
	}
	return doc.Synopsis(stripCommentMarkers(f.text(prev)))
}

// stripCommentMarkers removes // and /** */ decoration and JSDoc tag lines,
// leaving plain prose for the synopsis. Banner decoration such as
// "===== Section =====" is trimmed to its text.
func stripCommentMarkers(raw string) string {
	raw = strings.TrimPrefix(raw, "/**")
	raw = strings.TrimPrefix(raw, "/*")
	raw = strings.TrimSuffix(raw, "*/")
	var out []string
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		line = strings.TrimSpace(strings.Trim(line, "=-*#~"))
		if strings.HasPrefix(line, "@") { // JSDoc tags end the prose
			break
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

// normalizeWS collapses all whitespace runs (including newlines in multi-line
// parameter lists) to single spaces and tightens them against brackets.
func normalizeWS(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for _, r := range []struct{ old, new string }{
		{"( ", "("}, {" )", ")"}, {"[ ", "["}, {" ]", "]"}, {" ,", ","}, {" ;", ";"},
	} {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	return s
}

// calls walks the tree recording per-file call records for the AST fallback
// call graph. parent is the qualified name of the enclosing named function
// ("UserCard", "Store.add"); class tracks the enclosing class for methods.
// Module-level calls (empty parent) are not recorded.
func (f *file) calls(result *processor.Result, n *sitter.Node, parent, class string) {
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration":
		if name := n.ChildByFieldName("name"); name != nil {
			parent = f.text(name)
		}
	case "class_declaration", "abstract_class_declaration":
		if name := n.ChildByFieldName("name"); name != nil {
			class = f.text(name)
		}
	case "method_definition":
		if name := n.ChildByFieldName("name"); name != nil {
			parent = f.text(name)
			if class != "" {
				parent = class + "." + parent
			}
		}
	case "variable_declarator":
		name := n.ChildByFieldName("name")
		if value := n.ChildByFieldName("value"); name != nil && value != nil &&
			name.Kind() == "identifier" && isFunctionValue(value) {
			parent = f.text(name)
		}
	case "call_expression":
		if fn := n.ChildByFieldName("function"); fn != nil && fn.Kind() == "identifier" {
			f.record(result, parent, f.text(fn), fn)
		}
	case "new_expression":
		if c := n.ChildByFieldName("constructor"); c != nil && c.Kind() == "identifier" {
			f.record(result, parent, f.text(c), c)
		}
	case "jsx_opening_element", "jsx_self_closing_element":
		// Rendering a component is the JSX analogue of a call.
		if name := n.ChildByFieldName("name"); name != nil && name.Kind() == "identifier" {
			if t := f.text(name); isComponentName(t) {
				f.record(result, parent, t, name)
			}
		}
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		f.calls(result, n.NamedChild(i), parent, class)
	}
}

func (f *file) record(result *processor.Result, parent, callee string, at *sitter.Node) {
	if parent == "" || parent == callee {
		return
	}
	pos := at.StartPosition()
	result.CallGraph[callee] = append(result.CallGraph[callee], processor.CallInfo{
		CallerName: callee,
		CalleeName: callee,
		File:       f.path,
		Line:       int(pos.Row) + 1,
		Column:     int(pos.Column) + 1,
		ParentFunc: parent,
	})
}

// isComponentName reports whether a JSX element name looks like a component
// (capitalized identifier) rather than an intrinsic element like div.
func isComponentName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}
