package main

import (
	"fmt"
	"os"

	"github.com/jomolungmah/witc/internal/callgraph"
	"github.com/jomolungmah/witc/internal/processor"
	goparser "github.com/jomolungmah/witc/internal/processor/go"
	tsparser "github.com/jomolungmah/witc/internal/processor/ts"
	"github.com/jomolungmah/witc/internal/scanner"
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
			return goparser.BuildTypedCallGraphForModules(root, buildOpts)
		},
	}
}

// tsLanguage covers TypeScript and JavaScript, React dialects included. Its
// call-graph builder has two precise tiers: the type-checking node sidecar
// (resolves member calls through inferred types) when node and a project
// typescript install are available, else the import-resolving tree-sitter
// builder; on error the AST aggregate over per-file records takes over.
func tsLanguage() language {
	exts := []string{".ts", ".tsx", ".js", ".jsx"}
	return language{
		name:      "typescript",
		exts:      exts,
		processor: &tsparser.Processor{},
		buildCallGraph: func(root string, o buildOptions) (*callgraph.CallGraph, error) {
			files, err := scanner.Scan(root, scanner.Options{Extensions: exts, IncludeTests: o.includeTests})
			if err != nil {
				return nil, err
			}
			paths := make([]string, len(files))
			for i, f := range files {
				paths[i] = f.Path
			}
			var logf func(format string, args ...any)
			if o.verbose {
				logf = func(format string, args ...any) {
					fmt.Fprintf(os.Stderr, "witc: "+format+"\n", args...)
				}
			}
			if g, err := tsparser.BuildTypedCallGraph(root, paths, logf); err == nil {
				return g, nil
			} else if logf != nil {
				logf("typescript sidecar unavailable (%v); using import-resolved builder", err)
			}
			return tsparser.BuildCallGraph(root, paths)
		},
	}
}
