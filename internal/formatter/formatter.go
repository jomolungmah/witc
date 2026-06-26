package formatter

import (
	"github.com/jomolungmah/witc/internal/processor"
	goparser "github.com/jomolungmah/witc/internal/processor/go"
)

// Summary is the aggregated output to format.
type Summary struct {
	Root        string
	Paths       []string
	Packages    map[string]*processor.Result
	CallGraph   *goparser.CallGraph
	NoStructure bool
	// Detail selects how many output tiers to emit: "low" (package list +
	// exported API), "medium" (+ call graph + metrics), or "high" (everything,
	// including inline calls and prose). Empty defaults to "high".
	Detail string
	// MaxTokens caps the estimated output size; 0 means unlimited. Sections are
	// dropped from least to most important to fit, and the API surface is
	// truncated by call-graph centrality when it alone exceeds the budget.
	MaxTokens int
}
