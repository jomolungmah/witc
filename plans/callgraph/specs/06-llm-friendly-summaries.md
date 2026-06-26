# Spec: LLM-Friendly Call Graph Summaries

## Overview
Generate natural language summaries of call patterns and relationships that are optimized for LLM understanding. Transform structured call graph data into human-readable descriptions that help coding agents understand codebase architecture, dependencies, and execution flow.

## Files to Create/Modify

### 1. `/home/magnus/work/jomolungmah/witc/internal/formatter/callgraph_summary.go` (NEW)
- Implement natural language generation for call patterns
- Generate summaries of function relationships
- Create "flow" descriptions showing how code executes

### 2. `/home/magnus/work/jomolungmah/witc/internal/formatter/markdown.go`
- Add "Call Flow Summary" section with natural language descriptions
- Integrate generated summaries into existing output

## Detailed Implementation

### callgraph_summary.go - New File

```go
package formatter

import (
    "github.com/jomolungmah/witc/internal/processor"
    "strings"
)

// GenerateCallSummary creates a natural language summary of the call graph.
func GenerateCallSummary(packages map[string]*processor.Result) string {
    var b strings.Builder
    
    // High-level overview
    totalFuncs := 0
    totalCalls := 0
    entryPoints := []string{}
    
    for _, pkg := range packages {
        if pkg == nil {
            continue
        }
        totalFuncs += len(pkg.Functions)
        
        for funcName, calls := range pkg.CallGraph {
            totalCalls += len(calls)
            
            // Identify entry points (functions with no callers or main function)
            hasCallers := false
            for otherFunc, otherCalls := range pkg.CallGraph {
                for _, call := range otherCalls {
                    if call.CalleeName == funcName {
                        hasCallers = true
                        break
                    }
                }
                if hasCallers {
                    break
                }
            }
            
            if !hasCallers || funcName == "main" {
                entryPoints = append(entryPoints, funcName)
            }
        }
    }
    
    b.WriteString("### Call Flow Summary\n\n")
    
    // Overview paragraph
    if len(entryPoints) > 0 {
        b.WriteString("The codebase has ")
        b.WriteString(formatNumber(totalFuncs))
        b.WriteString(" functions with ")
        b.WriteString(formatNumber(totalCalls))
        b.WriteString(" total calls. Entry points include: ")
        
        for i, ep := range entryPoints {
            if i > 0 {
                b.WriteString(", ")
            }
            b.WriteString("`")
            b.WriteString(ep)
            b.WriteString("`")
        }
        b.WriteString(".\n\n")
    } else {
        b.WriteString("The codebase contains ")
        b.WriteString(formatNumber(totalFuncs))
        b.WriteString(" functions with ")
        b.WriteString(formatNumber(totalCalls))
        b.WriteString(" total calls.\n\n")
    }
    
    // Per-package summaries
    for pkgName, pkg := range packages {
        if pkg == nil || len(pkg.Functions) == 0 {
            continue
        }
        
        b.WriteString("**Package `")
        b.WriteString(pkgName)
        b.WriteString("`:**\n\n")
        
        // Generate summary for each function that has calls
        for _, fn := range pkg.Functions {
            if calls, ok := pkg.CallGraph[fn.Name]; ok && len(calls) > 0 {
                b.WriteString(generateFunctionSummary(fn.Name, calls))
            }
        }
        
        // Highlight key functions (entry points in this package)
        var pkgEntryPoints []string
        for _, fn := range pkg.Functions {
            if fn.Name == "main" || isExported(fn.Name) {
                pkgEntryPoints = append(pkgEntryPoints, fn.Name)
            }
        }
        
        if len(pkgEntryPoints) > 0 {
            b.WriteString("\nKey entry points in this package: ")
            for i, ep := range pkgEntryPoints {
                if i > 0 {
                    b.WriteString(", ")
                }
                b.WriteString("`")
                b.WriteString(ep)
                b.WriteString("`")
            }
            b.WriteString(".\n\n")
        } else {
            b.WriteString("\n")
        }
    }
    
    return b.String()
}

