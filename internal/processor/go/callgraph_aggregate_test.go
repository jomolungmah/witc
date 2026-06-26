package goparser

import (
	"testing"

	"github.com/jomolungmah/witc/internal/processor"
)

func TestAggregate_MergesMultipleFiles(t *testing.T) {
	result1 := &processor.Result{
		ImportPath: "pkg",
		CallGraph: map[string][]processor.CallInfo{
			"helper": {{CallerName: "Process", File: "file1.go", Line: 5, ParentFunc: "Process"}},
		},
	}

	result2 := &processor.Result{
		ImportPath: "pkg",
		CallGraph: map[string][]processor.CallInfo{
			"helper": {{CallerName: "Main", File: "file2.go", Line: 10, ParentFunc: "Main"}},
		},
	}

	cg := Aggregate([]*processor.Result{result1, result2})

	if len(cg.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(cg.Edges))
	}

	if _, ok := cg.Functions["helper"]; !ok {
		t.Error("Expected helper function in call graph")
	}

	helperInfo := cg.GetFunction("helper")
	if helperInfo == nil {
		t.Fatal("Expected helper function info")
	}
	if len(helperInfo.Callers) != 2 {
		t.Errorf("expected 2 callers for helper, got %d", len(helperInfo.Callers))
	}

	processInfo := cg.GetFunction("Process")
	if processInfo == nil {
		t.Fatal("Expected Process function info")
	}
	if len(processInfo.Callees) != 1 {
		t.Errorf("expected 1 callee for Process, got %d", len(processInfo.Callees))
	}
}

func TestAggregate_UsesParentFuncAsCaller(t *testing.T) {
	// For a plain call like helper(), CallerName holds the call-expression text
	// ("helper") which equals the callee. The real caller is ParentFunc.
	// Aggregate must attribute the edge to ParentFunc, not CallerName.
	result := &processor.Result{
		ImportPath: "pkg",
		CallGraph: map[string][]processor.CallInfo{
			"helper": {{CallerName: "helper", CalleeName: "helper", File: "f.go", Line: 5, ParentFunc: "Process"}},
		},
	}

	cg := Aggregate([]*processor.Result{result})

	proc := cg.GetFunction("Process")
	if proc == nil || len(proc.Callees) != 1 || proc.Callees[0].Name != "helper" {
		t.Fatalf("expected Process to call helper, got %+v", proc)
	}

	h := cg.GetFunction("helper")
	if h == nil || len(h.Callers) != 1 || h.Callers[0].Name != "Process" {
		t.Fatalf("expected helper to be called by Process, got %+v", h)
	}
	for _, c := range h.Callers {
		if c.Name == "helper" {
			t.Error("helper must not be recorded as its own caller")
		}
	}
}

func TestAggregate_HandlesDuplicateFunctions(t *testing.T) {
	result1 := &processor.Result{
		ImportPath: "pkg",
		CallGraph: map[string][]processor.CallInfo{
			"Process": {{CallerName: "Main", CalleeName: "Process", File: "file1.go", Line: 5, ParentFunc: "Main"}},
		},
	}

	result2 := &processor.Result{
		ImportPath: "pkg",
		CallGraph: map[string][]processor.CallInfo{
			"Process": {{CallerName: "Other", CalleeName: "Process", File: "file2.go", Line: 8, ParentFunc: "Other"}},
		},
	}

	cg := Aggregate([]*processor.Result{result1, result2})

	// Should merge into single function entry with multiple files
	if len(cg.Functions["Process"].Files) != 2 {
		t.Errorf("expected 2 files for Process, got %d", len(cg.Functions["Process"].Files))
	}
}

func TestAggregate_EmptyResults(t *testing.T) {
	cg := Aggregate([]*processor.Result{})

	if cg == nil {
		t.Fatal("expected non-nil call graph for empty results")
	}
	if len(cg.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(cg.Functions))
	}
	if len(cg.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(cg.Edges))
	}
}

func TestAggregate_NullResults(t *testing.T) {
	result := &processor.Result{CallGraph: nil}

	cg := Aggregate([]*processor.Result{result})

	if len(cg.Functions) != 0 {
		t.Errorf("expected 0 functions for null call graph, got %d", len(cg.Functions))
	}
}

func TestAggregate_NullCallGraph(t *testing.T) {
	result := &processor.Result{
		ImportPath: "pkg",
		CallGraph:  nil, // Explicitly nil
	}

	cg := Aggregate([]*processor.Result{result})

	if len(cg.Functions) != 0 {
		t.Errorf("expected 0 functions for null call graph, got %d", len(cg.Functions))
	}
}

