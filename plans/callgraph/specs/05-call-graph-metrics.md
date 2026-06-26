# Spec: Call Graph Metrics Calculation

## Overview
Implement comprehensive metrics analysis on the call graph to provide insights about codebase structure, complexity, and coupling. These metrics will help LLM agents understand architectural patterns, identify hotspots, and assess maintainability.

## Files to Create/Modify

### 1. `/home/magnus/work/jomolungmah/witc/internal/processor/go/callgraph_metrics.go` (NEW)
- Implement metric calculation functions
- Define metrics data structures
- Calculate fan-in/fan-out, coupling, complexity estimates

### 2. `/home/magnus/work/jomolungmah/witc/internal/formatter/markdown.go`
- Add "Metrics" section to display calculated statistics
- Format metrics in readable tables/lists

### 3. `/home/magnus/work/jomolungmah/witc/internal/formatter/json.go` (optional)
- Include metrics in JSON output if needed

## Detailed Implementation

### callgraph_metrics.go - New File

```go
package goparser

import (
    "github.com/jomolungmah/witc/internal/processor"
)

// Metrics contains various measurements about the codebase's call graph.
type Metrics struct {
    // Overall statistics
    TotalFunctions   int     `json:"totalFunctions"`
    TotalCalls       int     `json:"totalCalls"`
    MaxCallDepth     int     `json:"maxCallDepth"`
    
    // Average metrics
    AvgCallersPerFunc float64 `json:"avgCallersPerFunction"`
    AvgCalleesPerFunc float64 `json:"avgCalleesPerFunction"`
    
    // Extremes (function names with highest values)
    MaxFanIn         string  `json:"maxFanInFunction"`        // Function called by most others
    MaxFanOut        string  `json:"maxFanOutFunction"`       // Function that calls the most
    DeepestCallChain string  `json:"deepestCallChainFunction"`// Part of deepest call chain
    
    // Coupling metrics
    ExternalCalls    int     `json:"externalCalls"`           // Calls to external packages
    InternalCalls    int     `json:"internalCalls"`           // Calls within same package
    
    // Complexity indicators
    HighCouplingFuncs []string `json:"highCouplingFunctions"`  // Functions with fan-out > threshold
}

// CalculateMetrics computes various metrics from processor results.
func CalculateMetrics(results []*processor.Result) *Metrics {
    m := &Metrics{
        TotalFunctions:      0,
        TotalCalls:          0,
        MaxCallDepth:        0,
        AvgCallersPerFunc:   0,
        AvgCalleesPerFunc:   0,
        ExternalCalls:       0,
        InternalCalls:       0,
    }
    
    // Aggregate data across all results
    funcCallers := make(map[string]int)   // function -> number of callers
    funcCallees := make(map[string]int)   // function -> number of callees
    funcFiles := make(map[string][]string) // function -> files it appears in
    
    for _, r := range results {
        if r == nil || r.CallGraph == nil {
            continue
        }
        
        m.TotalFunctions += len(r.Functions)
        
        // Track functions defined in this result
        for _, fn := range r.Functions {
            funcFiles[fn.Name] = append(funcFiles[fn.Name], r.ImportPath)
        }
        
        // Analyze call graph
        for funcName, calls := range r.CallGraph {
            m.TotalCalls += len(calls)
            
            // Count unique callees per function
            calleeSet := make(map[string]bool)
            for _, call := range calls {
                calleeSet[call.CalleeName] = true
                
                // Track who calls this function (for fan-in)
                funcCallers[call.CalleeName]++
                
                // Check if external or internal call
                if isExternalCall(call.CalleeName, r.ImportPath) {
                    m.ExternalCalls++
                } else {
                    m.InternalCalls++
                }
            }
            
            // Count unique callers per function (for fan-out)
            funcCallees[funcName] = len(calleeSet)
        }
    }
    
    // Calculate averages
    totalFuncs := len(funcCallers) + len(funcCallees)
    if totalFuncs > 0 {
        var totalFanIn, totalFanOut int
        for _, fans := range funcCallers {
            totalFanIn += fans
        }
        for _, fans := range funcCallees {
            totalFanOut += fans
        }
        
        m.AvgCallersPerFunc = float64(totalFanIn) / float64(totalFuncs)
        m.AvgCalleesPerFunc = float64(totalFanOut) / float64(totalFuncs)
    }
    
    // Find extremes
    maxFanInCount := 0
    for funcName, count := range funcCallers {
        if count > maxFanInCount {
            maxFanInCount = count
            m.MaxFanIn = funcName
        }
    }
    
    maxFanOutCount := 0
    for funcName, count := range funcCallees {
        if count > maxFanOutCount {
            maxFanOutCount = count
            m.MaxFanOut = funcName
        }
    }
    
    // Calculate call depth (simplified - actual implementation would use DFS)
    m.MaxCallDepth = estimateMaxCallDepth(funcCallees)
    
    // Identify high coupling functions (fan-out > average * 2 or > 5)
    threshold := int(m.AvgCalleesPerFunc*2)
    if threshold < 5 {
        threshold = 5
    }
    for funcName, count := range funcCallees {
        if count >= threshold {
            m.HighCouplingFuncs = append(m.HighCouplingFuncs, funcName)
        }
    }
    
    return m
}

// isExternalCall checks if a call is to an external package.
func isExternalCall(calleeName, currentPackage string) bool {
    // Simple heuristic: if callee contains a dot, it's likely external
    for i, c := range calleeName {
        if c == '.' {
            pkg := calleeName[:i]
            // Check if this looks like a standard library or external package
            return !looksLikeLocalPackage(pkg, currentPackage)
        }
    }
    return false
}

// looksLikeLocalPackage checks if a package name is likely local.
func looksLikeLocalPackage(pkg, currentPkg string) bool {
    // If current package contains this as prefix, it's local
    if len(currentPkg) >= len(pkg) && currentPkg[:len(pkg)] == pkg {
        return true
    }
    
    // Common local patterns (simplified - in practice would check go.mod)
    commonPrefixes := []string{"internal", "pkg", "app"}
    for _, prefix := range commonPrefixes {
        if len(pkg) >= len(prefix) && pkg[:len(prefix)] == prefix {
            return true
        }
    }
    
    return false
}

// estimateMaxCallDepth performs a simplified depth calculation.
func estimateMaxCallDepth(funcCallees map[string]int) int {
    // Very simplified: max fan-out as proxy for depth
    // Proper implementation would use DFS to find longest call chain
    maxDepth := 0
    
    for _, fans := range funcCallees {
        if fans > maxDepth {
            maxDepth = fans
        }
    }
    
    return maxDepth
}

// CalculateCouplingScore computes a coupling score for a specific function.
func CalculateCouplingScore(calls []processor.CallInfo, allResults []*processor.Result) float64 {
    if len(calls) == 0 {
        return 0
    }
    
    external := 0
    internal := 0
    
    currentPkg := getPackageFromCalls(calls, allResults)
    
    for _, call := range calls {
        if isExternalCall(call.CalleeName, currentPkg) {
            external++
        } else {
            internal++
        }
    }
    
    total := external + internal
    if total == 0 {
        return 0
    }
    
    // Higher score = more external dependencies (higher coupling)
    return float64(external) / float64(total)
}

func getPackageFromCalls(calls []processor.CallInfo, allResults []*processor.Result) string {
    if len(calls) == 0 || len(allResults) == 0 {
        return ""
    }
    
    // Use the package from the first non-empty result
    for _, r := range allResults {
        if r != nil && r.ImportPath != "" {
            return r.ImportPath
        }
    }
    
    return ""
}
```

