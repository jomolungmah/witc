package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jomolungmah/witc/internal/callgraph"
	"github.com/jomolungmah/witc/internal/formatter"
	"github.com/jomolungmah/witc/internal/index"
	"github.com/jomolungmah/witc/internal/processor"
	"github.com/jomolungmah/witc/internal/progress"
	"github.com/jomolungmah/witc/internal/scanner"
	"github.com/spf13/cobra"
)

var (
	outputFile   string
	format       string
	noStructure  bool
	excludeGen   bool
	includeTests bool
	detail       string
	maxTokens    int
	noProgress   bool
	verbosity    int
	indexForce   bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "witc",
		Short: "Summarize codebases for LLM coding agents",
		// Keep failures to a one-line "Error: ..." rather than dumping usage,
		// which matters for the query commands that exit non-zero on no match.
		SilenceUsage: true,
	}

	summarizeCmd := &cobra.Command{
		Use:   "summarize [path]",
		Short: "Summarize a codebase",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSummarize,
	}

	summarizeCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Write output to file (default: stdout)")
	summarizeCmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown, json")
	summarizeCmd.Flags().BoolVar(&noStructure, "no-structure", false, "Omit file structure, output API surface only")
	summarizeCmd.Flags().BoolVar(&excludeGen, "exclude-generated", false, "Skip generated Go files")
	summarizeCmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include _test.go files in the summary")
	summarizeCmd.Flags().StringVar(&detail, "detail", "high", "Output detail: low (API only), medium (+call graph, metrics), high (everything)")
	summarizeCmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "Cap estimated output size in tokens (0 = unlimited)")
	summarizeCmd.Flags().BoolVar(&noProgress, "no-progress", false, "Disable progress output on stderr")
	summarizeCmd.Flags().CountVarP(&verbosity, "verbose", "v", "Increase verbosity (repeatable): -v logs call-graph build phase timings and per-package counts; -vv also traces go/packages driver (go list) invocations and timing")

	rootCmd.AddCommand(summarizeCmd)

	indexCmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Build and cache a JSON index for fast, low-token queries",
		Long: "Build the summary once and cache it to .witc/index.json so query\n" +
			"commands can answer targeted lookups without recomputing the call graph.\n" +
			"Re-running on unchanged source is a no-op unless --force is given.",
		Args: cobra.MaximumNArgs(1),
		RunE: runIndex,
	}
	indexCmd.Flags().BoolVar(&excludeGen, "exclude-generated", false, "Skip generated Go files")
	indexCmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include _test.go files in the index")
	indexCmd.Flags().BoolVar(&noProgress, "no-progress", false, "Disable progress output on stderr")
	indexCmd.Flags().CountVarP(&verbosity, "verbose", "v", "Increase verbosity (repeatable); see 'summarize --help'")
	indexCmd.Flags().BoolVar(&indexForce, "force", false, "Rebuild even when the cached index is up to date")
	rootCmd.AddCommand(indexCmd)

	for _, c := range queryCommands() {
		rootCmd.AddCommand(c)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// resolveRoot turns an optional path argument into a validated absolute
// directory, defaulting to the current directory.
func resolveRoot(args []string) (string, error) {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", root)
	}
	return root, nil
}

// buildOptions carries the inputs the scan/parse/call-graph pipeline needs,
// decoupled from the command-specific flags so summarize and index can share it.
type buildOptions struct {
	excludeGenerated bool
	includeTests     bool
	noStructure      bool
	detail           string
	maxTokens        int
	progress         bool
	verbose          bool
	veryVerbose      bool
}

// buildSummary runs the full pipeline (parse API surface, then build the
// call graph) over the pre-scanned files and returns the aggregated summary.
func buildSummary(root string, files []scanner.File, o buildOptions) (*formatter.Summary, error) {
	langs := languages(o)
	sum := &formatter.Summary{
		Root:        root,
		Paths:       make([]string, 0, len(files)),
		Packages:    make(map[string]*processor.Result),
		NoStructure: o.noStructure,
		Detail:      o.detail,
		MaxTokens:   o.maxTokens,
	}

	// Progress goes to stderr so it never corrupts the summary on stdout or in
	// the output file; it auto-disables when stderr isn't an interactive terminal.
	// Verbose modes print line-by-line diagnostics instead, so the animated
	// spinner is suppressed to avoid the two clobbering each other.
	rep := progress.New(os.Stderr, o.progress && !o.verbose && progress.IsTerminal(os.Stderr))

	ctx := context.Background()
	for i, f := range files {
		rep.Step("Parsing files", i+1, len(files))
		lang := langFor(langs, f.Ext)
		if lang == nil {
			continue
		}
		sum.Paths = append(sum.Paths, f.Path)
		src, err := os.ReadFile(filepath.Join(root, f.Path))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Path, err)
		}
		result, err := lang.processor.Process(ctx, f.Path, src)
		if err != nil {
			return nil, fmt.Errorf("process %s: %w", f.Path, err)
		}
		pkgPath := filepath.Dir(f.Path)
		result.ImportPath = pkgPath
		if existing, ok := sum.Packages[pkgPath]; ok {
			mergeResults(existing, result)
		} else {
			sum.Packages[pkgPath] = result
		}
	}

	// Per language: prefer its precise call-graph builder (Go's resolves callees
	// via full type information and merges duplicate nodes); fall back to the
	// AST-only aggregate over that language's results if it fails. This phase
	// type-checks the dependency tree and is usually the slowest, so show an
	// indeterminate spinner while it runs.
	stop := rep.Spin("Building call graph (type-checking)")
	var graphs []*callgraph.CallGraph
	for _, lang := range langs {
		results := resultsForLanguage(sum, lang.name)
		if len(results) == 0 {
			continue
		}
		var g *callgraph.CallGraph
		if lang.buildCallGraph != nil {
			var err error
			if g, err = lang.buildCallGraph(root, o); err != nil {
				fmt.Fprintf(os.Stderr, "witc: using AST call graph for %s (%v)\n", lang.name, err)
				g = nil
			}
		}
		if g == nil {
			// Languages without a precise builder use the AST aggregate directly.
			g = callgraph.Aggregate(results)
		}
		graphs = append(graphs, g)
	}
	stop()
	sum.CallGraph = callgraph.Merge(graphs...)
	rep.Done(fmt.Sprintf("Analyzed %d files in %d packages", len(sum.Paths), len(sum.Packages)))
	return sum, nil
}

