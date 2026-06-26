package formatter

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ai-suite/witc/internal/processor"
)

// outgoingCalls inverts a callee-keyed call map (callee -> call sites, each
// carrying ParentFunc = the calling function) into caller -> outgoing calls,
// where each returned CallInfo has CalleeName set to the function being called.
func outgoingCalls(cg map[string][]processor.CallInfo) map[string][]processor.CallInfo {
	out := make(map[string][]processor.CallInfo)
	for callee, sites := range cg {
		for _, s := range sites {
			caller := s.ParentFunc
			if caller == "" {
				continue
			}
			out[caller] = append(out[caller], processor.CallInfo{
				CalleeName: callee,
				File:       s.File,
				Line:       s.Line,
				Column:     s.Column,
				ParentFunc: caller,
			})
		}
	}
	return out
}

// GenerateCallSummary creates a natural language summary of the call graph.
func GenerateCallSummary(packages map[string]*processor.Result, cg interface{}) string {
	var b strings.Builder

	totalFuncs := 0
	totalCalls := 0

	// A function is called if it appears as a callee key with at least one
	// call site. Entry points are functions nothing calls, plus main.
	called := make(map[string]bool)
	for _, pkg := range packages {
		if pkg == nil {
			continue
		}
		totalFuncs += len(pkg.Functions)
		for callee, calls := range pkg.CallGraph {
			totalCalls += len(calls)
			if len(calls) > 0 {
				called[callee] = true
			}
		}
	}

	var entryPoints []string
	for _, pkg := range packages {
		if pkg == nil {
			continue
		}
		for _, fn := range pkg.Functions {
			if fn.Name == "main" || !called[fn.Name] {
				entryPoints = append(entryPoints, fn.Name)
			}
		}
	}
	entryPoints = deduplicateStrings(entryPoints)

	b.WriteString("### Call Flow Summary\n\n")

	if len(entryPoints) > 0 {
		b.WriteString("The codebase has ")
		b.WriteString(strconv.Itoa(totalFuncs))
		b.WriteString(" functions with ")
		b.WriteString(strconv.Itoa(totalCalls))
		b.WriteString(" total calls. Entry points include: ")

		for i, ep := range entryPoints {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("`")
			b.WriteString(ep)
			b.WriteString("`")
		}
		b.WriteString(".\n\n")
	} else {
		b.WriteString("The codebase contains ")
		b.WriteString(strconv.Itoa(totalFuncs))
		b.WriteString(" functions with ")
		b.WriteString(strconv.Itoa(totalCalls))
		b.WriteString(" total calls.\n\n")
	}

	for pkgName, pkg := range packages {
		if pkg == nil || len(pkg.Functions) == 0 {
			continue
		}

		b.WriteString("**Package `")
		b.WriteString(pkgName)
		b.WriteString("`:**\n\n")

		outgoing := outgoingCalls(pkg.CallGraph)
		for _, fn := range pkg.Functions {
			if calls := outgoing[fn.Name]; len(calls) > 0 {
				b.WriteString(generateFunctionSummary(fn.Name, calls))
			}
		}

		var pkgEntryPoints []string
		for _, fn := range pkg.Functions {
			if fn.Name == "main" || isExported(fn.Name) {
				pkgEntryPoints = append(pkgEntryPoints, fn.Name)
			}
		}

		if len(pkgEntryPoints) > 0 {
			b.WriteString("\nKey entry points in this package: ")
			for i, ep := range pkgEntryPoints {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString("`")
				b.WriteString(ep)
				b.WriteString("`")
			}
			b.WriteString(".\n\n")
		} else {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// generateFunctionSummary creates a natural language description of a function's call relationships.
func generateFunctionSummary(funcName string, calls []processor.CallInfo) string {
	var b strings.Builder

	calleeSet := make(map[string]bool)
	for _, call := range calls {
		calleeSet[call.CalleeName] = true
	}

	numCallees := len(calleeSet)

	b.WriteString("- `")
	b.WriteString(funcName)
	b.WriteString("` ")

	if numCallees == 0 {
		b.WriteString("is a leaf function (calls no other functions).\n\n")
		return b.String()
	}

	b.WriteString("calls ")
	b.WriteString(strconv.Itoa(numCallees))
	b.WriteString(" ")
	if numCallees == 1 {
		b.WriteString("other function: ")
	} else {
		b.WriteString("other functions, including: ")
	}

	count := 0
	for callee := range calleeSet {
		if count >= 3 {
			b.WriteString(", and others")
			break
		}

		if count > 0 {
			b.WriteString(", ")
		}
		b.WriteString("`")
		b.WriteString(callee)
		b.WriteString("`")
		count++
	}

	if len(calls) > 0 {
		loc := calls[0]
		b.WriteString(" (called at `")
		b.WriteString(filepath.Base(loc.File))
		b.WriteString(":")
		b.WriteString(strconv.Itoa(int(loc.Line)))
		b.WriteString("`)")
	}

	b.WriteString(".\n\n")

	return b.String()
}

// formatNumber formats a number with words for readability.
func formatNumber(n int) string {
	if n == 0 {
		return "no"
	}
	if n == 1 {
		return "one"
	}
	if n < 20 {
		words := []string{"zero", "one", "two", "three", "four", "five",
			"six", "seven", "eight", "nine", "ten", "eleven", "twelve",
			"thirteen", "fourteen", "fifteen", "sixteen", "seventeen",
			"eighteen", "nineteen"}
		if n < len(words) {
			return words[n]
		}
	}

	if n < 100 {
		tens := []string{"", "", "twenty", "thirty", "forty", "fifty",
			"sixty", "seventy", "eighty", "ninety"}
		t := n / 10
		u := n % 10

		if t > 0 && t < len(tens) {
			result := tens[t]
			if u > 0 {
				words := []string{"zero", "one", "two", "three", "four", "five",
					"six", "seven", "eight", "nine"}
				if u < len(words) {
					result += "-" + words[u]
				} else {
					result += string(rune('0' + u))
				}
			}
			return result
		}
	}

	if n >= 100 && n <= 999 {
		hundreds := n / 100
		remainder := n % 100

		hundredsWords := []string{"zero", "one", "two", "three", "four", "five",
			"six", "seven", "eight", "nine"}

		if hundreds < len(hundredsWords) {
			result := hundredsWords[hundreds] + " hundred"
			if remainder > 0 {
				result += " " + formatNumber(remainder)
			}
			return result
		}
	}

	return strconv.Itoa(n)
}

// GenerateCallFlow describes the execution flow for a specific entry point.
func GenerateCallFlow(entryPoint string, packages map[string]*processor.Result) string {
	var b strings.Builder

	b.WriteString("### Execution Flow: `")
	b.WriteString(entryPoint)
	b.WriteString("`\n\n")

	// Merge per-package outgoing-call maps into a single caller -> callees view.
	outgoing := make(map[string][]processor.CallInfo)
	for _, pkg := range packages {
		if pkg == nil {
			continue
		}
		for caller, calls := range outgoingCalls(pkg.CallGraph) {
			outgoing[caller] = append(outgoing[caller], calls...)
		}
	}

	visited := make(map[string]bool)
	traverseCallFlow(entryPoint, outgoing, &b, visited, 0)

	return b.String()
}

func traverseCallFlow(funcName string, outgoing map[string][]processor.CallInfo, b *strings.Builder, visited map[string]bool, depth int) {
	indent := strings.Repeat("  ", depth)

	if visited[funcName] {
		b.WriteString(indent + "- `" + funcName + "` *cycle detected*\n")
		return
	}
	visited[funcName] = true

	calls := outgoing[funcName]
	if len(calls) == 0 {
		b.WriteString(indent + "- `" + funcName + "` (no outgoing calls)\n")
		return
	}

	b.WriteString(indent + "- `" + funcName + "` calls:\n")
	for _, call := range calls {
		if depth < 3 {
			traverseCallFlow(call.CalleeName, outgoing, b, visited, depth+1)
		} else {
			b.WriteString(indent + "  - `" + call.CalleeName + "` (depth limit)\n")
		}
	}
}

// GenerateDependencyMap creates a simplified dependency map for the package.
func GenerateDependencyMap(packages map[string]*processor.Result) string {
	var b strings.Builder

	b.WriteString("### Package Dependencies\n\n")

	stdLibPkgs := []string{"fmt", "strings", "bytes", "io", "os", "path", "strconv", "time", "log"}

	for pkgName, pkg := range packages {
		if pkg == nil {
			continue
		}

		externalCalls := make(map[string]bool)

		for _, calls := range pkg.CallGraph {
			for _, call := range calls {
				for _, stdPkg := range stdLibPkgs {
					if strings.HasPrefix(call.CalleeName, stdPkg+".") {
						externalCalls[stdPkg] = true
						break
					}
				}
			}
		}

		if len(externalCalls) > 0 {
			b.WriteString("**`")
			b.WriteString(pkgName)
			b.WriteString("`** uses:\n\n")

			for stdPkg := range externalCalls {
				b.WriteString("- `")
				b.WriteString(stdPkg)
				b.WriteString("`\n")
			}

			b.WriteString("\n")
		}
	}

	return b.String()
}
