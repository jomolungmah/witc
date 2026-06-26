package goparser

import (
	"github.com/ai-suite/witc/internal/processor"
)

// Metrics contains various measurements about the codebase's call graph.
type Metrics struct {
	// Overall statistics
	TotalFunctions    int     `json:"totalFunctions"`
	TotalCalls        int     `json:"totalCalls"`
	MaxCallDepth      int     `json:"maxCallDepth"`
	AvgCallersPerFunc float64 `json:"avgCallersPerFunction"`
	AvgCalleesPerFunc float64 `json:"avgCalleesPerFunction"`

	// Extremes (function names with highest values)
	MaxFanIn          string   `json:"maxFanInFunction"`
	MaxFanOut         string   `json:"maxFanOutFunction"`
	DeepestCallChain  string   `json:"deepestCallChainFunction"`
	ExternalCalls     int      `json:"externalCalls"`
	InternalCalls     int      `json:"internalCalls"`
	HighCouplingFuncs []string `json:"highCouplingFunctions"`
}

// CalculateMetrics computes various metrics from an aggregated call graph.
func CalculateMetrics(cg *CallGraph) *Metrics {
	m := &Metrics{
		TotalFunctions:    0,
		TotalCalls:        0,
		MaxCallDepth:      0,
		AvgCallersPerFunc: 0,
		AvgCalleesPerFunc: 0,
		ExternalCalls:     0,
		InternalCalls:     0,
	}

	if cg == nil || cg.Functions == nil {
		return m
	}

	m.TotalFunctions = len(cg.Functions)

	funcCallers := make(map[string]int) // function -> number of callers
	funcCallees := make(map[string]int) // function -> number of unique callees

	for funcName, info := range cg.Functions {
		if info == nil {
			continue
		}

		funcCallees[funcName] = len(info.Callees)

		for _, callee := range info.Callees {
			funcCallers[callee.Name]++
			m.TotalCalls++

			if isExternalCall(callee.Name, info.Package) {
				m.ExternalCalls++
			} else {
				m.InternalCalls++
			}
		}

		for _, caller := range info.Callers {
			funcCallers[caller.Name]++
		}
	}

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

	m.MaxCallDepth = estimateMaxCallDepth(funcCallees)

	threshold := int(m.AvgCalleesPerFunc * 2)
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