// generateFunctionSummary creates a natural language description of a function's call relationships.
func generateFunctionSummary(funcName string, calls []processor.CallInfo) string {
    var b strings.Builder
    
    // Count unique callees
    calleeSet := make(map[string]bool)
    for _, call := range calls {
        calleeSet[call.CalleeName] = true
    }
    
    numCallees := len(calleeSet)
    
    // Start with function name
    b.WriteString("- `")
    b.WriteString(funcName)
    b.WriteString("` ")
    
    if numCallees == 0 {
        b.WriteString("is a leaf function (calls no other functions).\n\n")
        return b.String()
    }
    
    // Describe what it calls
    b.WriteString("calls ")
    b.WriteString(formatNumber(numCallees))
    b.WriteString(" ")
    if numCallees == 1 {
        b.WriteString("other function: ")
    } else {
        b.WriteString("other functions, including: ")
    }
    
    // List up to 3 key callees
    count := 0
    for callee := range calleeSet {
        if count >= 3 {
            b.WriteString(", and others")
            break
        }
        
        if count > 0 {
            b.WriteString(", ")
        }
        b.WriteString("`")
        b.WriteString(callee)
        b.WriteString("`")
        count++
    }
    
    // Add context about call location
    if len(calls) > 0 {
        loc := calls[0]
        file := strings.TrimPrefix(loc.File, "/home/magnus/work/jomolungmah/witc/")
        b.WriteString(" (called at `")
        b.WriteString(file)
        b.WriteString(":")
        b.WriteString(formatNumber(loc.Line))
        b.WriteString("`)")
    }
    
    b.WriteString(".\n\n")
    
    return b.String()
}

// formatNumber formats a number with commas for readability.
func formatNumber(n int) string {
    if n == 0 {
        return "no"
    }
    if n == 1 {
        return "one"
    }
    if n < 1000 {
        return toString(n)
    }
    
    // Simple number to word conversion for small numbers
    words := []string{"", "one", "two", "three", "four", "five", 
                      "six", "seven", "eight", "nine", "ten"}
    if n < len(words) {
        return words[n]
    }
    
    // For larger numbers, just use digits
    return toString(n)
}

func toString(n int) string {
    // Simple implementation - in production would use better number formatting
    if n == 0 {
        return "zero"
    }
    
    result := ""
    switch n {
    case 1:
        result = "one"
    case 2:
        result = "two"
    case 3:
        result = "three"
    case 4:
        result = "four"
    case 5:
        result = "five"
    case 6:
        result = "six"
    case 7:
        result = "seven"
    case 8:
        result = "eight"
    case 9:
        result = "nine"
    case 10:
        result = "ten"
    default:
        return string(rune('0'+n%10)) // Simplified - would use strconv.Itoa in production
    }
    
    return result
}

// isExported checks if a function name is exported (capitalized).
func isExported(name string) bool {
    if len(name) == 0 {
        return false
    }
    return name[0] >= 'A' && name[0] <= 'Z'
}

// GenerateCallFlow describes the execution flow for a specific entry point.
func GenerateCallFlow(entryPoint string, packages map[string]*processor.Result) string {
    var b strings.Builder
    
    b.WriteString("### Execution Flow: `")
    b.WriteString(entryPoint)
    b.WriteString("`**\n\n")
    
    // Find the function and trace its calls (simplified - would need DFS for full flow)
    visited := make(map[string]bool)
    traverseCallFlow(entryPoint, packages, &b, visited, 0)
    
    return b.String()
}

func traverseCallFlow(funcName string, packages map[string]*processor.Result, b *strings.Builder, visited map[string]bool, depth int) {
    if visited[funcName] {
        b.WriteString("   ".repeat(depth))
        b.WriteString("*cycle detected*\n")
        return
    }
    
    visited[funcName] = true
    
    indent := "  ".repeat(depth)
    
    // Find function in packages
    found := false
    for _, pkg := range packages {
        if calls, ok := pkg.CallGraph[funcName]; ok {
            found = true
            
            b.WriteString(indent)
            b.WriteString("- `")
            b.WriteString(funcName)
            b.WriteString("` ")
            
            if len(calls) == 0 {
                b.WriteString("(no outgoing calls)\n\n")
                return
            }
            
            b.WriteString("calls:\n")
            
            for _, call := range calls {
                b.WriteString(indent)
                b.WriteString("  - `")
                b.WriteString(call.CalleeName)
                b.WriteString("` ")
                
                if depth < 3 { // Limit recursion depth
                    traverseCallFlow(call.CalleeName, packages, b, visited, depth+1)
                } else {
                    b.WriteString("(depth limit)\n")
                }
            }
            
            break
        }
    }
    
    if !found {
        b.WriteString(indent)
        b.WriteString("- `")
        b.WriteString(funcName)
        b.WriteString("` (external or not found)\n\n")
    }
}

