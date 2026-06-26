package formatter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	goparser "github.com/ai-suite/witc/internal/processor/go"
)

// stripFuncKeyword removes a leading "func" keyword from a stored signature so
// method and interface renderings don't read "func ... func(...)".
func stripFuncKeyword(sig string) string {
	return strings.TrimPrefix(sig, "func")
}

// writeTypedCalls renders a function's internal callees inline, looked up in the
// type-checked graph by its qualified name (e.g. "pkg.Fn" or "pkg.(*T).M").
// It renders nothing when the name isn't a node (e.g. under the AST fallback).
func writeTypedCalls(b *strings.Builder, cg *goparser.CallGraph, qualified, indent string) {
	info := cg.GetFunction(qualified)
	if info == nil || len(info.Callees) == 0 {
		return
	}
	b.WriteString(indent + "Calls: " + joinCode(uniqueCalleeNames(info)) + "\n")
}

// Markdown formats the summary as markdown.
func Markdown(sum *Summary) (string, error) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# witc summary: %s\n\n", sum.Root))

	if !sum.NoStructure && len(sum.Paths) > 0 {
		b.WriteString("## Structure\n\n```\n")
		b.WriteString(filepath.Base(sum.Root) + "\n")
		b.WriteString(treeFromPaths(sum.Paths))
		b.WriteString("```\n\n")
	}

	// Sort packages for deterministic output
	pkgNames := make([]string, 0, len(sum.Packages))
	for k := range sum.Packages {
		pkgNames = append(pkgNames, k)
	}
	sort.Strings(pkgNames)

	b.WriteString("## Packages\n\n")
	for _, pkg := range pkgNames {
		r := sum.Packages[pkg]
		if r == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", pkg))

		for _, s := range r.Structs {
			// Struct with fields summary
			if len(s.Fields) > 0 {
				fieldStrs := make([]string, 0, len(s.Fields))
				for _, f := range s.Fields {
					if f.Name != "" {
						fieldStrs = append(fieldStrs, f.Name+" "+f.Type)
					} else {
						fieldStrs = append(fieldStrs, f.Type)
					}
				}
				b.WriteString(fmt.Sprintf("- `type %s struct { %s }`\n", s.Name, strings.Join(fieldStrs, "; ")))
			} else if len(s.Methods) > 0 {
				b.WriteString(fmt.Sprintf("- `type %s struct`\n", s.Name))
			}
			for _, m := range s.Methods {
				b.WriteString(fmt.Sprintf("  - `func (%s) %s%s`\n", m.Receiver, m.Name, stripFuncKeyword(m.Signature)))
				qualified := r.Package + ".(" + m.Receiver + ")." + m.Name
				writeTypedCalls(&b, sum.CallGraph, qualified, "    ")
			}
		}

		for _, iface := range r.Interfaces {
			if len(iface.Methods) > 0 {
				methStrs := make([]string, 0, len(iface.Methods))
				for _, m := range iface.Methods {
					if m.Name != "" {
						methStrs = append(methStrs, m.Name+stripFuncKeyword(m.Signature))
					} else {
						methStrs = append(methStrs, m.Signature)
					}
				}
				b.WriteString(fmt.Sprintf("- `type %s interface { %s }`\n", iface.Name, strings.Join(methStrs, "; ")))
			} else {
				b.WriteString(fmt.Sprintf("- `type %s interface`\n", iface.Name))
			}
		}

		for _, fn := range r.Functions {
			sig := fn.Signature
			if strings.HasPrefix(sig, "func") {
				sig = "func " + fn.Name + sig[4:]
			} else {
				sig = "func " + fn.Name + " " + sig
			}
			b.WriteString(fmt.Sprintf("- `%s`\n", sig))

			writeTypedCalls(&b, sum.CallGraph, r.Package+"."+fn.Name, "  ")
		}

		b.WriteString("\n")
	}

	// Add Call Graph section after Packages
	b.WriteString("\n## Call Graph\n\n")

	if sum.CallGraph != nil && len(sum.CallGraph.Functions) > 0 {
		b.WriteString("### Function Relationships\n\n")
		b.WriteString("| Function | Calls | Called By |\n")
		b.WriteString("|----------|-------|-----------|\n")

		funcNames := make([]string, 0, len(sum.CallGraph.Functions))
		for name := range sum.CallGraph.Functions {
			funcNames = append(funcNames, name)
		}
		sort.Strings(funcNames)

		for _, funcName := range funcNames {
			callList := sum.CallGraph.Functions[funcName]
			callerMap := make(map[string]bool)
			for _, c := range callList.Callers {
				callerMap[c.Name] = true
			}
			callers := make([]string, 0, len(callerMap))
			for name := range callerMap {
				callers = append(callers, name)
			}
			sort.Strings(callers)

			callersStr := strings.Join(callers, ", ")
			b.WriteString(fmt.Sprintf("| `%s` | %d | %s |\n", funcName, len(callList.Callees), callersStr))
		}

		b.WriteString("\n### Entry Points\n\n")
		entryPoints := entryPointNames(sum.CallGraph)
		if len(entryPoints) > 0 {
			for _, ep := range entryPoints {
				b.WriteString(fmt.Sprintf("- `%s`\n", ep))
			}
		} else {
			b.WriteString("_No clear entry points detected_\n")
		}

		b.WriteString("\n### Leaf Functions\n\n")
		leafFuncs := findLeafFunctions(sum.CallGraph)
		if len(leafFuncs) > 0 {
			for _, lf := range leafFuncs {
				b.WriteString(fmt.Sprintf("- `%s`\n", lf))
			}
		} else {
			b.WriteString("_No leaf functions detected_\n")
		}

		b.WriteString("\n### Cross-File Dependencies\n\n")
		showCrossFileDeps(&b, sum.CallGraph)
	} else {
		b.WriteString("*No call graph data available*\n")
	}

	b.WriteString("\n## Metrics\n\n")

	if sum.CallGraph != nil && len(sum.CallGraph.Functions) > 0 {
		metrics := goparser.CalculateMetrics(sum.CallGraph)

		if metrics.TotalFunctions > 0 {
			b.WriteString("### Overview\n\n")
			b.WriteString(fmt.Sprintf("- **Total Functions:** %d\n", metrics.TotalFunctions))
			b.WriteString(fmt.Sprintf("- **Total Calls:** %d\n", metrics.TotalCalls))
			b.WriteString(fmt.Sprintf("- **Average Callees per Function:** %.2f\n", metrics.AvgCalleesPerFunc))

			if metrics.TotalCalls > 0 {
				externalPct := float64(metrics.ExternalCalls) / float64(metrics.TotalCalls) * 100
				b.WriteString(fmt.Sprintf("- **External Calls:** %d (%.1f%%)\n", metrics.ExternalCalls, externalPct))
			}

			if metrics.MaxFanIn != "" {
				b.WriteString(fmt.Sprintf("- **Most Called Function:** `%s` (called by %d functions)\n",
					metrics.MaxFanIn, metrics.MaxFanInCount))
			}

			if metrics.MaxFanOut != "" {
				b.WriteString(fmt.Sprintf("- **Highest Fan-out:** `%s` (calls %d other functions)\n",
					metrics.MaxFanOut, metrics.MaxFanOutCount))
			}

			if len(metrics.HighCouplingFuncs) > 0 {
				b.WriteString("\n### High Coupling Functions\n\n")
				b.WriteString("Functions with many dependencies (may indicate refactoring opportunities):\n\n")
				displayCount := len(metrics.HighCouplingFuncs)
				if displayCount > 10 {
					displayCount = 10
				}
				for _, fn := range metrics.HighCouplingFuncs[:displayCount] {
					b.WriteString(fmt.Sprintf("- `%s`\n", fn))
				}
			}
		} else {
			b.WriteString("*No metrics available*\n")
		}
	} else {
		b.WriteString("*No call graph data available*\n")
	}

	b.WriteString(GenerateCallSummary(sum.CallGraph))
	b.WriteString(GenerateDependencyMap(sum.CallGraph))

	// Trace execution flow for a handful of entry points that actually drive
	// calls, so the section stays useful without dumping every function.
	if sum.CallGraph != nil {
		const maxFlows = 6
		count := 0
		for _, ep := range entryPointNames(sum.CallGraph) {
			info := sum.CallGraph.GetFunction(ep)
			if info == nil || len(info.Callees) == 0 {
				continue
			}
			b.WriteString(GenerateCallFlow(ep, sum.CallGraph))
			if count++; count >= maxFlows {
				break
			}
		}
	}

	return b.String(), nil
}

