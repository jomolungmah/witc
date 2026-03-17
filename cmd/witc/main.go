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
	outputFile    string
	format        string
	noStructure   bool
	excludeGen    bool
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

	proc := goparser.Processor{ExcludeGenerated: excludeGen}
	sum := &formatter.Summary{
		Root:        root,
		Paths:       make([]string, 0, len(files)),
		Packages:    make(map[string]*processor.Result),
		NoStructure: noStructure,
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
	// Merge structs by name (methods may be in different files)
	for _, s := range src.Structs {
		found := false
		for i := range dst.Structs {
			if dst.Structs[i].Name == s.Name {
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
