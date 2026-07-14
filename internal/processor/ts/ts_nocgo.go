//go:build !cgo

package tsparser

import (
	"context"
	"errors"

	"github.com/jomolungmah/witc/internal/callgraph"
	"github.com/jomolungmah/witc/internal/processor"
)

// errNoCGO is returned when CGO is disabled and tree-sitter is unavailable.
var errNoCGO = errors.New("tree-sitter support requires CGO; rebuild with CGO_ENABLED=1 and a C compiler for TypeScript/JavaScript analysis")

// Processor implements processor.Processor for TypeScript/JavaScript files.
// When CGO is disabled, Process returns an error.
type Processor struct{}

var supportedExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}

// Supports reports whether the file extension is handled by this processor.
func (p *Processor) Supports(ext string) bool { return supportedExts[ext] }

// Process always returns an error when CGO is disabled.
func (p *Processor) Process(ctx context.Context, path string, src []byte) (*processor.Result, error) {
	return nil, errNoCGO
}

// BuildCallGraph always returns an error when CGO is disabled.
func BuildCallGraph(root string, relPaths []string) (*callgraph.CallGraph, error) {
	return nil, errNoCGO
}
