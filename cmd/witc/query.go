package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jomolungmah/witc/internal/formatter"
	"github.com/jomolungmah/witc/internal/index"
	"github.com/jomolungmah/witc/internal/scanner"
	"github.com/spf13/cobra"
)

var queryJSON bool

// queryCommands returns the read-only lookup commands that answer targeted
// questions from the cached index without emitting the whole summary.
func queryCommands() []*cobra.Command {
	// Each command takes the query plus an optional project path (default ".").
	find := &cobra.Command{
		Use:   "find <name> [path]",
		Short: "Locate symbol(s) by name (exact, pkg-qualified, or substring)",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runFind,
	}
	where := &cobra.Command{
		Use:   "where <name> [path]",
		Short: "Print just the file:line of matching symbol(s)",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runWhere,
	}
	callers := &cobra.Command{
		Use:   "callers <func> [path]",
		Short: "List in-module functions that call the given function",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  func(c *cobra.Command, a []string) error { return runEdges(a, true) },
	}
	callees := &cobra.Command{
		Use:   "callees <func> [path]",
		Short: "List in-module functions the given function calls",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  func(c *cobra.Command, a []string) error { return runEdges(a, false) },
	}
	pkg := &cobra.Command{
		Use:   "package <path> [root]",
		Short: "Print one package's API surface",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runPackage,
	}

	cmds := []*cobra.Command{find, where, callers, callees, pkg}
	for _, c := range cmds {
		c.Flags().BoolVar(&queryJSON, "json", false, "Emit the result as JSON instead of text")
		c.Flags().BoolVar(&includeTests, "include-tests", false, "Include _test.go files when (re)building the index")
		c.Flags().BoolVar(&noProgress, "no-progress", false, "Disable progress output on stderr")
	}
	return cmds
}

// ensureIndex loads the cached index for the resolved root, (re)building it
// first when missing or stale so queries always reflect the current source.
func ensureIndex(args []string) (*index.Index, error) {
	root, err := resolveRoot(args)
	if err != nil {
		return nil, err
	}

	files, err := scanner.Scan(root, scanner.Options{Extensions: scanExtensions(), IncludeTests: includeTests})
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	key, err := index.ComputeKey(root, files, cacheSalt())
	if err != nil {
		return nil, fmt.Errorf("compute cache key: %w", err)
	}
	if !index.Fresh(root, key) {
		sum, err := buildSummary(root, files, buildOptions{
			excludeGenerated: excludeGen,
			includeTests:     includeTests,
			detail:           "high",
			progress:         !noProgress,
			verbose:          verbosity >= 1,
			veryVerbose:      verbosity >= 2,
		})
		if err != nil {
			return nil, err
		}
		data, err := formatter.JSON(sum)
		if err != nil {
			return nil, err
		}
		if err := index.Write(root, data, key); err != nil {
			return nil, err
		}
	}

	data, err := index.Load(root)
	if err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}
	return index.Parse(data)
}

func runFind(cmd *cobra.Command, args []string) error {
	ix, err := ensureIndex(args[1:]) // args[0] is the query; args[1:] is the optional path
	if err != nil {
		return err
	}
	hits := ix.Find(args[0])
	if len(hits) == 0 {
		return fmt.Errorf("no symbol matching %q", args[0])
	}
	if queryJSON {
		return printJSON(hits)
	}
	for _, s := range hits {
		fmt.Printf("%s\t%s\n", s.Location, declString(s))
		if s.Doc != "" {
			fmt.Printf("\t%s\n", s.Doc)
		}
	}
	return nil
}

func runWhere(cmd *cobra.Command, args []string) error {
	ix, err := ensureIndex(args[1:])
	if err != nil {
		return err
	}
	hits := ix.Find(args[0])
	if len(hits) == 0 {
		return fmt.Errorf("no symbol matching %q", args[0])
	}
	if queryJSON {
		locs := make([]string, len(hits))
		for i, s := range hits {
			locs[i] = s.Location.String()
		}
		return printJSON(locs)
	}
	// One location per line; bare when unambiguous, name-qualified when not.
	for _, s := range hits {
		if len(hits) == 1 {
			fmt.Println(s.Location)
		} else {
			fmt.Printf("%s\t%s\n", s.Location, s.Name)
		}
	}
	return nil
}

