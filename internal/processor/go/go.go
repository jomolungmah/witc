package goparser

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"

	"github.com/ai-suite/witc/internal/processor"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
)

// docSynopsis returns the first sentence of a doc comment, or "" when absent.
func docSynopsis(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	return doc.Synopsis(cg.Text())
}

// Processor implements processor.Processor for Go source files.
type Processor struct {
	ExcludeGenerated bool
}

// Supports returns true for .go extension.
func (p *Processor) Supports(ext string) bool {
	return ext == ".go"
}

// Process parses the Go source and extracts API surface.
func (p *Processor) Process(ctx context.Context, path string, src []byte) (*processor.Result, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	if p.ExcludeGenerated && ast.IsGenerated(f) {
		return &processor.Result{Package: f.Name.Name}, nil
	}

	result := &processor.Result{
		Package:    f.Name.Name,
		ImportPath: filepath.Dir(path),
		Doc:        docSynopsis(f.Doc),
		Structs:    nil,
		Interfaces: nil,
		Functions:  nil,
	}

	// Collect types and functions via Inspect
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok != token.TYPE {
				return true
			}
			for _, spec := range x.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				// In a grouped "type ( ... )" block the doc is on the spec; for a
				// single "type X ..." it is on the GenDecl.
				typeDoc := ts.Doc
				if typeDoc == nil {
					typeDoc = x.Doc
				}
				switch t := ts.Type.(type) {
				case *ast.StructType:
					s := processor.Struct{Name: ts.Name.Name, Doc: docSynopsis(typeDoc)}
					for _, field := range t.Fields.List {
						for _, name := range field.Names {
							s.Fields = append(s.Fields, processor.Field{
								Name: name.Name,
								Type: formatExpr(fset, field.Type),
							})
						}
						if len(field.Names) == 0 {
							s.Fields = append(s.Fields, processor.Field{
								Type: formatExpr(fset, field.Type),
							})
						}
					}
					result.Structs = append(result.Structs, s)
				case *ast.InterfaceType:
					iface := processor.Interface{Name: ts.Name.Name, Doc: docSynopsis(typeDoc)}
					for _, m := range t.Methods.List {
						if len(m.Names) > 0 {
							sig := formatExpr(fset, m.Type)
							for _, name := range m.Names {
								iface.Methods = append(iface.Methods, processor.Method{
									Name:      name.Name,
									Signature: sig,
								})
							}
						} else {
							iface.Methods = append(iface.Methods, processor.Method{
								Signature: formatExpr(fset, m.Type),
							})
						}
					}
					result.Interfaces = append(result.Interfaces, iface)
				}
			}
			return false
		case *ast.FuncDecl:
			sig := formatFuncType(fset, x.Type)
			fnDoc := docSynopsis(x.Doc)
			if x.Recv != nil {
				recv := formatRecv(fset, x.Recv)
				m := processor.Method{Receiver: recv, Name: x.Name.Name, Doc: fnDoc, Signature: sig}
				// Attach to struct if found
				for i := range result.Structs {
					if result.Structs[i].Name == baseType(recv) {
						result.Structs[i].Methods = append(result.Structs[i].Methods, m)
						return true
					}
				}
				result.Structs = append(result.Structs, processor.Struct{
					Name:    baseType(recv),
					Methods: []processor.Method{m},
				})
			} else {
				result.Functions = append(result.Functions, processor.Function{
					Name:      x.Name.Name,
					Doc:       fnDoc,
					Signature: sig,
				})
			}
			return true
		}
		return true
	})

	// Map imported package identifiers to their import paths so the call
	// visitor can tell standard-library calls from local/third-party ones.
	imports := make(map[string]string)
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := path
		if imp.Name != nil {
			name = imp.Name.Name
		} else if i := strings.LastIndexByte(path, '/'); i >= 0 {
			name = path[i+1:]
		}
		imports[name] = path
	}

	// Collect call graph with parent function tracking
	var localCalls map[string][]CallInfo = make(map[string][]CallInfo)

	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body == nil {
				continue
			}
			currentFunc := fn.Name.Name

			// Create a visitor for this function body with the same fileset
			funcVisitor := NewCallVisitorWithFileSet(fset)
			funcVisitor.imports = imports

			// Walk the function body with context
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if callExpr, ok := n.(*ast.CallExpr); ok {
					funcVisitor.processCallExpr(callExpr, currentFunc)
				}
				return true
			})

			// Merge calls into local map
			for callee, calls := range funcVisitor.Calls {
				localCalls[callee] = append(localCalls[callee], calls...)
			}
		}
	}

	// Convert to processor.CallInfo and set on result
	result.CallGraph = make(map[string][]processor.CallInfo)
	for callee, calls := range localCalls {
		result.CallGraph[callee] = make([]processor.CallInfo, len(calls))
		for i, c := range calls {
			result.CallGraph[callee][i] = processor.CallInfo{
				CallerName: c.CallerName,
				CalleeName: c.CalleeName,
				File:       filepath.Base(c.File), // Use just filename for cleaner output
				Line:       c.Line,
				Column:     c.Column,
				ParentFunc: c.ParentFunc,
			}
		}
	}

	return result, nil
}

func formatExpr(fset *token.FileSet, expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, expr)
	return buf.String()
}

func formatFuncType(fset *token.FileSet, ft *ast.FuncType) string {
	if ft == nil {
		return "func()"
	}
	var buf bytes.Buffer
	buf.WriteString("func(")
	buf.WriteString(formatFieldList(fset, ft.Params))
	buf.WriteString(")")
	if ft.Results != nil && len(ft.Results.List) > 0 {
		buf.WriteString(" ")
		// Parenthesize when there are multiple results or any are named.
		parens := len(ft.Results.List) > 1 || len(ft.Results.List[0].Names) > 0
		if parens {
			buf.WriteString("(")
		}
		buf.WriteString(formatFieldList(fset, ft.Results))
		if parens {
			buf.WriteString(")")
		}
	}
	return buf.String()
}

// formatFieldList renders a parameter or result list, preserving names and
// expanding grouped fields (e.g. "dst, src *T") so the arity is accurate.
func formatFieldList(fset *token.FileSet, fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fl.List))
	for _, f := range fl.List {
		typ := formatExpr(fset, f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, typ)
			continue
		}
		names := make([]string, len(f.Names))
		for i, n := range f.Names {
			names[i] = n.Name
		}
		parts = append(parts, strings.Join(names, ", ")+" "+typ)
	}
	return strings.Join(parts, ", ")
}

func formatRecv(fset *token.FileSet, recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	return formatExpr(fset, recv.List[0].Type)
}

func baseType(recv string) string {
	// *T or T -> T
	if len(recv) > 0 && recv[0] == '*' {
		return recv[1:]
	}
	return recv
}
