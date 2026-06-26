# CallGraph Implementation Plan

## Current Status

✅ **Core integration complete**
- `CallInfo` type added to `processor.Result`
- `CallVisitor` integrated into `Processor.Process()`
- Markdown formatter shows function calls
- JSON output includes call graph data

⚠️ **Issues identified**
- Location tracking is incomplete (file/line often empty or 0)
- Caller identification needs improvement
- CallGraph aggregation across files not working correctly

---

## Phase 1: Fix Location Tracking

### Problem
Currently `extractCallee()` returns location from the call expression, but this loses context about where in the file the call occurs.

### Solution

**File:** `internal/processor/go/callgraph.go`

```go
// Update CallInfo to include caller function context
type CallInfo struct {
    CallerName string // Function name that contains the call
    CalleeName string // Function being called
    File       string // Source file path
    Line       int    // Line number
    Column     int    // Column number
    ParentFunc string // Name of parent function/method containing this call
}
```

**Changes needed:**
1. Track current function context during AST traversal
2. Pass parent function name to `processCallExpr()`
3. Store parent function in `CallInfo`

---

## Phase 2: Fix Caller Identification

### Problem
Current implementation identifies callers as simple identifiers, but doesn't properly track which function contains each call.

### Solution

**File:** `internal/processor/go/go.go`

```go
// Enhanced AST traversal with context tracking
func (p *Processor) Process(ctx context.Context, path string, src []byte) (*processor.Result, error) {
    // ... existing code ...
    
    callVisitor := NewCallVisitor(path) // Pass file path
    ast.Inspect(f, func(n ast.Node) bool {
        if fn, ok := n.(*ast.FuncDecl); ok {
            visitor := &CallVisitor{path: path}
            ast.Inspect(fn.Body, visitor)
            callVisitor.Merge(visitor.Calls)
        }
        return true
    })
    
    result.CallGraph = callVisitor.BuildResult()
    return result, nil
}
```

---

## Phase 3: Cross-File Call Graph Aggregation

### Problem
CallGraph currently only tracks calls within a single file. Need to aggregate across all files in a package.

### Solution

**New File:** `internal/processor/go/callgraph_aggregate.go`

```go
// Aggregate builds a cross-file call graph from multiple results
func Aggregate(results []*processor.Result) *CallGraph {
    cg := &CallGraph{
        Functions: make(map[string]*FuncInfo),
        Edges:     []Edge{},
    }
    
    for _, r := range results {
        for funcName, calls := range r.CallGraph {
            if _, ok := cg.Functions[funcName]; !ok {
                cg.Functions[funcName] = &FuncInfo{
                    Name:       funcName,
                    Package:    r.ImportPath,
                    Callers:    []string{},
                    Callees:    []string{},
                }
            }
            
            for _, call := range calls {
                cg.Functions[funcName].Callees = append(cg.Functions[funcName].Callees, call.CalleeName)
                // Track reverse edges too
            }
        }
    }
    
    return cg
}
```

---

## Phase 4: Enhanced Output Formats

### Markdown Improvements

**File:** `internal/formatter/markdown.go`

Add new sections:
1. **Call Graph Summary** - Top-level view of function relationships
2. **Dependency Analysis** - Show which functions call each other across packages
3. **Entry Points** - Highlight main, exported functions as entry points

```markdown
## Call Graph

### Function Relationships

| Function | Calls | Called By |
|----------|-------|-----------|
| `main` | 5 | - |
| `Process` | 2 | `main`, `TestProcess` |
| `helper` | 0 | `Process` |
```

### JSON Enhancements

Add new top-level fields to Summary:
```json
{
  "CallGraphSummary": {
    "totalFunctions": 15,
    "totalCalls": 42,
    "entryPoints": ["main", "NewService"],
    "leafFunctions": ["helper", "formatExpr"]
  },
  "Packages": { ... }
}
```

---

## Phase 5: Call Graph Metrics

### Metrics to Calculate

**File:** `internal/processor/go/callgraph_metrics.go`

1. **Cyclomatic Complexity Estimate** - Count branches in call chains
2. **Coupling Score** - Number of external calls per function
3. **Fan-in/Fan-out** - How many functions call/receive from each function
4. **Call Depth** - Maximum nesting depth of function calls

```go
type Metrics struct {
    MaxCallDepth     int
    AvgCallersPerFunc float64
    AvgCalleesPerFunc float64
    MaxFanIn         string // Function with most callers
    MaxFanOut        string // Function with most callees
}
```

---

## Phase 6: LLM-Friendly Output

### Natural Language Summaries

Generate human-readable descriptions of call patterns:

```markdown
### Call Flow Summary

The `Process` function is called by 2 other functions. It calls:
- `helper()` - utility function (no dependencies)
- `fmt.Println()` - standard library

Call depth: 2 levels maximum.
```

---

## Implementation Order

1. **Phase 1** - Fix location tracking (highest priority)
2. **Phase 2** - Fix caller identification 
3. **Phase 3** - Cross-file aggregation
4. **Phase 4** - Enhanced output formats
5. **Phase 5** - Metrics calculation
6. **Phase 6** - LLM-friendly summaries

---

## Testing Strategy

### Unit Tests
- `TestCallGraph_TracksLocations` - Verify file/line info correct
- `TestCallGraph_IdentifiesCallers` - Verify parent function tracking
- `TestAggregate_CrossFileCalls` - Verify aggregation works
- `TestMetrics_CalculatesCorrectly` - Verify metric calculations

### Integration Tests
- Test on real codebases (e.g., witc itself)
- Verify output quality in markdown/JSON formats
- Compare call graph against actual source code

---

## Performance Considerations

- **Memory:** CallGraph can grow large for big codebases
  - Solution: Add size limits, optional aggregation
- **Speed:** AST traversal is O(n) per file
  - Solution: Parallel processing of files (future enhancement)
- **Output Size:** Full call graph can be verbose
  - Solution: Provide summary-only mode via flag

---

## Future Enhancements

1. **Call Direction** - Show both caller→callee and callee←caller views
2. **Type Analysis** - Track interface method calls vs concrete implementations
3. **Package Dependencies** - Cross-package call graph visualization
4. **Export to Graphviz** - Generate `.dot` files for visualization
5. **Incremental Updates** - Only reprocess changed files
