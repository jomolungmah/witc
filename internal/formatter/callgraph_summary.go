package formatter

import (
	"sort"
	"strconv"
	"strings"

	goparser "github.com/ai-suite/witc/internal/processor/go"
)

// GenerateCallSummary creates a natural-language summary of the call graph,
// reading the type-checked graph so names, packages, and counts match the
// "Call Graph" and "Metrics" sections.
func GenerateCallSummary(cg *goparser.CallGraph) string {
	var b strings.Builder
	b.WriteString("### Call Flow Summary\n\n")

	if cg == nil || len(cg.Functions) == 0 {
		b.WriteString("No call graph data available (0 functions).\n\n")
		return b.String()
	}

	names := sortedFuncNames(cg)

	internalCalls := 0
	for _, n := range names {
		internalCalls += len(cg.Functions[n].Callees)
	}

	b.WriteString("The codebase has " + strconv.Itoa(len(cg.Functions)) +
		" functions with " + strconv.Itoa(internalCalls) + " internal calls")
	if cg.ExternalCalls > 0 {
		b.WriteString(" and " + strconv.Itoa(cg.ExternalCalls) + " external calls")
	}
	b.WriteString(".")

	if eps := entryPointNames(cg); len(eps) > 0 {
		b.WriteString(" Entry points include: " + joinCode(capStrings(eps, 12)) + ".")
	}
	b.WriteString("\n\n")

	// Group functions by their package for a per-package walk-through.
	byPkg := make(map[string][]string)
	var pkgs []string
	for _, n := range names {
		p := cg.Functions[n].Package
		if _, ok := byPkg[p]; !ok {
			pkgs = append(pkgs, p)
		}
		byPkg[p] = append(byPkg[p], n)
	}
	sort.Strings(pkgs)

	for _, p := range pkgs {
		var body strings.Builder
		for _, n := range byPkg[p] {
			info := cg.Functions[n]
			if len(info.Callees) == 0 {
				continue
			}
			body.WriteString(describeFunction(n, info))
		}
		if body.Len() > 0 {
			b.WriteString("**Package `" + p + "`:**\n\n")
			b.WriteString(body.String())
			b.WriteString("\n")
		}
	}

	return b.String()
}

// describeFunction renders one sentence about a function's outgoing calls.
func describeFunction(name string, info *goparser.FuncInfo) string {
	callees := uniqueCalleeNames(info)
	var b strings.Builder
	b.WriteString("- `" + name + "` ")

	switch len(callees) {
	case 1:
		b.WriteString("calls `" + callees[0] + "`.\n")
	default:
		shown := capStrings(callees, 5)
		b.WriteString("calls " + strconv.Itoa(len(callees)) + " functions: " + joinCode(shown))
		if len(callees) > len(shown) {
			b.WriteString(", and others")
		}
		b.WriteString(".\n")
	}
	return b.String()
}

// GenerateCallFlow describes the execution flow for a specific entry point by
// walking the call graph depth-first with cycle detection.
func GenerateCallFlow(entryPoint string, cg *goparser.CallGraph) string {
	var b strings.Builder
	b.WriteString("### Execution Flow: `" + entryPoint + "`\n\n")
	if cg == nil {
		return b.String()
	}
	traverseCallFlow(entryPoint, cg, &b, map[string]bool{}, 0)
	b.WriteString("\n")
	return b.String()
}

func traverseCallFlow(name string, cg *goparser.CallGraph, b *strings.Builder, visited map[string]bool, depth int) {
	indent := strings.Repeat("  ", depth)

	if visited[name] {
		b.WriteString(indent + "- `" + name + "` *(cycle)*\n")
		return
	}
	visited[name] = true

	info := cg.Functions[name]
	if info == nil || len(info.Callees) == 0 {
		b.WriteString(indent + "- `" + name + "` (no internal calls)\n")
		return
	}

	b.WriteString(indent + "- `" + name + "` calls:\n")
	for _, callee := range uniqueCalleeNames(info) {
		if depth < 3 {
			traverseCallFlow(callee, cg, b, visited, depth+1)
		} else {
			b.WriteString(indent + "  - `" + callee + "` …\n")
		}
	}
}

// GenerateDependencyMap lists the external packages each analyzed package calls
// into, derived from the type-checked external-call edges.
func GenerateDependencyMap(cg *goparser.CallGraph) string {
	var b strings.Builder
	b.WriteString("### Package Dependencies\n\n")
	if cg == nil || len(cg.ExternalDeps) == 0 {
		b.WriteString("No external package calls detected.\n\n")
		return b.String()
	}

	pkgs := make([]string, 0, len(cg.ExternalDeps))
	for p := range cg.ExternalDeps {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, p := range pkgs {
		deps := cg.ExternalDeps[p]
		if len(deps) == 0 {
			continue
		}
		b.WriteString("**`" + p + "`** uses:\n\n")
		for _, dep := range deps {
			b.WriteString("- `" + dep + "`\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// entryPointNames returns functions nothing in the module calls, plus main.
func entryPointNames(cg *goparser.CallGraph) []string {
	var eps []string
	for name, info := range cg.Functions {
		if info == nil {
			continue
		}
		if len(info.Callers) == 0 || name == "main" || strings.HasSuffix(name, ".main") {
			eps = append(eps, name)
		}
	}
	eps = deduplicateStrings(eps)
	sort.Strings(eps)
	return eps
}

func sortedFuncNames(cg *goparser.CallGraph) []string {
	names := make([]string, 0, len(cg.Functions))
	for n := range cg.Functions {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// uniqueCalleeNames returns a function's distinct callees in sorted order.
func uniqueCalleeNames(info *goparser.FuncInfo) []string {
	seen := make(map[string]bool, len(info.Callees))
	var names []string
	for _, c := range info.Callees {
		if !seen[c.Name] {
			seen[c.Name] = true
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	return names
}

// capStrings returns at most n elements of s.
func capStrings(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// joinCode renders a list as comma-separated inline code spans.
func joinCode(items []string) string {
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = "`" + it + "`"
	}
	return strings.Join(quoted, ", ")
}
