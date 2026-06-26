package goparser

import (
	"sort"

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
	MaxFanInCount     int      `json:"maxFanInCount"`
	MaxFanOut         string   `json:"maxFanOutFunction"`
	MaxFanOutCount    int      `json:"maxFanOutCount"`
	ExternalCalls     int      `json:"externalCalls"`
	InternalCalls     int      `json:"internalCalls"`
	HighCouplingFuncs []string `json:"highCouplingFunctions"`
}

// CalculateMetrics computes metrics directly from the call graph's structure.
// Fan-in is a function's number of incoming edges (Callers) and fan-out its
// outgoing edges (Callees); internal calls are edges within the analyzed
// module, while external calls are recorded on the graph by the typed builder.
func CalculateMetrics(cg *CallGraph) *Metrics {
	m := &Metrics{}
	if cg == nil || cg.Functions == nil {
		return m
	}

	m.TotalFunctions = len(cg.Functions)
	m.ExternalCalls = cg.ExternalCalls

	// Deterministic iteration so ties resolve to a stable function name.
	names := make([]string, 0, len(cg.Functions))
	for name := range cg.Functions {
		names = append(names, name)
	}
	sort.Strings(names)

	var totalFanIn, totalFanOut int
	for _, name := range names {
		info := cg.Functions[name]
		if info == nil {
			continue
		}
		fanIn := len(info.Callers)
		fanOut := len(info.Callees)
		totalFanIn += fanIn
		totalFanOut += fanOut

		if fanIn > m.MaxFanInCount {
			m.MaxFanInCount = fanIn
			m.MaxFanIn = name
		}
		if fanOut > m.MaxFanOutCount {
			m.MaxFanOutCount = fanOut
			m.MaxFanOut = name
		}
	}

	// Each outgoing edge is one internal call.
	m.InternalCalls = totalFanOut
	m.TotalCalls = m.InternalCalls + m.ExternalCalls

	if m.TotalFunctions > 0 {
		m.AvgCallersPerFunc = float64(totalFanIn) / float64(m.TotalFunctions)
		m.AvgCalleesPerFunc = float64(totalFanOut) / float64(m.TotalFunctions)
	}

	m.MaxCallDepth = maxCallDepth(cg)

	threshold := int(m.AvgCalleesPerFunc * 2)
	if threshold < 5 {
		threshold = 5
	}
	for _, name := range names {
		if len(cg.Functions[name].Callees) >= threshold {
			m.HighCouplingFuncs = append(m.HighCouplingFuncs, name)
		}
	}

	return m
}

// maxCallDepth returns the length of the longest call chain in the graph,
// guarding against cycles so recursive code doesn't loop forever.
func maxCallDepth(cg *CallGraph) int {
	memo := make(map[string]int)
	onStack := make(map[string]bool)

	var depth func(name string) int
	depth = func(name string) int {
		if d, ok := memo[name]; ok {
			return d
		}
		if onStack[name] {
			return 0 // cycle: stop counting along this path
		}
		info := cg.Functions[name]
		if info == nil || len(info.Callees) == 0 {
			memo[name] = 1
			return 1
		}
		onStack[name] = true
		best := 0
		for _, callee := range info.Callees {
			if d := depth(callee.Name); d > best {
				best = d
			}
		}
		onStack[name] = false
		memo[name] = best + 1
		return best + 1
	}

	max := 0
	for name := range cg.Functions {
		if d := depth(name); d > max {
			max = d
		}
	}
	return max
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
