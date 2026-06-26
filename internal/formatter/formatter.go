package formatter

import (
	"github.com/ai-suite/witc/internal/processor"
	goparser "github.com/ai-suite/witc/internal/processor/go"
)

// Summary is the aggregated output to format.
type Summary struct {
	Root        string
	Paths       []string
	Packages    map[string]*processor.Result
	CallGraph   *goparser.CallGraph
	NoStructure bool
}