func TestAggregate_TracksCallers(t *testing.T) {
	result := &processor.Result{
		ImportPath: "pkg",
		CallGraph: map[string][]processor.CallInfo{
			"helper": {{CallerName: "Main", File: "file.go", Line: 5, ParentFunc: "Main"}},
		},
	}

	cg := Aggregate([]*processor.Result{result})

	funcInfo := cg.GetFunction("helper")
	if funcInfo == nil {
		t.Fatal("Expected helper function info")
	}

	if len(funcInfo.Callers) != 1 {
		t.Errorf("expected 1 caller, got %d", len(funcInfo.Callers))
	}

	if funcInfo.Callers[0].Name != "Main" {
		t.Errorf("caller name = %q, want Main", funcInfo.Callers[0].Name)
	}
}

func TestAggregate_TracksMultipleCallees(t *testing.T) {
	result := &processor.Result{
		ImportPath: "pkg",
		CallGraph: map[string][]processor.CallInfo{
			"helper1": {{CallerName: "Main", File: "file.go", Line: 5, ParentFunc: "Main"}},
			"helper2": {{CallerName: "Main", File: "file.go", Line: 6, ParentFunc: "Main"}},
			"helper3": {{CallerName: "Other", File: "file.go", Line: 7, ParentFunc: "Other"}},
		},
	}

	cg := Aggregate([]*processor.Result{result})

	mainInfo := cg.GetFunction("Main")
	if mainInfo == nil {
		t.Fatal("Expected Main function info")
	}

	if len(mainInfo.Callees) != 2 {
		t.Errorf("expected 2 callees for Main, got %d", len(mainInfo.Callees))
	}

	callerNames := make(map[string]bool)
	for _, c := range mainInfo.Callees {
		callerNames[c.Name] = true
	}
	if !callerNames["helper1"] || !callerNames["helper2"] {
		t.Error("Expected Main to call both helper1 and helper2")
	}

	otherInfo := cg.GetFunction("Other")
	if otherInfo == nil {
		t.Fatal("Expected Other function info")
	}
	if len(otherInfo.Callees) != 1 || otherInfo.Callees[0].Name != "helper3" {
		t.Error("Expected Other to call helper3")
	}
}

func TestCallGraph_GetFunction(t *testing.T) {
	cg := &CallGraph{
		Functions: map[string]*FuncInfo{
			"Process": {Name: "Process"},
		},
	}

	result := cg.GetFunction("Process")
	if result == nil {
		t.Fatal("Expected Process function info")
	}
	if result.Name != "Process" {
		t.Errorf("function name = %q, want Process", result.Name)
	}
}

func TestCallGraph_GetFunction_NotFound(t *testing.T) {
	cg := &CallGraph{
		Functions: map[string]*FuncInfo{
			"Process": {Name: "Process"},
		},
	}

	result := cg.GetFunction("NotFound")
	if result != nil {
		t.Error("Expected nil for non-existent function")
	}
}

func TestCallGraph_GetAllFunctions(t *testing.T) {
	cg := &CallGraph{
		Functions: map[string]*FuncInfo{
			"A": {Name: "A"},
			"B": {Name: "B"},
			"C": {Name: "C"},
		},
	}

	names := cg.GetAllFunctions()
	if len(names) != 3 {
		t.Errorf("expected 3 functions, got %d", len(names))
	}
}

func TestCallGraph_GetEdges(t *testing.T) {
	cg := &CallGraph{
		Edges: []Edge{
			{Caller: "A", Callee: "B"},
			{Caller: "B", Callee: "C"},
		},
	}

	edges := cg.GetEdges()
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func TestCallGraph_GetEdges_Nil(t *testing.T) {
	cg := &CallGraph{}

	edges := cg.GetEdges()
	if edges == nil {
		t.Error("Expected empty slice, not nil")
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestFuncInfo_addFileIfNew(t *testing.T) {
	fi := &FuncInfo{
		Files: []string{"file1.go"},
	}

	// Add new file
	fi.addFileIfNew("file2.go")
	if len(fi.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(fi.Files))
	}

	// Try to add duplicate - should not increase count
	fi.addFileIfNew("file1.go")
	if len(fi.Files) != 2 {
		t.Errorf("expected still 2 files after duplicate, got %d", len(fi.Files))
	}

	// Empty file should be ignored
	fi.addFileIfNew("")
	if len(fi.Files) != 2 {
		t.Errorf("expected still 2 files after empty, got %d", len(fi.Files))
	}
}
