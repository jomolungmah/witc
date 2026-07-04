package callgraph

import "testing"

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

func TestMerge_CombinesGraphs(t *testing.T) {
	g1 := &CallGraph{
		Functions: map[string]*FuncInfo{
			"pkg.A": {Name: "pkg.A", Package: "pkg", Files: []string{"a.go"}, Callees: []Callee{{Name: "pkg.B"}}},
			"pkg.B": {Name: "pkg.B", Package: "pkg", Files: []string{"b.go"}, Callers: []Caller{{Name: "pkg.A"}}},
		},
		Edges:         []Edge{{Caller: "pkg.A", Callee: "pkg.B"}},
		ExternalCalls: 2,
		ExternalDeps:  map[string][]string{"pkg": {"fmt"}},
	}
	g2 := &CallGraph{
		Functions: map[string]*FuncInfo{
			"App":   {Name: "App", Package: "src/ui", Files: []string{"app.tsx"}, Callees: []Callee{{Name: "pkg.B"}}},
			"pkg.B": {Name: "pkg.B", Package: "pkg", Files: []string{"b.go"}, Callers: []Caller{{Name: "App"}}},
		},
		Edges:         []Edge{{Caller: "App", Callee: "pkg.B"}},
		ExternalCalls: 1,
	}

	m := Merge(g1, g2, nil)

	if len(m.Functions) != 3 {
		t.Fatalf("expected 3 functions after merge, got %d", len(m.Functions))
	}
	if len(m.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(m.Edges))
	}
	if m.ExternalCalls != 3 {
		t.Errorf("ExternalCalls = %d, want 3", m.ExternalCalls)
	}
	b := m.GetFunction("pkg.B")
	if b == nil || len(b.Callers) != 2 {
		t.Fatalf("expected pkg.B to keep callers from both graphs, got %+v", b)
	}
	if len(b.Files) != 1 {
		t.Errorf("expected duplicate file entries to be deduplicated, got %v", b.Files)
	}
	if len(m.ExternalDeps["pkg"]) != 1 {
		t.Errorf("expected external deps to carry over, got %v", m.ExternalDeps)
	}
}
