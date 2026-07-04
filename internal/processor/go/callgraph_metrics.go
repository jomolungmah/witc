package goparser

import (
	"github.com/jomolungmah/witc/internal/processor"
)

// The graph-wide Metrics/CalculateMetrics moved to internal/callgraph; the
// helpers below are Go-specific name heuristics used for coupling scoring.

func isExternalCall(calleeName, currentPackage string) bool {
	for i, c := range calleeName {
		if c == '.' {
			pkg := calleeName[:i]
			return !looksLikeLocalPackage(pkg, currentPackage)
		}
	}
	return false
}

func looksLikeLocalPackage(pkg, currentPkg string) bool {
	if len(currentPkg) >= len(pkg) && currentPkg[:len(pkg)] == pkg {
		return true
	}

	commonPrefixes := []string{"internal", "pkg", "app"}
	for _, prefix := range commonPrefixes {
		if len(pkg) >= len(prefix) && pkg[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

func estimateMaxCallDepth(funcCallees map[string]int) int {
	maxDepth := 0
	for _, fans := range funcCallees {
		if fans > maxDepth {
			maxDepth = fans
		}
	}
	return maxDepth
}

// CalculateCouplingScore computes a coupling score for a specific function.
func CalculateCouplingScore(calls []CallInfo, allResults []*processor.Result) float64 {
	if len(calls) == 0 {
		return 0
	}

	external := 0
	internal := 0

	currentPkg := getPackageFromCalls(allResults)

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

	return float64(external) / float64(total)
}

func getPackageFromCalls(allResults []*processor.Result) string {
	for _, r := range allResults {
		if r != nil && r.ImportPath != "" {
			return r.ImportPath
		}
	}
	return ""
}
