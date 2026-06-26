package formatter

import (
	"strings"
	"testing"

	goparser "github.com/ai-suite/witc/internal/processor/go"
)

// makeGraph builds a CallGraph from a caller -> callees adjacency map, wiring
// both the forward (Callees) and reverse (Callers) edges.
func makeGraph(edges map[string][]string) *goparser.CallGraph {
	cg := &goparser.CallGraph{Functions: map[string]*goparser.FuncInfo{}}
	get := func(name string) *goparser.FuncInfo {
		fi := cg.Functions[name]
		if fi == nil {
			fi = &goparser.FuncInfo{Name: name, Package: "pkg"}
			cg.Functions[name] = fi
		}
		return fi
	}
	for caller, callees := range edges {
		cf := get(caller)
		for _, callee := range callees {
			ef := get(callee)
			cf.Callees = append(cf.Callees, goparser.Callee{Name: callee})
			ef.Callers = append(ef.Callers, goparser.Caller{Name: caller})
		}
	}
	return cg
}

func TestGenerateCallSummary_Empty(t *testing.T) {
	summary := GenerateCallSummary(nil)
	if !strings.Contains(summary, "0 functions") {
		t.Errorf("expected mention of zero functions, got: %q", summary)
	}
}

func TestGenerateCallSummary_WithFunctions(t *testing.T) {
	cg := makeGraph(map[string][]string{
		"main":    {"Process"},
		"Process": {"helper"},
	})
	cg.ExternalCalls = 4

	summary := GenerateCallSummary(cg)

	for _, want := range []string{"main", "Process", "helper", "4 external calls"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q, got:\n%s", want, summary)
		}
	}
}

func TestEntryPointNames(t *testing.T) {
	cg := makeGraph(map[string][]string{
		"main":    {"Process"},
		"Process": {"helper"},
	})

	eps := entryPointNames(cg)
	// main has no callers; Process and helper do.
	if len(eps) != 1 || eps[0] != "main" {
		t.Errorf("entry points = %v, want [main]", eps)
	}
}

func TestGenerateCallFlow(t *testing.T) {
	cg := makeGraph(map[string][]string{
		"main":    {"Process"},
		"Process": {"helper"},
	})

	flow := GenerateCallFlow("main", cg)
	if !strings.Contains(flow, "main") || !strings.Contains(flow, "Process") {
		t.Errorf("expected main and Process in flow, got:\n%s", flow)
	}
}

func TestGenerateCallFlow_CycleDetection(t *testing.T) {
	cg := makeGraph(map[string][]string{
		"A": {"B"},
		"B": {"A"},
	})

	flow := GenerateCallFlow("A", cg)
	if !strings.Contains(flow, "(cycle)") {
		t.Errorf("expected cycle marker in flow, got:\n%s", flow)
	}
}

func TestGenerateDependencyMap(t *testing.T) {
	cg := makeGraph(map[string][]string{"Process": {"helper"}})
	cg.ExternalCalls = 7

	summary := GenerateDependencyMap(cg)
	if !strings.Contains(summary, "7") {
		t.Errorf("expected external call count in dependency map, got: %q", summary)
	}
}

func TestIsExported(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"Main", true},
		{"process", false},
		{"Helper", true},
		{"helper", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExported(tt.name); got != tt.expected {
				t.Errorf("isExported(%q) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}