func findLeafFunctions(cg *goparser.CallGraph) []string {
	var leaves []string
	for funcName := range cg.Functions {
		if len(cg.Functions[funcName].Callees) == 0 {
			leaves = append(leaves, funcName)
		}
	}
	leaves = deduplicateStrings(leaves)
	sort.Strings(leaves)
	return leaves
}

func showCrossFileDeps(b *strings.Builder, cg *goparser.CallGraph) {
	hasCrossFile := false

	for callee := range cg.Functions {
		filesForCallee := make(map[string]bool)
		for _, callerInfo := range cg.Functions {
			for _, caller := range callerInfo.Callers {
				if caller.Name == callee {
					filesForCallee[caller.File] = true
				}
			}
		}

		if len(filesForCallee) > 1 {
			hasCrossFile = true
			break
		}
	}

	if hasCrossFile {
		b.WriteString("Functions called from multiple files:\n\n")
		for callee := range cg.Functions {
			files := make(map[string]bool)
			for _, callerInfo := range cg.Functions {
				for _, caller := range callerInfo.Callers {
					if caller.Name == callee {
						files[caller.File] = true
					}
				}
			}

			if len(files) > 1 {
				b.WriteString(fmt.Sprintf("- `%s` (called from %d files)\n", callee, len(files)))
				for f := range files {
					b.WriteString(fmt.Sprintf("  - `%s`\n", filepath.Base(f)))
				}
			}
		}
	} else {
		b.WriteString("_No cross-file dependencies detected_\n")
	}
}

func treeFromPaths(paths []string) string {
	sort.Strings(paths)
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range paths {
		prefix := "├── "
		if i == len(paths)-1 {
			prefix = "└── "
		}
		b.WriteString(prefix + p + "\n")
	}
	return b.String()
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

func deduplicateStrings(s []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, str := range s {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}

