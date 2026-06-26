# Spec: Enhanced Output Formats - Call Graph Display

## Overview
Enhance both Markdown and JSON output formats to better display call graph information. Add structured sections showing function relationships, dependencies, and summary statistics that are useful for LLM coding agents understanding codebase architecture.

## Files to Modify

### 1. `/home/magnus/work/jomolungmah/witc/internal/formatter/markdown.go`
- Add "Call Graph" section after Packages section
- Create function relationship table showing callers/callees
- Highlight entry points and leaf functions

### 2. `/home/magnus/work/jomolungmah/witc/internal/formatter/json.go`
- Ensure `CallGraph` data is properly serialized
- Add summary statistics object at top level

### 3. `/home/magnus/work/jomolungmah/witc/internal/formatter/formatter.go`
- Extend `Summary` struct to include call graph metadata if needed

## Detailed Changes

### markdown.go - New Call Graph Section

**Location:** After the "Packages" section, before the closing of `Markdown()` function

```go
// Add this after the Packages loop and before return b.String()
b.WriteString("\n## Call Graph\n\n")

if r.CallGraph != nil && len(r.CallGraph) > 0 {
    // Function Relationship Table
    b.WriteString("### Function Relationships\n\n")
    b.WriteString("| Function | Calls | Called By |\n")
    b.WriteString("|----------|-------|-----------|\n")
    
    for funcName := range r.Functions {
        calls := []string{}
        if callList, ok := r.CallGraph[funcName]; ok {
            for _, c := range callList {
                // Deduplicate callee names
                called := true
                for _, existing := range calls {
                    if existing == c.CalleeName {
                        called = false
                        break
                    }
                }
                if called {
                    calls = append(calls, c.CalleeName)
                }
            }
        }
        
        callers := []string{}
        // Find functions that call this one
        for otherFunc, callList := range r.CallGraph {
            for _, c := range callList {
                if c.CalleeName == funcName {
                    callerFound := true
                    for _, existing := range callers {
                        if existing == otherFunc {
                            callerFound = false
                            break
                        }
                    }
                    if callerFound {
                        callers = append(callers, otherFunc)
                    }
                }
            }
        }
        
        callsStr := strings.Join(calls, ", ")
        callersStr := strings.Join(callers, ", ")
        b.WriteString(fmt.Sprintf("| `%s` | %d | %s |\n", funcName, len(calls), callersStr))
    }
    
    // Entry Points Section
    b.WriteString("\n### Entry Points\n\n")
    entryPoints := findEntryPoints(r)
    if len(entryPoints) > 0 {
        for _, ep := range entryPoints {
            b.WriteString(fmt.Sprintf("- `%s`\n", ep))
        }
    } else {
        b.WriteString("_No clear entry points detected_\n")
    }
    
    // Leaf Functions Section (functions that don't call anything)
    b.WriteString("\n### Leaf Functions\n\n")
    leafFuncs := findLeafFunctions(r)
    if len(leafFuncs) > 0 {
        for _, lf := range leafFuncs {
            b.WriteString(fmt.Sprintf("- `%s`\n", lf))
        }
    } else {
        b.WriteString("_No leaf functions detected_\n")
    }
} else {
    b.WriteString("*No call graph data available*\n")
}
```

**Helper Functions:**

```go
func findEntryPoints(r *processor.Result) []string {
    // Entry points: main, exported functions (capitalized), or functions with 0 callers
    var entries []string
    
    for _, fn := range r.Functions {
        if fn.Name == "main" || isExported(fn.Name) {
            entries = append(entries, fn.Name)
        }
    }
    
    // Also include functions that are called but don't call anything (potential entry points)
    for funcName := range r.CallGraph {
        calls, ok := r.CallGraph[funcName]
        if !ok || len(calls) == 0 {
            // Check if it's not a private function
            if isExported(funcName) {
                entries = append(entries, funcName)
            }
        }
    }
    
    return deduplicateStrings(entries)
}

func findLeafFunctions(r *processor.Result) []string {
    // Leaf functions: functions that don't call anything else (excluding stdlib)
    var leaves []string
    
    for _, fn := range r.Functions {
        if calls, ok := r.CallGraph[fn.Name]; !ok || len(calls) == 0 {
            leaves = append(leaves, fn.Name)
        }
    }
    
    return deduplicateStrings(leaves)
}

func isExported(name string) bool {
    if len(name) == 0 {
        return false
    }
    return name[0] >= 'A' && name[0] <= 'Z'
}

func deduplicateStrings(s []string) []string {
    seen := make(map[string]bool)
    var result []string
    
    for _, str := range s {
        if !seen[str] {
            seen[str] = true
            result = append(result, str)
        }
    }
    
    return result
}
```