// GenerateDependencyMap creates a simplified dependency map for the package.
func GenerateDependencyMap(packages map[string]*processor.Result) string {
    var b strings.Builder
    
    b.WriteString("### Package Dependencies\n\n")
    
    // Simple heuristic: check if functions call standard library or external packages
    stdLibPkg := []string{"fmt", "strings", "bytes", "io", "os", "path", "strconv"}
    
    for pkgName, pkg := range packages {
        if pkg == nil {
            continue
        }
        
        externalCalls := make(map[string]bool)
        
        for _, calls := range pkg.CallGraph {
            for _, call := range calls {
                for _, stdPkg := range stdLibPkg {
                    if strings.HasPrefix(call.CalleeName, stdPkg+".") {
                        externalCalls[stdPkg] = true
                        break
                    }
                }
            }
        }
        
        if len(externalCalls) > 0 {
            b.WriteString("**`")
            b.WriteString(pkgName)
            b.WriteString("`** uses:\n\n")
            
            for stdPkg := range externalCalls {
                b.WriteString("- `")
                b.WriteString(stdPkg)
                b.WriteString("`\n")
            }
            
            b.WriteString("\n")
        }
    }
    
    return b.String()
}
```

### markdown.go - Integrate Call Summaries

**Location:** Add after Metrics section in `Markdown()` function

```go
// Add call flow summary
b.WriteString(GenerateCallSummary(sum.Packages))

// Optionally add execution flow for main entry points
for _, pkg := range sum.Packages {
    if pkg != nil {
        // Find main or exported functions as potential entry points
        for _, fn := range pkg.Functions {
            if fn.Name == "main" || isExported(fn.Name) {
                b.WriteString(GenerateCallFlow(fn.Name, sum.Packages))
            }
        }
    }
}

// Add dependency map
b.WriteString(GenerateDependencyMap(sum.Packages))
```

## Testing Requirements

### Unit Tests
**File:** `internal/formatter/callgraph_summary_test.go` (create new file)

```go
func TestGenerateCallSummary_Empty(t *testing.T) {
    summary := GenerateCallSummary(map[string]*processor.Result{})
    
    if !strings.Contains(summary, "no") && !strings.Contains(summary, "0") {
        t.Error("Expected mention of zero functions for empty input")
    }
}

func TestGenerateCallSummary_WithFunctions(t *testing.T) {
    packages := map[string]*processor.Result{
        "pkg": {
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
    }
    
    summary := GenerateCallSummary(packages)
    
    if !strings.Contains(summary, "main") {
        t.Error("Expected 'main' to be mentioned in summary")
    }
    if !strings.Contains(summary, "Process") {
        t.Error("Expected 'Process' to be mentioned in summary")
    }
}

func TestGenerateFunctionSummary(t *testing.T) {
    calls := []processor.CallInfo{
        {CalleeName: "fmt.Println"},
        {CalleeName: "helper"},
    }
    
    summary := generateFunctionSummary("Process", calls)
    
    if !strings.Contains(summary, "Process") {
        t.Error("Expected function name in summary")
    }
    if !strings.Contains(summary, "fmt.Println") {
        t.Error("Expected callee mentioned in summary")
    }
}

func TestIsExported(t *testing.T) {
    tests := []struct {
        name     string
        expected bool
    }{
        {"Main", true},
        {"process", false},
        {"Helper", true},
        {"helper", false},
        {"", false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := isExported(tt.name)
            if result != tt.expected {
                t.Errorf("isExported(%q) = %v, want %v", tt.name, result, tt.expected)
            }
        })
    }
}

func TestGenerateDependencyMap(t *testing.T) {
    packages := map[string]*processor.Result{
        "pkg": {
            CallGraph: map[string][]processor.CallInfo{
                "Process": {{CalleeName: "fmt.Println"}},
            },
        },
    }
    
    summary := GenerateDependencyMap(packages)
    
    if !strings.Contains(summary, "fmt") {
        t.Error("Expected fmt to be mentioned in dependency map")
    }
}
```

## Acceptance Criteria

- [ ] `GenerateCallSummary()` produces coherent natural language descriptions
- [ ] Function summaries mention what they call and where
- [ ] Entry points are identified and highlighted
- [ ] Package dependencies on standard library are detected
- [ ] Call flow visualization works for main functions
- [ ] All unit tests pass
- [ ] Output is readable and helpful for LLM understanding

## Expected Output Examples

### Markdown Summary Section
```markdown
### Call Flow Summary

The codebase has 45 functions with 128 total calls. Entry points include: `main`, `RunServer`.

**Package `cmd/witc`:**

- `main` calls 2 other functions, including: `runSummarize`, `fmt.Println` (called at main.go:10).

- `runSummarize` calls 5 other functions, including: `mergeResults`, `scanner.Scan`, `formatter.Markdown`.

Key entry points in this package: `main`, `RunServer`.

### Package Dependencies

**`cmd/witc`** uses:

- `fmt`
- `github.com/spf13/cobra`
```

## Dependencies
- Uses existing `processor.Result` and `CallInfo` types
- No external dependencies beyond standard library

## Notes for Reviewer
- Number-to-word conversion is simplified; consider using a proper library for production
- Call flow traversal uses DFS with depth limit to prevent infinite loops
- Consider making summary length configurable (short/medium/detailed)
- Natural language generation could be enhanced with more sophisticated templates
