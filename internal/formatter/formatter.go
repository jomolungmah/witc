package formatter

import "github.com/ai-suite/witc/internal/processor"

// Summary is the aggregated output to format.
type Summary struct {
	Root      string
	Paths     []string
	Packages  map[string]*processor.Result
	NoStructure bool
}
