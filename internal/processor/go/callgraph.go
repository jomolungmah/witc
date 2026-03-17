package goparser

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
)

// CallInfo contains information about a function call.
type CallInfo struct {
	CallerName string // The name of the code that makes the call
	CalleeName string // The name/function being called
	File       string // Source file path where call occurs
	Line       int    // Line number in source
	Column     int    // Column number in source
}

// CallVisitor traverses Go AST nodes and identifies all function calls.
type CallVisitor struct {
	Calls map[string][]CallInfo // Map of callee name to list of CallInfo
}

// NewCallVisitor creates a new CallVisitor instance.
func NewCallVisitor() *CallVisitor {
	return &CallVisitor{
		Calls: make(map[string][]CallInfo),
	}
}

// Visit implements ast.Visitor interface for traversing AST nodes.
func (v *CallVisitor) Visit(node ast.Node) ast.Visitor {
	if callExpr, ok := node.(*ast.CallExpr); ok {
		v.processCallExpr(callExpr)
	}
	return v
}

// processCallExpr extracts call information from a CallExpr node.
func (v *CallVisitor) processCallExpr(callExpr *ast.CallExpr) {
	callerName := ""
	if fn, ok := callExpr.Fun.(*ast.FuncLit); ok {
		// Function literal being called - use its position as caller name
		fileSet := token.NewFileSet()
		pos := fileSet.Position(fn.Pos())
		callerName = fmt.Sprintf("anonymous_function:%s:%d", pos.Filename, pos.Line)
	} else if ident, ok := callExpr.Fun.(*ast.Ident); ok {
		callerName = ident.Name
	}

	calleeName, file, line, column := v.extractCallee(callExpr)

	if calleeName != "" {
		v.Calls[calleeName] = append(v.Calls[calleeName], CallInfo{
			CallerName: callerName,
			CalleeName: calleeName,
			File:       file,
			Line:       line,
			Column:     column,
		})
	}
}

// extractCallee extracts the called function name and location from different call expression types.
func (v *CallVisitor) extractCallee(callExpr *ast.CallExpr) (calleeName string, file string, line int, column int) {
	fset := token.NewFileSet()

	switch fn := callExpr.Fun.(type) {
	case *ast.Ident:
		// Simple identifier: foo() or method().method2()
		calleeName = fn.Name
	case *ast.SelectorExpr:
		// Selector expression: pkg.Func() or obj.Method()
		if ident, ok := fn.X.(*ast.Ident); ok {
			calleeName = ident.Name + "." + fn.Sel.Name
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
