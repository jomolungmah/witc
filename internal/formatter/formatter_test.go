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
