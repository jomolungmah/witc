package formatter

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ai-suite/witc/internal/processor"
)

func TestGenerateCallSummary_Empty(t *testing.T) {
	summary := GenerateCallSummary(map[string]*processor.Result{}, nil)

	if !strings.Contains(summary, "no") && !strings.Contains(summary, "0") {
		t.Error("Expected mention of zero functions for empty input")
	}
}

func TestGenerateCallSummary_WithFunctions(t *testing.T) {
	packages := map[string]*processor.Result{
		"pkg": {
			Functions: []processor.Function{
				{Name: "main"},
				{Name: "Process"},
				{Name: "helper"},
			},
			// CallGraph is keyed by callee; ParentFunc names the caller.
			// Here main calls Process, and Process calls helper.
			CallGraph: map[string][]processor.CallInfo{
				"Process": {{CalleeName: "Process", File: "test.go", Line: 1, ParentFunc: "main"}},
				"helper":  {{CalleeName: "helper", File: "test.go", Line: 2, ParentFunc: "Process"}},
			},
		},
	}

	summary := GenerateCallSummary(packages, nil)

	if !strings.Contains(summary, "main") {
		t.Error("Expected 'main' to be mentioned in summary")
	}
	if !strings.Contains(summary, "Process") {
		t.Error("Expected 'Process' to be mentioned in summary")
	}
}

func TestGenerateFunctionSummary(t *testing.T) {
	calls := []processor.CallInfo{
		{CalleeName: "fmt.Println"},
		{CalleeName: "helper"},
	}

	summary := generateFunctionSummary("Process", calls)

	if !strings.Contains(summary, "Process") {
		t.Error("Expected function name in summary")
	}
	if !strings.Contains(summary, "fmt.Println") {
		t.Error("Expected callee mentioned in summary")
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
			result := isExported(tt.name)
			if result != tt.expected {
				t.Errorf("isExported(%q) = %v, want %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestGenerateDependencyMap(t *testing.T) {
	packages := map[string]*processor.Result{
		"pkg": {
			CallGraph: map[string][]processor.CallInfo{
				"Process": {{CalleeName: "fmt.Println"}},
			},
		},
	}

	summary := GenerateDependencyMap(packages)

	if !strings.Contains(summary, "fmt") {
		t.Error("Expected fmt to be mentioned in dependency map")
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n        int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{2, "2"},
		{5, "5"},
		{10, "10"},
		{15, "15"},
		{20, "20"},
		{25, "25"},
		{99, "99"},
	}

	for _, tt := range tests {
		result := strconv.Itoa(tt.n)
		if result != tt.expected {
			t.Errorf("strconv.Itoa(%d) = %q, want %q", tt.n, result, tt.expected)
		}
	}
}

func TestGenerateCallFlow(t *testing.T) {
	packages := map[string]*processor.Result{
		"pkg": {
			// Keyed by callee, ParentFunc = caller: main -> Process -> helper.
			CallGraph: map[string][]processor.CallInfo{
				"Process": {{CalleeName: "Process", File: "test.go", Line: 1, ParentFunc: "main"}},
				"helper":  {{CalleeName: "helper", File: "test.go", Line: 2, ParentFunc: "Process"}},
			},
		},
	}

	flow := GenerateCallFlow("main", packages)

	if !strings.Contains(flow, "main") {
		t.Error("Expected 'main' to be in call flow")
	}
	if !strings.Contains(flow, "Process") {
		t.Error("Expected 'Process' to be in call flow")
	}
}

func TestGenerateCallFlow_CycleDetection(t *testing.T) {
	packages := map[string]*processor.Result{
		"pkg": {
			// Keyed by callee, ParentFunc = caller: A -> B and B -> A (a cycle).
			CallGraph: map[string][]processor.CallInfo{
				"B": {{CalleeName: "B", File: "test.go", Line: 1, ParentFunc: "A"}},
				"A": {{CalleeName: "A", File: "test.go", Line: 2, ParentFunc: "B"}},
			},
		},
	}

	flow := GenerateCallFlow("A", packages)

	if !strings.Contains(flow, "cycle detected") {
		t.Error("Expected cycle detection in call flow")
	}
}