### markdown.go - Add Metrics Section

**Location:** After Call Graph section in `Markdown()` function

```go
// Calculate and display metrics
b.WriteString("\n## Metrics\n\n")

metrics := goparser.CalculateMetrics(packagesToResults(sum.Packages))

if metrics.TotalFunctions > 0 {
    b.WriteString("### Overview\n\n")
    b.WriteString(fmt.Sprintf("- **Total Functions:** %d\n", metrics.TotalFunctions))
    b.WriteString(fmt.Sprintf("- **Total Calls:** %d\n", metrics.TotalCalls))
    b.WriteString(fmt.Sprintf("- **Average Callees per Function:** %.2f\n", metrics.AvgCalleesPerFunc))
    b.WriteString(fmt.Sprintf("- **External Calls:** %d (%.1f%%)\n", 
        metrics.ExternalCalls, float64(metrics.ExternalCalls)/float64(metrics.TotalCalls)*100))
    
    if metrics.MaxFanIn != "" {
        b.WriteString(fmt.Sprintf("- **Most Called Function:** `%s` (called by %d functions)\n",
            metrics.MaxFanIn, funcCallersCount(sum.Packages, metrics.MaxFanIn)))
    }
    
    if metrics.MaxFanOut != "" {
        b.WriteString(fmt.Sprintf("- **Highest Fan-out:** `%s` (calls %d other functions)\n",
            metrics.MaxFanOut, metrics.AvgCalleesPerFunc)) // Would need actual value
    }
    
    if len(metrics.HighCouplingFuncs) > 0 {
        b.WriteString("\n### High Coupling Functions\n\n")
        b.WriteString("Functions with many dependencies (may indicate refactoring opportunities):\n\n")
        for _, fn := range metrics.HighCouplingFuncs[:min(len(metrics.HighCouplingFuncs), 10)] {
            b.WriteString(fmt.Sprintf("- `%s`\n", fn))
        }
    }
} else {
    b.WriteString("*No metrics available*\n")
}
```

