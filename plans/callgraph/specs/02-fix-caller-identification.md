# Spec: Fix Caller Identification in CallGraph

## Overview
Fix the issue where caller identification is incomplete or incorrect. Currently, callers are identified as simple identifiers but don't properly track which function contains each call. Need to improve identification of complex call patterns like method calls, anonymous functions, and nested calls.

## Files to Modify

### 1. `/home/magnus/work/jomolungmah/witc/internal/processor/go/callgraph.go`
- Enhance `CallerName` extraction logic in `processCallExpr()`
- Handle method receivers properly (e.g., `obj.Method()`)
- Improve handling of anonymous function calls

### 2. `/home/magnus/work/jomolungmah/witc/internal/processor/go/go.go`
- Update integration to use improved caller identification
- Ensure consistent naming across different call types

## Detailed Changes

### callgraph.go - processCallExpr Method

**Current logic:**
```go
callerName := ""
if fn, ok := callExpr.Fun.(*ast.FuncLit); ok {
    fileSet := token.NewFileSet()
    pos := fileSet.Position(fn.Pos())
    callerName = fmt.Sprintf("anonymous_function:%s:%d", pos.Filename, pos.Line)
} else if ident, ok := callExpr.Fun.(*ast.Ident); ok {
    callerName = ident.Name
}
```

**Enhanced logic:**
```go
callerName := v.extractCaller(callExpr)

func (v *CallVisitor) extractCaller(callExpr *ast.CallExpr) string {
    switch fn := callExpr.Fun.(type) {
    case *ast.FuncLit:
        // Anonymous function - use position as identifier
        fileSet := token.NewFileSet()
        pos := fileSet.Position(fn.Pos())
        return fmt.Sprintf("anonymous_function:%s:%d", filepath.Base(pos.Filename), pos.Line)
    
    case *ast.Ident:
        // Simple identifier: foo()
        return fn.Name
    
    case *ast.SelectorExpr:
        // Method or package function call: obj.Method() or pkg.Func()
        if ident, ok := fn.X.(*ast.Ident); ok {
            // Could be method call on variable or package qualified
            // For caller identification, use the selector as a whole
            var buf bytes.Buffer
            printer.Fprint(&buf, token.NewFileSet(), fn)
            return buf.String()
        }
        return fn.Sel.Name
    
    case *ast.CallExpr:
        // Nested call: func()() or foo().bar()
        // Use the innermost function identifier
        return v.extractCaller(fn)
    
    default:
        // Fallback - stringify the expression
        var buf bytes.Buffer
        printer.Fprint(&buf, token.NewFileSet(), fn)
        return buf.String()
    }
}
```

### callgraph.go - extractCallee Method Updates

The `extractCallee` method already handles various cases. Enhance it to better distinguish between:
- **Package functions:** `fmt.Println`, `strings.Split`
- **Method calls:** `obj.Method()`, `(*T).Method()`
- **Interface methods:** `reader.Read()`

**Enhanced callee naming:**
```go
func (v *CallVisitor) extractCallee(callExpr *ast.CallExpr) (calleeName string, file string, line int, column int) {
    fset := token.NewFileSet()
    
    switch fn := callExpr.Fun.(type) {
    case *ast.Ident:
        calleeName = fn.Name
    
    case *ast.SelectorExpr:
        if ident, ok := fn.X.(*ast.Ident); ok {
            // Determine if this is a method or package function
            calleeName = ident.Name + "." + fn.Sel.Name
            
            // Check receiver type to distinguish methods from packages
            // This requires more context from the AST
        } else if sel, ok := fn.X.(*ast.SelectorExpr); ok {
            // Chained selectors: pkg.Sub.Method()
            calleeName = sel.Sel.Name + "." + fn.Sel.Name
        } else {
            // Complex expression like slice[0].method()
            calleeName = fn.Sel.Name
        }
    
    // ... rest of existing cases remain the same
    }
    
    pos := fset.Position(callExpr.Pos())
    return calleeName, pos.Filename, pos.Line, pos.Column
}
```

## Testing Requirements

### Unit Tests to Add/Update
**File:** `internal/processor/go/callgraph_test.go`

```go
func TestCallerIdentification_SimpleFunction(t *testing.T) {
    src := `
package pkg
    
func Process() {
    helper()
}

func helper() {}
`
    // Verify caller is identified as "Process" (ParentFunc) and callee as "helper"
}

func TestCallerIdentification_MethodCall(t *testing.T) {
    src := `
package pkg
    
type Calculator struct{}

func (c *Calculator) Process() {
    c.add(1, 2)
}

func (c *Calculator) add(a, b int) int { return a + b }
`
    // Verify callee is identified as "add" and parent function as "Process"
}

func TestCallerIdentification_PackageFunction(t *testing.T) {
    src := `
package pkg
    
import "fmt"

func Process() {
    fmt.Println("hello")
}
`
    // Verify callee includes package: "fmt.Println"
}

func TestCallerIdentification_AnonymousCall(t *testing.T) {
    src := `
package pkg
    
func Process() {
    f := func() { helper() }
    f()
}

func helper() {}
`
    // Verify anonymous function is identified by position
}

func TestCallerIdentification_ChainedMethod(t *testing.T) {
    src := `
package pkg
    
import "strings"

func Process() {
    strings.Trim(" test ", " ").Upper()
}
`
    // Verify chained calls are handled correctly
}
```

## Acceptance Criteria

- [ ] Simple function calls: caller = containing function, callee = function name
- [ ] Method calls: callee includes receiver info (e.g., `obj.Method`)
- [ ] Package functions: callee includes package prefix (e.g., `fmt.Println`)
- [ ] Anonymous functions: identified by filename:line format
- [ ] Chained calls: properly flattened or nested representation
- [ ] All existing tests still pass
- [ ] New unit tests added and passing

## Expected Output Examples

**Simple function:**
```json
{
  "CallerName": "Process",
  "CalleeName": "helper",
  "ParentFunc": "Process"
}
```

**Method call:**
```json
{
  "CallerName": "Process",
  "CalleeName": "Calculator.add",
  "ParentFunc": "Process"
}
```

**Package function:**
```json
{
  "CallerName": "Process",
  "CalleeName": "fmt.Println",
  "ParentFunc": "Process"
}
```

## Dependencies
- No external dependencies
- Uses existing `go/ast`, `go/parser`, `go/token`, `go/printer` packages

## Notes for Reviewer
- Ensure backward compatibility with existing callgraph format
- Consider performance impact of string formatting in extractCaller
- Verify method receiver detection works correctly (may require type information)
- Test edge cases: function literals, type assertions, type switches