### json.go - Ensure Proper Serialization

**Current:** Already uses `json.MarshalIndent(sum, "", "  ")` which will automatically serialize the `CallGraph` field if it exists in Summary.

**No changes needed** for basic serialization, but we can add custom JSON types for better formatting:

```go
// Optional enhancement - create a CallGraphSummary type
type CallGraphSummary struct {
    TotalFunctions int     `json:"totalFunctions"`
    TotalCalls     int     `json:"totalCalls"`
    EntryPoints    []string `json:"entryPoints"`
    LeafFunctions  []string `json:"leafFunctions"`
}

// In Markdown or create new JSON function:
func addCallGraphSummary(sum *Summary) CallGraphSummary {
    // Calculate statistics across all packages
    totalFuncs := 0
    totalCalls := 0
    
    for _, pkg := range sum.Packages {
        totalFuncs += len(pkg.Functions)
        for _, calls := range pkg.CallGraph {
            totalCalls += len(calls)
        }
    }
    
    return CallGraphSummary{
        TotalFunctions: totalFuncs,
        TotalCalls:     totalCalls,
    }
}
```

### formatter.go - Optional Summary Enhancement

If we want to include call graph summary at the top level of Summary:

```go
type Summary struct {
    Root          string
    Paths         []string
    Packages      map[string]*processor.Result
    NoStructure   bool
    CallGraphInfo *CallGraphSummary // Optional field for aggregated stats
}
```

## Testing Requirements

### Unit Tests
**File:** `internal/formatter/markdown_test.go` (add to existing file)

```go
func TestMarkdown_CallGraphSection(t *testing.T) {
    sum := &Summary{
        Root: "/test",
        Packages: map[string]*processor.Result{
            "pkg": {
                Package: "pkg",
                Functions: []processor.Function{
                    {Name: "main"},
                    {Name: "Process"},
                    {Name: "helper"},
                },
                CallGraph: map[string][]processor.CallInfo{
                    "main": {{CallerName: "", CalleeName: "Process", File: "test.go", Line: 1}},
                    "Process": {{CallerName: "main", CalleeName: "helper", File: "test.go", Line: 2}},
                },
            },
        },
    }
    
    md, err := Markdown(sum)
    if err != nil {
        t.Fatalf("Markdown() error = %v", err)
    }
    
    // Check for call graph sections
    if !strings.Contains(md, "## Call Graph") {
        t.Error("Expected '## Call Graph' section in output")
    }
    if !strings.Contains(md, "### Function Relationships") {
        t.Error("Expected '### Function Relationships' section")
    }
}

func TestMarkdown_EntryPoints(t *testing.T) {
    // Test that exported functions are identified as entry points
}

func TestMarkdown_LeafFunctions(t *testing.T) {
    // Test that functions with no callees are identified as leaf functions
}
```

### Integration Tests
Test on witc project:
- Verify "main" function is listed as entry point in `cmd/witc` package
- Check that standard library calls (fmt.Println, etc.) are handled correctly

## Acceptance Criteria

- [ ] Markdown output includes "Call Graph" section after Packages
- [ ] Function relationship table shows callers and callees for each function
- [ ] Entry points section identifies main/exported functions
- [ ] Leaf functions section identifies functions with no calls
- [ ] JSON output properly serializes CallGraph data
- [ ] All existing tests still pass
- [ ] New unit tests added and passing

## Expected Output Examples

### Markdown Example
```markdown
## Packages

### cmd/witc

- `func main()`
- `func runSummarize(*cobra.Command, []string) error`

## Call Graph

### Function Relationships

| Function | Calls | Called By |
|----------|-------|-----------|
| `main` | 2 | - |
| `runSummarize` | 3 | `main` |
| `mergeResults` | 1 | `runSummarize` |

### Entry Points

- `main`

### Leaf Functions

- `mergeResults`
```

### JSON Example
```json
{
  "Root": "/home/magnus/work/jomolungmah/witc",
  "Packages": {
    ".": {
      "Package": "goparser",
      "Functions": [...],
      "CallGraph": {
        "append": [
          {"CallerName": "append", "CalleeName": "append", ...}
        ]
      }
    }
  }
}
```

## Dependencies
- Uses existing `processor.Result` and `CallInfo` types
- No external dependencies

## Notes for Reviewer
- Consider performance of caller/callee lookup (O(n²) in worst case)
- Evaluate if deduplication is necessary or helpful
- Check that table formatting aligns properly in markdown renderers
- Consider adding configuration option to disable call graph section for large codebases
