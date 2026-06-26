package goparser

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"path/filepath"
	"strings"
)

// CallInfo contains information about a function call.
type CallInfo struct {
	CallerName string // The name of the code that makes the call (e.g., "append")
	CalleeName string // The name/function being called
	File       string // Source file path where call occurs
	Line       int    // Line number in source
	Column     int    // Column number in source
	ParentFunc string // Name of function/method containing this call (e.g., "Process")
}

// CallVisitor traverses Go AST nodes and identifies all function calls.
type CallVisitor struct {
	Calls   map[string][]CallInfo // Map of callee name to list of CallInfo
	fset    *token.FileSet        // FileSet used for position lookups
	imports map[string]string     // Imported identifier -> import path (for stdlib detection)
}

// NewCallVisitor creates a new CallVisitor instance.
func NewCallVisitor() *CallVisitor {
	return &CallVisitor{
		Calls: make(map[string][]CallInfo),
		fset:  token.NewFileSet(),
	}
}

// NewCallVisitorWithFileSet creates a new CallVisitor with a specific FileSet.
func NewCallVisitorWithFileSet(fset *token.FileSet) *CallVisitor {
	return &CallVisitor{
		Calls: make(map[string][]CallInfo),
		fset:  fset,
	}
}

// Visit implements ast.Visitor interface for traversing AST nodes.
func (v *CallVisitor) Visit(node ast.Node) ast.Visitor {
	if callExpr, ok := node.(*ast.CallExpr); ok {
		v.processCallExpr(callExpr, "")
	}
	return v
}

// processCallExpr extracts call information from a CallExpr node.
func (v *CallVisitor) processCallExpr(callExpr *ast.CallExpr, parentFunc string) {
	if v.skipCall(callExpr) {
		return
	}

	callerName := v.extractCaller(callExpr)

	calleeName, file, line, column := v.extractCallee(callExpr, v.fset)

	if calleeName != "" {
		v.Calls[calleeName] = append(v.Calls[calleeName], CallInfo{
			CallerName: callerName,
			CalleeName: calleeName,
			File:       file,
			Line:       line,
			Column:     column,
			ParentFunc: parentFunc,
		})
	}
}

// extractCaller identifies the caller from a CallExpr node.
func (v *CallVisitor) extractCaller(callExpr *ast.CallExpr) string {
	switch fn := callExpr.Fun.(type) {
	case *ast.FuncLit:
		// Anonymous function - use position as identifier
		pos := v.fset.Position(fn.Pos())
		return fmt.Sprintf("anonymous_function:%s:%d", filepath.Base(pos.Filename), pos.Line)

	case *ast.Ident:
		// Simple identifier: foo()
		return fn.Name

	case *ast.SelectorExpr:
		// Method or package function call: obj.Method() or pkg.Func()
		var buf bytes.Buffer
		printer.Fprint(&buf, v.fset, fn)
		return buf.String()

	case *ast.CallExpr:
		// Nested call: func()() or foo().bar()
		// Recursively extract caller from inner expression
		return v.extractCaller(fn)

	default:
		// Fallback - stringify the expression
		var buf bytes.Buffer
		printer.Fprint(&buf, v.fset, fn)
		return buf.String()
	}
}

