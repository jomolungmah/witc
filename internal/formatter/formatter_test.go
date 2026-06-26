package formatter

import (
	"strings"
	"testing"

	"github.com/ai-suite/witc/internal/processor"
)

func TestMarkdown(t *testing.T) {
	sum := &Summary{
		Root:  "/tmp/example",
		Paths: []string{"cmd/main.go", "pkg/foo.go"},
		Packages: map[string]*processor.Result{
			"cmd": {
				Package: "main",
				Functions: []processor.Function{
					{Name: "main", Signature: "func()"},
				},
			},
			"pkg": {
				Package: "pkg",
				Structs: []processor.Struct{
					{
						Name: "Foo",
						Methods: []processor.Method{
							{Receiver: "*Foo", Name: "Bar", Signature: "func() int"},
						},
					},
				},
			},
		},
	}

	out, err := Markdown(sum)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty output")
	}
	if !strings.Contains(out, "witc summary") {
		t.Errorf("output should contain header, got: %q", out[:min(100, len(out))])
	}
	if !strings.Contains(out, "main") {
		t.Error("output should contain main")
	}
	if !strings.Contains(out, "Foo") {
		t.Error("output should contain Foo")
	}
	if !strings.Contains(out, "Bar") {
		t.Error("output should contain Bar")
	}
}

func TestMarkdown_MethodRenderingNoDoubleFunc(t *testing.T) {
	sum := &Summary{
		Root: "/tmp/example",
		Packages: map[string]*processor.Result{
			"pkg": {
				Package: "pkg",
				Structs: []processor.Struct{
					{
						Name: "Foo",
						Methods: []processor.Method{
							{Receiver: "*Foo", Name: "Bar", Signature: "func() int"},
						},
					},
				},
			},
		},
	}

	out, err := Markdown(sum)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	if !strings.Contains(out, "func (*Foo) Bar() int") {
		t.Errorf("expected clean method signature, got:\n%s", out)
	}
	if strings.Contains(out, "Bar func(") {
		t.Errorf("method rendering should not contain double 'func', got:\n%s", out)
	}
}

func TestMarkdown_ShowsOutgoingCalls(t *testing.T) {
	sum := &Summary{
		Root: "/tmp/example",
		Packages: map[string]*processor.Result{
			"pkg": {
				Package: "pkg",
				Functions: []processor.Function{
					{Name: "Caller", Signature: "func()"},
					{Name: "callee", Signature: "func()"},
				},
				// Keyed by callee; Caller calls callee at p.go:3.
				CallGraph: map[string][]processor.CallInfo{
					"callee": {{CalleeName: "callee", File: "p.go", Line: 3, ParentFunc: "Caller"}},
				},
			},
		},
	}

	out, err := Markdown(sum)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	// The outgoing call must be listed under Caller, pointing at callee.
	callerIdx := strings.Index(out, "func Caller()")
	calleeRef := strings.Index(out, "`callee` (at p.go:3)")
	if callerIdx < 0 || calleeRef < 0 {
		t.Fatalf("expected Caller to list outgoing call to callee, got:\n%s", out)
	}
	if calleeRef < callerIdx {
		t.Errorf("outgoing call should appear under Caller, got:\n%s", out)
	}
}

func TestJSON(t *testing.T) {
	sum := &Summary{
		Root:  "/tmp/example",
		Paths: []string{"main.go"},
		Packages: map[string]*processor.Result{
			".": {Package: "main", Functions: []processor.Function{{Name: "main", Signature: "func()"}}},
		},
	}

	out, err := JSON(sum)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty output")
	}
	if !strings.Contains(out, "Root") || !strings.Contains(out, "Packages") {
		t.Errorf("output should be valid JSON, got: %q", out[:min(100, len(out))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
