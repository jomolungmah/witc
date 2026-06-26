# Spec: Cross-File Call Graph Aggregation

## Overview
Build a package-level call graph by aggregating calls across all source files in a package. Currently, `CallGraph` only tracks calls within individual files. Need to merge results from multiple files and create a unified view of function relationships across the entire package.

## Files to Create/Modify

### 1. `/home/magnus/work/ai-suite/witc/internal/processor/go/callgraph_aggregate.go` (NEW)
- Implement `Aggregate()` function to combine call graphs from multiple results
- Handle duplicate function names across files
- Build cross-file caller/callee relationships

### 2. `/home/magnus/work/ai-suite/witc/internal/formatter/markdown.go`
- Add call graph summary section showing cross-file dependencies
- Display functions that span multiple files

### 3. `/home/magnus/work/ai-suite/witc/internal/formatter/json.go`
- Ensure `CallGraph` is properly marshaled in JSON output
- Consider adding aggregated statistics at package level

## Detailed Implementation

### callgraph_aggregate.go - New File

```go
package goparser

import (
    "github.com/ai-suite/witc/internal/processor"
)

// CallGraph represents a unified call graph across multiple files.
type CallGraph struct {
    Functions map[string]*FuncInfo // function name -> info
    Edges     []Edge               // caller->callee relationships
}

// FuncInfo contains information about a function in the call graph.
type FuncInfo struct {
    Name       string   // Function name
    Package    string   // Import path of the package
    Files      []string // Source files where this function appears
    Callers    []Caller // Functions that call this one
    Callees    []Callee // Functions this one calls
}

// Caller represents a caller relationship.
type Caller struct {
    Name       string
    File       string
    Line       int
    ParentFunc string
}

// Callee represents a callee relationship.
type Callee struct {
    Name       string
    File       string
    Line       int
    ParentFunc string
}

// Aggregate builds a cross-file call graph from multiple processor results.
func Aggregate(results []*processor.Result) *CallGraph {
    cg := &CallGraph{
        Functions: make(map[string]*FuncInfo),
        Edges:     []Edge{},
    }
    
    for _, r := range results {
        if r == nil || r.CallGraph == nil {
            continue
        }
        
        // Process each function in the result
        for funcName, calls := range r.CallGraph {
            // Initialize function info if not exists
            if _, ok := cg.Functions[funcName]; !ok {
                cg.Functions[funcName] = &FuncInfo{
                    Name:    funcName,
                    Package: r.ImportPath,
                    Files:   []string{},
                    Callers: []Caller{},
                    Callees: []Callee{},
                }
            }
            
            // Add file to the function's list if not already present
            for _, call := range calls {
                cg.Functions[funcName].addFileIfNew(call.File)
                
                // Create callee edge
                cg.Functions[funcName].Callees = append(cg.Functions[funcName].Callees, Callee{
                    Name:       call.CalleeName,
                    File:       call.File,
                    Line:       call.Line,
                    ParentFunc: call.ParentFunc,
                })
                
                // Create edge for the graph
                cg.Edges = append(cg.Edges, Edge{
                    Caller:   funcName,
                    Callee:   call.CalleeName,
                    File:     call.File,
                    Line:     call.Line,
                })
            }
        }
    }
    
    return cg
}

// Helper methods for FuncInfo
func (fi *FuncInfo) addFileIfNew(file string) {
    if file == "" {
        return
    }
    for _, f := range fi.Files {
        if f == file {
            return
        }
    }
    fi.Files = append(fi.Files, file)
}

// Edge represents a caller->callee relationship.
type Edge struct {
    Caller string
    Callee string
    File   string
    Line   int
}
```

### go.go - Update Process Method to Collect Across Files

The main `cmd/witc/main.go` already iterates over files and merges results. We need to add a step after merging to build the aggregated call graph:

```go
// In runSummarize function, after merging all package results:
callGraph := goparser.Aggregate(results)
sum.CallGraph = callGraph // Add to Summary struct if needed
```

### Update Result Structure (if needed)

May need to add call graph field to the main `Result` or keep aggregation separate. Let's keep it separate in the aggregate package for clarity.

## Testing Requirements

### Unit Tests
**File:** `internal/processor/go/callgraph_aggregate_test.go`

