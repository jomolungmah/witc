# Spec: Fix Location Tracking in CallGraph

## Overview
Fix the issue where `CallInfo.File` and `CallInfo.Line` are empty or zero when tracking function calls. Currently the location information is lost during AST traversal.

## Files to Modify

### 1. `/home/magnus/work/jomolungmah/witc/internal/processor/go/callgraph.go`
- Update `CallInfo` struct to include `ParentFunc` field
- Modify `processCallExpr()` to accept parent function context
- Ensure file/line information is preserved from the AST position

### 2. `/home/magnus/work/jomolungmah/witc/internal/processor/go/go.go`
- Update `Process()` method to track current function during AST traversal
- Pass parent function name to call visitor
- Convert local `CallInfo` to `processor.CallInfo` with correct file paths

## Detailed Changes

### callgraph.go - CallInfo Struct

**Current:**
```go
type CallInfo struct {
    CallerName string // The name of the code that makes the call
    CalleeName string // The name/function being called
    File       string // Source file path where call occurs
    Line       int    // Line number in source
    Column     int    // Column number in source
}
```

**New:**
```go
type CallInfo struct {
    CallerName string // The name of the code that makes the call (e.g., "append")
    CalleeName string // The name/function being called
    File       string // Source file path where call occurs
    Line       int    // Line number in source
    Column     int    // Column number in source
    ParentFunc string // Name of function/method containing this call (e.g., "Process")
}
```

### callgraph.go - ProcessCallExpr Method

**Current signature:**
```go
func (v *CallVisitor) processCallExpr(callExpr *ast.CallExpr) {
```

**New signature:**
```go
func (v *CallVisitor) processCallExpr(callExpr *ast.CallExpr, parentFunc string) {
```

**Changes needed:**
1. Add `parentFunc` parameter to method signature
2. Store `parentFunc` in the created `CallInfo` struct:
   ```go
   v.Calls[calleeName] = append(v.Calls[calleeName], CallInfo{
       CallerName: callerName,
       CalleeName: calleeName,
       File:       file,
       Line:       line,
       Column:     column,
       ParentFunc: parentFunc,
   })
   ```

### go.go - Process Method

**Current approach:**
```go
callVisitor := NewCallVisitor()
ast.Walk(callVisitor, f)
```

**New approach:**
```go
// Track current function during traversal
var currentFunc string
var fileDir string // Get from path parameter

// First pass: find all function declarations and their bodies
for _, decl := range f.Decls {
    if fn, ok := decl.(*ast.FuncDecl); ok {
        currentFunc = fn.Name.Name
        callVisitor := NewCallVisitor()
        
        // Walk the function body with context
        ast.Inspect(fn.Body, func(n ast.Node) bool {
            if callExpr, ok := n.(*ast.CallExpr); ok {
                callVisitor.processCallExpr(callExpr, currentFunc)
            }
            return true
        })
        
        // Merge calls into main visitor
        for callee, calls := range callVisitor.Calls {
            for _, c := range calls {
                resultCalls[callee] = append(resultCalls[callee], c)
            }
        }
    }
}

result.CallGraph = convertToProcessorCallInfo(resultCalls, filepath.Dir(path))
```

## Testing Requirements

### Unit Tests to Add
**File:** `internal/processor/go/callgraph_test.go` (create new file)

```go
func TestCallInfo_TracksParentFunction(t *testing.T) {
    src := `
package pkg
    
func Outer() {
    Inner()
}

func Inner() {}
`
    fset := token.NewFileSet()
    f, _ := parser.ParseFile(fset, "test.go", src, 0)
    
    var calls []CallInfo
    var currentFunc string
    
    for _, decl := range f.Decls {
        if fn, ok := decl.(*ast.FuncDecl); ok {
            currentFunc = fn.Name.Name
            visitor := NewCallVisitor()
            
            ast.Inspect(fn.Body, func(n ast.Node) bool {
                if callExpr, ok := n.(*ast.CallExpr); ok {
                    visitor.processCallExpr(callExpr, currentFunc)
                }
                return true
            })
            
            for _, c := range visitor.Calls["Inner"] {
                calls = append(calls, c)
            }
        }
    }
    
    if len(calls) != 1 {
        t.Fatalf("expected 1 call, got %d", len(calls))
    }
    
    if calls[0].ParentFunc != "Outer" {
        t.Errorf("ParentFunc = %q, want Outer", calls[0].ParentFunc)
    }
}

func TestCallInfo_TracksLocation(t *testing.T) {
    src := `
package pkg
    
func Process() {
    helper() // line 4
    other()  // line 5
}

func helper() {}
func other() {}
`
    fset := token.NewFileSet()
    f, _ := parser.ParseFile(fset, "test.go", src, 0)
    
    // Verify Line and Column are non-zero for calls
    // Implementation depends on how extractCallee captures positions
}
```

## Acceptance Criteria

- [ ] `CallInfo.ParentFunc` is populated with the containing function name
- [ ] `CallInfo.File` contains the correct file path (directory from source path)
- [ ] `CallInfo.Line` and `CallInfo.Column` are non-zero for all calls
- [ ] All existing tests still pass
- [ ] New unit tests added and passing

## Expected Output Example

**Before:**
```json
{
  "CallerName": "helper",
  "CalleeName": "fmt.Println",
  "File": "",
  "Line": 0,
  "Column": 0
}
```

**After:**
```json
{
  "CallerName": "helper",
  "CalleeName": "fmt.Println",
  "File": "/path/to/file.go",
  "Line": 15,
  "Column": 8,
  "ParentFunc": "Process"
}
```

## Dependencies
- No external dependencies
- Uses existing `go/ast`, `go/parser`, `go/token` packages

## Notes for Reviewer
- Ensure file path handling is consistent (relative vs absolute)
- Check that anonymous functions are handled correctly (use function literal position as caller name per existing code)
- Verify no regression in existing functionality
