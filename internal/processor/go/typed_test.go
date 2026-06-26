package goparser

import (
	"testing"
)

// TestBuildTypedCallGraph_ResolvesInternalEdges loads this package with full
// type information and verifies that a method's call to a package-level
// function is resolved to a single shared node (no name duplication).
func TestBuildTypedCallGraph_ResolvesInternalEdges(t *testing.T) {
	cg, err := BuildTypedCallGraph(".")
	if err != nil {
		t.Fatalf("BuildTypedCallGraph: %v", err)
	}
	if len(cg.Functions) == 0 {
		t.Fatal("expected a non-empty call graph")
	}

	// (*Processor).Process calls formatFuncType in this package.
	caller := "goparser.(*Processor).Process"
	callee := "goparser.formatFuncType"

	info := cg.GetFunction(caller)
	if info == nil {
		t.Fatalf("missing caller node %q; have %d nodes", caller, len(cg.Functions))
	}
	found := false
	for _, c := range info.Callees {
		if c.Name == callee {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q to call %q; callees: %v", caller, callee, calleeNames(info))
	}

	// The callee must also record the reverse edge.
	cInfo := cg.GetFunction(callee)
	if cInfo == nil {
		t.Fatalf("missing callee node %q", callee)
	}
	hasCaller := false
	for _, c := range cInfo.Callers {
		if c.Name == caller {
			hasCaller = true
			break
		}
	}
	if !hasCaller {
		t.Errorf("expected %q to be called by %q; callers: %v", callee, caller, callerNames(cInfo))
	}
}

func calleeNames(fi *FuncInfo) []string {
	var out []string
	for _, c := range fi.Callees {
		out = append(out, c.Name)
	}
	return out
}

func callerNames(fi *FuncInfo) []string {
	var out []string
	for _, c := range fi.Callers {
		out = append(out, c.Name)
	}
	return out
}