**Helper function:**
```go
func funcCallersCount(packages map[string]*processor.Result, funcName string) int {
    count := 0
    for _, pkg := range packages {
        if calls, ok := pkg.CallGraph[funcName]; ok {
            count += len(calls)
        }
    }
    return count
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

## Testing Requirements

### Unit Tests
**File:** `internal/processor/go/callgraph_metrics_test.go` (create new file)

```go
func TestCalculateMetrics_Basic(t *testing.T) {
    results := []*processor.Result{
        {
            ImportPath: "pkg",
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
    
    metrics := CalculateMetrics(results)
    
    if metrics.TotalFunctions != 3 {
        t.Errorf("TotalFunctions = %d, want 3", metrics.TotalFunctions)
    }
    if metrics.TotalCalls != 2 {
        t.Errorf("TotalCalls = %d, want 2", metrics.TotalCalls)
    }
}

func TestCalculateMetrics_EmptyResults(t *testing.T) {
    metrics := CalculateMetrics([]*processor.Result{})
    
    if metrics.TotalFunctions != 0 {
        t.Errorf("TotalFunctions = %d, want 0", metrics.TotalFunctions)
    }
}

func TestCalculateCouplingScore(t *testing.T) {
    calls := []processor.CallInfo{
        {CalleeName: "fmt.Println"},  // External
        {CalleeName: "helper"},       // Internal (assumed)
        {CalleeName: "strings.Trim"}, // External
    }
    
    allResults := []*processor.Result{
        {ImportPath: "mypackage"},
    }
    
    score := CalculateCouplingScore(calls, allResults)
    
    // Expected: 2/3 external = 0.667
    if score < 0.6 || score > 0.7 {
        t.Errorf("Coupling score = %f, want ~0.67", score)
    }
}

func TestIsExternalCall_StandardLibrary(t *testing.T) {
    if !isExternalCall("fmt.Println", "mypackage") {
        t.Error("Expected fmt.Println to be external")
    }
}

func TestIsExternalCall_LocalPackage(t *testing.T) {
    if isExternalCall("mypackage.helper", "mypackage") {
        t.Error("Expected mypackage.helper to be internal")
    }
}
```

## Acceptance Criteria

- [ ] `CalculateMetrics()` returns accurate function and call counts
- [ ] Fan-in/fan-out calculations are correct
- [ ] External vs internal call distinction works properly
- [ ] High coupling functions are identified (threshold > avg * 2 or > 5)
- [ ] Metrics section appears in Markdown output
- [ ] All unit tests pass
- [ ] Integration test on real codebase produces reasonable metrics

## Expected Output Example

### Markdown Metrics Section
```markdown
## Metrics

### Overview

- **Total Functions:** 45
- **Total Calls:** 128
- **Average Callees per Function:** 2.84
- **External Calls:** 35 (27.3%)
- **Most Called Function:** `fmt.Println` (called by 8 functions)
- **Highest Fan-out:** `Process` (calls 12 other functions)

### High Coupling Functions

Functions with many dependencies (may indicate refactoring opportunities):

- `RunServer`
- `HandleRequest`
- `Initialize`
```

## Dependencies
- Uses existing `processor.Result` and `CallInfo` types
- No external dependencies beyond standard library

## Notes for Reviewer
- The call depth estimation is simplified; consider implementing proper DFS for accuracy
- External/internal detection heuristic may need refinement based on actual project structure
- Threshold for high coupling could be made configurable
- Consider adding more metrics: cyclomatic complexity, cohesion scores, etc.
