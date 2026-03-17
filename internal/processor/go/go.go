package goparser

import (
	"bytes"
	"context"
	"path/filepath"

	"github.com/ai-suite/witc/internal/processor"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
)

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
				switch t := ts.Type.(type) {
				case *ast.StructType:
					s := processor.Struct{Name: ts.Name.Name}
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
					iface := processor.Interface{Name: ts.Name.Name}
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
			if x.Recv != nil {
				recv := formatRecv(fset, x.Recv)
				m := processor.Method{Receiver: recv, Name: x.Name.Name, Signature: sig}
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
					Signature: sig,
				})
			}
			return true
		}
		return true
	})

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
		return ""
	}
	var buf bytes.Buffer
	buf.WriteString("func(")
	if ft.Params != nil {
		for i, p := range ft.Params.List {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(formatExpr(fset, p.Type))
		}
	}
	buf.WriteString(")")
	if ft.Results != nil && len(ft.Results.List) > 0 {
		buf.WriteString(" ")
		if len(ft.Results.List) == 1 && len(ft.Results.List[0].Names) == 0 {
			buf.WriteString(formatExpr(fset, ft.Results.List[0].Type))
		} else {
			buf.WriteString("(")
			for i, r := range ft.Results.List {
				if i > 0 {
					buf.WriteString(", ")
				}
				buf.WriteString(formatExpr(fset, r.Type))
			}
			buf.WriteString(")")
		}
	}
	return buf.String()
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