// resultsForLanguage returns the merged per-package results produced by the
// named language's processor.
func resultsForLanguage(sum *formatter.Summary, lang string) []*processor.Result {
	var out []*processor.Result
	for _, r := range sum.Packages {
		if r != nil && r.Language == lang {
			out = append(out, r)
		}
	}
	return out
}

func runIndex(cmd *cobra.Command, args []string) error {
	root, err := resolveRoot(args)
	if err != nil {
		return err
	}

	// The cache key is cheap to compute (just stats the scanned files), so check
	// freshness before the expensive type-checked build.
	files, err := scanner.Scan(root, scanner.Options{Extensions: scanExtensions(), IncludeTests: includeTests})
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	key, err := index.ComputeKey(root, files, formatter.SchemaVersion)
	if err != nil {
		return fmt.Errorf("compute cache key: %w", err)
	}
	if !indexForce && index.Fresh(root, key) {
		fmt.Fprintf(os.Stderr, "witc: index is up to date (%s)\n", index.Path(root))
		return nil
	}

	// The index always stores the full surface at JSON's schema; detail and token
	// budgeting are presentation concerns applied later by query/summarize.
	sum, err := buildSummary(root, files, buildOptions{
		excludeGenerated: excludeGen,
		includeTests:     includeTests,
		detail:           "high",
		progress:         !noProgress,
		verbose:          verbosity >= 1,
		veryVerbose:      verbosity >= 2,
	})
	if err != nil {
		return err
	}

	data, err := formatter.JSON(sum)
	if err != nil {
		return err
	}
	if err := index.Write(root, data, key); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "witc: wrote %s (%d packages)\n", index.Path(root), len(sum.Packages))
	return nil
}

func runSummarize(cmd *cobra.Command, args []string) error {
	root, err := resolveRoot(args)
	if err != nil {
		return err
	}

	files, err := scanner.Scan(root, scanner.Options{Extensions: scanExtensions(), IncludeTests: includeTests})
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	sum, err := buildSummary(root, files, buildOptions{
		excludeGenerated: excludeGen,
		includeTests:     includeTests,
		noStructure:      noStructure,
		detail:           detail,
		maxTokens:        maxTokens,
		progress:         !noProgress,
		verbose:          verbosity >= 1,
		veryVerbose:      verbosity >= 2,
	})
	if err != nil {
		return err
	}

	var out string
	switch format {
	case "markdown":
		out, err = formatter.Markdown(sum)
	case "json":
		out, err = formatter.JSON(sum)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
	if err != nil {
		return err
	}

	if outputFile != "" {
		return os.WriteFile(outputFile, []byte(out), 0644)
	}
	fmt.Print(out)
	return nil
}

func mergeResults(dst, src *processor.Result) {
	// Preserve the package doc from whichever file carries it.
	if dst.Doc == "" {
		dst.Doc = src.Doc
	}
	if dst.Language == "" {
		dst.Language = src.Language
	}
	// Merge structs by name (methods may be in different files)
	for _, s := range src.Structs {
		found := false
		for i := range dst.Structs {
			if dst.Structs[i].Name == s.Name {
				if dst.Structs[i].Doc == "" {
					dst.Structs[i].Doc = s.Doc
				}
				dst.Structs[i].Fields = append(dst.Structs[i].Fields, s.Fields...)
				dst.Structs[i].Methods = append(dst.Structs[i].Methods, s.Methods...)
				found = true
				break
			}
		}
		if !found {
			dst.Structs = append(dst.Structs, s)
		}
	}
	// Merge interfaces by name
	for _, iface := range src.Interfaces {
		found := false
		for i := range dst.Interfaces {
			if dst.Interfaces[i].Name == iface.Name {
				if dst.Interfaces[i].Doc == "" {
					dst.Interfaces[i].Doc = iface.Doc
				}
				dst.Interfaces[i].Methods = append(dst.Interfaces[i].Methods, iface.Methods...)
				found = true
				break
			}
		}
		if !found {
			dst.Interfaces = append(dst.Interfaces, iface)
		}
	}
	dst.Functions = append(dst.Functions, src.Functions...)
}
