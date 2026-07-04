package main

import (
	"fmt"
	"os"

	"github.com/jomolungmah/witc/internal/callgraph"
	"github.com/jomolungmah/witc/internal/processor"
	goparser "github.com/jomolungmah/witc/internal/processor/go"
	tsparser "github.com/jomolungmah/witc/internal/processor/ts"
)

// language bundles what witc needs to support one source language: the
// per-file API-surface processor and, optionally, a precise whole-module
// call-graph builder (Go's is type-checked via go/packages). When
// buildCallGraph is nil or returns an error, the language falls back to
// callgraph.Aggregate over its per-file results.
type language struct {
	name           string
	exts           []string
	processor      processor.Processor
	buildCallGraph func(root string, o buildOptions) (*callgraph.CallGraph, error)
}

// languages returns the registered language integrations.
func languages(o buildOptions) []language {
	return []language{goLanguage(o), tsLanguage()}
}

// scanExtensions collects the file extensions of every registered language,
// for the scanner. Extensions are static per language, so the build options
// they are registered with are irrelevant here.
func scanExtensions() []string {
	var exts []string
	for _, l := range languages(buildOptions{}) {
		exts = append(exts, l.exts...)
	}
	return exts
}

// langFor returns the registered language that handles the extension, or nil.
func langFor(langs []language, ext string) *language {
	for i := range langs {
		if langs[i].processor.Supports(ext) {
			return &langs[i]
		}
	}
	return nil
}

func goLanguage(o buildOptions) language {
	return language{
		name:      "go",
		exts:      []string{".go"},
		processor: &goparser.Processor{ExcludeGenerated: o.excludeGenerated},
		buildCallGraph: func(root string, o buildOptions) (*callgraph.CallGraph, error) {
			buildOpts := goparser.BuildOptions{PerPackage: o.verbose, TracePackages: o.veryVerbose}
			if o.verbose {
				buildOpts.Logf = func(format string, args ...any) {
					fmt.Fprintf(os.Stderr, "witc: "+format+"\n", args...)
				}
			}
			return goparser.BuildTypedCallGraphWithOptions(root, buildOpts)
		},
	}
}

// tsLanguage covers TypeScript and JavaScript, React dialects included. It has
// no precise call-graph builder yet, so the AST aggregate (identifier calls,
// new expressions, and JSX render edges) is its call-graph tier.
func tsLanguage() language {
	return language{
		name:      "typescript",
		exts:      []string{".ts", ".tsx", ".js", ".jsx"},
		processor: &tsparser.Processor{},
	}
}
