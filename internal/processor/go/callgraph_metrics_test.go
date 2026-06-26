package goparser

import (
	"testing"

	"github.com/ai-suite/witc/internal/processor"
)

func TestCalculateMetrics_Basic(t *testing.T) {
	cg := &CallGraph{
		Functions: map[string]*FuncInfo{
			"main": {
				Name:    "main",
				Package: "pkg",
				Callees: []Callee{{Name: "Process"}},
				Callers: []Caller{},
			},
			"Process": {
				Name:    "Process",
				Package: "pkg",
				Callees: []Callee{{Name: "helper"}},
				Callers: []Caller{{Name: "main"}},
			},
			"helper": {
				Name:    "helper",
				Package: "pkg",
				Callees: []Callee{},
				Callers: []Caller{{Name: "Process"}},
			},
		},
	}

	metrics := CalculateMetrics(cg)

	if metrics.TotalFunctions != 3 {
		t.Errorf("TotalFunctions = %d, want 3", metrics.TotalFunctions)
	}
	if metrics.TotalCalls != 2 {
		t.Errorf("TotalCalls = %d, want 2", metrics.TotalCalls)
	}
}

func TestCalculateMetrics_EmptyCallGraph(t *testing.T) {
	metrics := CalculateMetrics(nil)

	if metrics.TotalFunctions != 0 {
		t.Errorf("TotalFunctions = %d, want 0", metrics.TotalFunctions)
	}
}

func TestCalculateMetrics_NullFunctions(t *testing.T) {
	cg := &CallGraph{
		Functions: nil,
	}

	metrics := CalculateMetrics(cg)

	if metrics.TotalFunctions != 0 {
		t.Errorf("TotalFunctions = %d, want 0", metrics.TotalFunctions)
	}
}

func TestCalculateCouplingScore(t *testing.T) {
	calls := []CallInfo{
		{CalleeName: "fmt.Println"},
		{CalleeName: "helper"},
		{CalleeName: "strings.Trim"},
	}

	allResults := []*processor.Result{
		{ImportPath: "mypackage"},
	}

	score := CalculateCouplingScore(calls, allResults)

	if score < 0.6 || score > 0.7 {
		t.Errorf("Coupling score = %f, want ~0.67", score)
	}
}

func TestCalculateCouplingScore_EmptyCalls(t *testing.T) {
	score := CalculateCouplingScore([]CallInfo{}, nil)

	if score != 0 {
		t.Errorf("Coupling score = %f, want 0", score)
	}
}

func TestIsExternalCall_StandardLibrary(t *testing.T) {
	if !isExternalCall("fmt.Println", "mypackage") {
		t.Error("Expected fmt.Println to be external")
	}
}

func TestIsExternalCall_LocalPackage(t *testing.T) {
	if isExternalCall("mypackage.helper", "mypackage") {
		t.Error("Expected mypackage.helper to be internal")
	}
}

func TestIsExternalCall_NoDot(t *testing.T) {
	if isExternalCall("helper", "mypackage") {
		t.Error("Expected helper without dot to be internal")
	}
}

func TestLooksLikeLocalPackage_PrefixMatch(t *testing.T) {
	if !looksLikeLocalPackage("mypkg", "mypkg.subpkg") {
		t.Error("Expected mypkg to match mypkg.subpkg")
	}
}

func TestLooksLikeLocalPackage_CommonPrefix(t *testing.T) {
	if !looksLikeLocalPackage("internal", "myapp/internal/util") {
		t.Error("Expected internal to be recognized as local")
	}
}

func TestEstimateMaxCallDepth(t *testing.T) {
	funcCallees := map[string]int{
		"main":    3,
		"Process": 5,
		"helper":  1,
	}

	depth := estimateMaxCallDepth(funcCallees)

	if depth != 5 {
		t.Errorf("Max call depth = %d, want 5", depth)
	}
}

func TestEstimateMaxCallDepth_Empty(t *testing.T) {
	depth := estimateMaxCallDepth(map[string]int{})

	if depth != 0 {
		t.Errorf("Max call depth = %d, want 0", depth)
	}
}