func runEdges(args []string, callers bool) error {
	name := args[0]
	ix, err := ensureIndex(args[1:])
	if err != nil {
		return err
	}
	funcs := ix.GraphFuncs(name)
	if len(funcs) == 0 {
		return fmt.Errorf("no function matching %q in the call graph", name)
	}
	if queryJSON {
		return printJSON(funcs)
	}
	for _, gf := range funcs {
		edges, verb := gf.Callees, "calls"
		if callers {
			edges, verb = gf.Callers, "called by"
		}
		if len(edges) == 0 {
			fmt.Printf("%s: (no in-module %s)\n", gf.Name, map[bool]string{true: "callers", false: "callees"}[callers])
			continue
		}
		fmt.Printf("%s %s:\n", gf.Name, verb)
		for _, e := range edges {
			fmt.Printf("  %s\n", e)
		}
	}
	return nil
}

func runPackage(cmd *cobra.Command, args []string) error {
	ix, err := ensureIndex(args[1:])
	if err != nil {
		return err
	}
	p := ix.Package(args[0])
	if p == nil {
		return fmt.Errorf("no package matching %q", args[0])
	}
	if queryJSON {
		return printJSON(p)
	}
	fmt.Printf("# %s\n", p.ImportPath)
	if p.Doc != "" {
		fmt.Printf("%s\n", p.Doc)
	}
	for _, s := range p.Structs {
		printSym(index.Symbol{Kind: "struct", Language: p.Language, Name: s.Name, Doc: s.Doc, Location: s.Location})
		for _, m := range s.Methods {
			printSym(index.Symbol{Kind: "method", Language: p.Language, Receiver: m.Receiver, Name: m.Name, Signature: m.Signature, Doc: m.Doc, Location: m.Location})
		}
	}
	for _, in := range p.Interfaces {
		sig := ""
		if in.Alias != "" {
			sig = "= " + in.Alias
		}
		printSym(index.Symbol{Kind: "interface", Language: p.Language, Name: in.Name, Signature: sig, Doc: in.Doc, Location: in.Location})
	}
	for _, f := range p.Functions {
		printSym(index.Symbol{Kind: "func", Language: p.Language, Name: f.Name, Signature: f.Signature, Doc: f.Doc, Location: f.Location})
	}
	return nil
}

func printSym(s index.Symbol) {
	fmt.Printf("%s\t%s\n", s.Location, declString(s))
}

// declString renders a one-line declaration for a symbol in its language's
// syntax: for Go, reconstructing the "func Name(...)" form from the stored
// "func(...)" signature; for TypeScript/JavaScript, class/interface/function
// forms (with type aliases shown as "type Name = ...").
func declString(s index.Symbol) string {
	if s.Language != "" && s.Language != "go" {
		switch s.Kind {
		case "struct":
			return "class " + s.Name
		case "interface":
			if strings.HasPrefix(s.Signature, "= ") {
				return "type " + s.Name + " " + s.Signature
			}
			return "interface " + s.Name
		case "method":
			return s.Receiver + "." + s.Name + s.Signature
		default:
			if s.Signature == "" { // factory-result const (store, wrapped component)
				return "const " + s.Name
			}
			return "function " + s.Name + s.Signature
		}
	}
	switch s.Kind {
	case "struct":
		return "type " + s.Name + " struct"
	case "interface":
		return "type " + s.Name + " interface"
	case "method":
		return "func (" + s.Receiver + ") " + s.Name + strings.TrimPrefix(s.Signature, "func")
	default:
		return "func " + s.Name + strings.TrimPrefix(s.Signature, "func")
	}
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