```go
func TestAggregate_MergesMultipleFiles(t *testing.T) {
    result1 := &processor.Result{
        ImportPath: "pkg",
        CallGraph: map[string][]processor.CallInfo{
            "helper": {{CallerName: "Process", CalleeName: "fmt.Println", File: "file1.go", Line: 5}},
        },
    }
    
    result2 := &processor.Result{
        ImportPath: "pkg",
        CallGraph: map[string][]processor.CallInfo{
            "helper": {{CallerName: "Main", CalleeName: "helper", File: "file2.go", Line: 10}},
        },
    }
    
    cg := Aggregate([]*processor.Result{result1, result2})
    
    if len(cg.Edges) != 2 {
        t.Errorf("expected 2 edges, got %d", len(cg.Edges))
    }
}

func TestAggregate_HandlesDuplicateFunctions(t *testing.T) {
    // Same function defined in multiple files (e.g., methods on same type)
    result1 := &processor.Result{
        ImportPath: "pkg",
        CallGraph: map[string][]processor.CallInfo{
            "Process": {{CallerName: "Main", CalleeName: "Process", File: "file1.go", Line: 5}},
        },
    }
    
    result2 := &processor.Result{
        ImportPath: "pkg",
        CallGraph: map[string][]processor.CallInfo{
            "Process": {{CallerName: "Other", CalleeName: "Process", File: "file2.go", Line: 8}},
        },
    }
    
    cg := Aggregate([]*processor.Result{result1, result2})
    
    // Should merge into single function entry with multiple files
    if len(cg.Functions["Process"].Files) != 2 {
        t.Errorf("expected 2 files for Process, got %d", len(cg.Functions["Process"].Files))
    }
}

func TestAggregate_EmptyResults(t *testing.T) {
    cg := Aggregate([]*processor.Result{})
    
    if cg == nil {
        t.Fatal("expected non-nil call graph for empty results")
    }
    if len(cg.Functions) != 0 {
        t.Errorf("expected 0 functions, got %d", len(cg.Functions))
    }
}

func TestAggregate_NullResults(t *testing.T) {
    result := &processor.Result{CallGraph: nil}
    
    cg := Aggregate([]*processor.Result{result})
    
    if len(cg.Functions) != 0 {
        t.Errorf("expected 0 functions for null call graph, got %d", len(cg.Functions))
    }
}
```

### Integration Tests
Test aggregation on the witc project itself:
- Verify that `Process()` in `cmd/witc/main.go` correctly calls functions from `internal/processor/go/`
- Check cross-file call tracking between scanner and processor packages

## Acceptance Criteria

- [ ] `Aggregate()` function successfully combines multiple `*processor.Result` into single `*CallGraph`
- [ ] Functions defined across multiple files are merged correctly
- [ ] Call edges track the correct source file and line for each call
- [ ] Empty or nil results are handled gracefully
- [ ] All unit tests pass
- [ ] Integration test on real codebase works

## Expected Output Example

**Markdown Summary:**
```markdown
### Function: Process
**Defined in:** `cmd/witc/main.go`, `internal/server/server.go`
**Calls:**
  - helper() at main.go:15
  - fmt.Println() at server.go:42
**Called By:**
  - main() at main.go:20
```

**JSON Output:**
```json
{
  "CallGraph": {
    "Functions": {
      "Process": {
        "Name": "Process",
        "Package": ".",
        "Files": ["main.go", "server.go"],
        "Callees": [
          {"Name": "helper", "File": "main.go", "Line": 15},
          {"Name": "fmt.Println", "File": "server.go", "Line": 42}
        ]
      }
    },
    "Edges": [
      {"Caller": "Process", "Callee": "helper", "File": "main.go", "Line": 15},
      {"Caller": "Process", "Callee": "fmt.Println", "File": "server.go", "Line": 42}
    ]
  }
}
```

## Dependencies
- Uses existing `processor.Result` and `CallInfo` types
- No external dependencies beyond standard library

## Notes for Reviewer
- Consider thread-safety if aggregation happens concurrently
- Evaluate performance for large codebases with many files
- Decide whether to store aggregated graph in Summary or keep separate
- Consider deduplication logic for identical calls across files