// extractCallee extracts the called function name and location from different call expression types.
// Built-in and standard-library calls are filtered earlier by skipCall, so this
// only names the callee and its location.
func (v *CallVisitor) extractCallee(callExpr *ast.CallExpr, fset *token.FileSet) (calleeName string, file string, line int, column int) {
	switch fn := callExpr.Fun.(type) {
	case *ast.Ident:
		// Simple identifier: foo() or method().method2()
		calleeName = fn.Name
	case *ast.SelectorExpr:
		// Selector expression: pkg.Func() or obj.Method()
		if ident, ok := fn.X.(*ast.Ident); ok {
			calleeName = ident.Name + "." + fn.Sel.Name
		} else if sel, ok := fn.X.(*ast.SelectorExpr); ok {
			// Chained selectors: pkg.Sub.Method()
			calleeName = sel.Sel.Name + "." + fn.Sel.Name
		} else {
			// Complex selector like slice[0].method(), use just the method name
			calleeName = fn.Sel.Name
		}
	case *ast.BasicLit:
		// Basic literal call (e.g., "string".len()) - less common but valid
		calleeName = fn.Value
	case *ast.IndexExpr:
		// Index expression: map[key]() or array[index]().method()
		if ident, ok := fn.X.(*ast.Ident); ok {
			calleeName = ident.Name + "[" + stringifyIndex(fn.Index) + "]"
		} else if selector, ok := fn.X.(*ast.SelectorExpr); ok {
			calleeName = selector.Sel.Name + "[" + stringifyIndex(fn.Index) + "]"
		} else {
			var buf bytes.Buffer
			printer.Fprint(&buf, fset, fn.X)
			buf.WriteString("[")
			printer.Fprint(&buf, fset, fn.Index)
			buf.WriteString("]")
			calleeName = buf.String()
		}
	case *ast.CallExpr:
		// Nested call expression: func()() or foo().bar()
		switch inner := callExpr.Fun.(*ast.CallExpr).Fun.(type) {
		case *ast.Ident:
			calleeName = inner.Name
		case *ast.SelectorExpr:
			if ident, ok := inner.X.(*ast.Ident); ok {
				calleeName = ident.Name + "." + inner.Sel.Name
			} else {
				calleeName = inner.Sel.Name
			}
		default:
			var buf bytes.Buffer
			printer.Fprint(&buf, fset, callExpr.Fun)
			calleeName = buf.String()
		}
	default:
		// Fallback for other node types (e.g., type switch expressions)
		var buf bytes.Buffer
		printer.Fprint(&buf, fset, callExpr.Fun)
		calleeName = buf.String()
	}

	// Get source location from the CallExpr itself
	pos := fset.Position(callExpr.Pos())
	return calleeName, pos.Filename, pos.Line, pos.Column
}

// stringifyIndex converts an index expression to a string representation.
func stringifyIndex(index ast.Expr) string {
	switch idx := index.(type) {
	case *ast.Ident:
		return idx.Name
	case *ast.BasicLit:
		return idx.Value
	default:
		var buf bytes.Buffer
		printer.Fprint(&buf, token.NewFileSet(), index)
		return buf.String()
	}
}

// goBuiltins are Go's predeclared functions, which are not user-defined and
// therefore excluded from the call graph.
var goBuiltins = map[string]bool{
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
}

// predeclaredTypes are Go's predeclared type names. A call expression whose
// function is one of these is a type conversion (e.g. string(b)), not a call.
var predeclaredTypes = map[string]bool{
	"bool": true, "byte": true, "rune": true, "string": true, "error": true,
	"any": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true, "float32": true, "float64": true,
	"complex64": true, "complex128": true,
}

// skipCall reports whether a call should be excluded from the graph: Go
// built-ins and standard-library package calls (resolved through the file's
// imports). Local functions, method calls on receivers, and third-party
// package calls are kept.
func (v *CallVisitor) skipCall(callExpr *ast.CallExpr) bool {
	switch fn := callExpr.Fun.(type) {
	case *ast.Ident:
		return goBuiltins[fn.Name] || predeclaredTypes[fn.Name]
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok {
			if path, isImport := v.imports[pkg.Name]; isImport {
				return isStdlibPath(path)
			}
		}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.StructType,
		*ast.InterfaceType, *ast.FuncType:
		// Conversions to composite types, e.g. []byte(s) or map[string]int(m).
		return true
	}
	return false
}

// isStdlibPath reports whether an import path belongs to the Go standard
// library. Standard-library paths have no dot in their first segment (e.g.
// "fmt", "path/filepath"), whereas module paths do ("github.com/...").
func isStdlibPath(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return !strings.Contains(first, ".")
}
