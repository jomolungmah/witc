package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ai-suite/witc/internal/formatter"
	"github.com/ai-suite/witc/internal/processor"
	goparser "github.com/ai-suite/witc/internal/processor/go"
	"github.com/ai-suite/witc/internal/scanner"
	"github.com/spf13/cobra"
)

var (
	outputFile   string
	format       string
	noStructure  bool
	excludeGen   bool
	excludeTests bool
	detail       string
	maxTokens    int
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "witc",
		Short: "Summarize codebases for LLM coding agents",
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
	summarizeCmd.Flags().BoolVar(&excludeTests, "exclude-tests", false, "Exclude test functions from output")
	summarizeCmd.Flags().StringVar(&detail, "detail", "high", "Output detail: low (API only), medium (+call graph, metrics), high (everything)")
	summarizeCmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "Cap estimated output size in tokens (0 = unlimited)")

	rootCmd.AddCommand(summarizeCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runSummarize(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}

	files, err := scanner.Scan(root)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	proc := goparser.Processor{ExcludeGenerated: excludeGen, ExcludeTests: excludeTests}
	sum := &formatter.Summary{
		Root:        root,
		Paths:       make([]string, 0, len(files)),
		Packages:    make(map[string]*processor.Result),
		NoStructure: noStructure,
		Detail:      detail,
		MaxTokens:   maxTokens,
	}

	ctx := context.Background()
	for _, f := range files {
		if !proc.Supports(f.Ext) {
			continue
		}
		sum.Paths = append(sum.Paths, f.Path)
		src, err := os.ReadFile(filepath.Join(root, f.Path))
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Path, err)
		}
		result, err := proc.Process(ctx, f.Path, src)
		if err != nil {
			return fmt.Errorf("process %s: %w", f.Path, err)
		}
		pkgPath := filepath.Dir(f.Path)
		result.ImportPath = pkgPath
		if existing, ok := sum.Packages[pkgPath]; ok {
			mergeResults(existing, result)
		} else {
			sum.Packages[pkgPath] = result
		}
	}

	var results []*processor.Result
	for _, r := range sum.Packages {
		results = append(results, r)
	}

	// Prefer a type-checked call graph (resolves callees precisely and merges
	// duplicate nodes); fall back to the AST-only aggregate if the module can't
	// be loaded or type-checked.
	if typed, err := goparser.BuildTypedCallGraph(root); err == nil {
		sum.CallGraph = typed
	} else {
		fmt.Fprintf(os.Stderr, "witc: using AST call graph (%v)\n", err)
		sum.CallGraph = goparser.Aggregate(results)
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
