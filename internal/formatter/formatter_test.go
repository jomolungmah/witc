package formatter

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ai-suite/witc/internal/processor"
	goparser "github.com/ai-suite/witc/internal/processor/go"
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
			},
		},
		// Inline calls are sourced from the type-checked graph by qualified name.
		CallGraph: &goparser.CallGraph{
			Functions: map[string]*goparser.FuncInfo{
				"pkg.Caller": {Name: "pkg.Caller", Package: "pkg", Callees: []goparser.Callee{{Name: "pkg.callee"}}},
				"pkg.callee": {Name: "pkg.callee", Package: "pkg", Callers: []goparser.Caller{{Name: "pkg.Caller"}}},
			},
		},
	}

	out, err := Markdown(sum)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	// The outgoing call must be listed under Caller, pointing at callee.
	callerIdx := strings.Index(out, "func Caller()")
	calleeRef := strings.Index(out, "Calls: `pkg.callee`")
	if callerIdx < 0 || calleeRef < 0 {
		t.Fatalf("expected Caller to list outgoing call to callee, got:\n%s", out)
	}
	if calleeRef < callerIdx {
		t.Errorf("outgoing call should appear under Caller, got:\n%s", out)
	}
}

func bigSummary(nFuncs int) *Summary {
	fns := make([]processor.Function, nFuncs)
	for i := range fns {
		fns[i] = processor.Function{
			Name:      fmt.Sprintf("Func%02d", i),
			Doc:       "Func does something moderately wordy for token accounting.",
			Signature: "func(input string, count int) (result string, err error)",
		}
	}
	return &Summary{
		Root:     "/tmp/example",
		Paths:    []string{"pkg/a.go"},
		Packages: map[string]*processor.Result{"pkg": {Package: "pkg", Functions: fns}},
		CallGraph: &goparser.CallGraph{
			Functions: map[string]*goparser.FuncInfo{},
		},
	}
}

func TestMarkdown_DetailLevels(t *testing.T) {
	sum := bigSummary(3)

	sum.Detail = detailLow
	low, _ := Markdown(sum)
	if strings.Contains(low, "## Call Graph") || strings.Contains(low, "## Metrics") {
		t.Errorf("low detail should omit call graph and metrics:\n%s", low)
	}
	if !strings.Contains(low, "## Packages") {
		t.Error("low detail must still contain the package API surface")
	}

	sum.Detail = detailMedium
	med, _ := Markdown(sum)
	if !strings.Contains(med, "## Call Graph") || !strings.Contains(med, "## Metrics") {
		t.Error("medium detail should include call graph and metrics")
	}
	if strings.Contains(med, "### Call Flow Summary") {
		t.Error("medium detail should omit the prose call-flow summary")
	}

	sum.Detail = detailHigh
	high, _ := Markdown(sum)
	if !strings.Contains(high, "### Call Flow Summary") {
		t.Error("high detail should include the prose call-flow summary")
	}
}

func TestMarkdown_MaxTokensEnforced(t *testing.T) {
	sum := bigSummary(60) // far more than a small budget can hold
	sum.MaxTokens = 1000

	out, _ := Markdown(sum)

	if got := estimateTokens(out); got > sum.MaxTokens {
		t.Errorf("output is %d tokens, over budget %d", got, sum.MaxTokens)
	}
	if !strings.Contains(out, "## Packages") {
		t.Error("budgeted output must still contain the package list")
	}
	if !strings.Contains(out, "omitted") && !strings.Contains(out, "truncated") {
		t.Error("expected a note that content was omitted/truncated")
	}
}

func TestMarkdown_UnlimitedIncludesAll(t *testing.T) {
	sum := bigSummary(60) // MaxTokens 0
	out, _ := Markdown(sum)
	if strings.Contains(out, "omitted") || strings.Contains(out, "truncated") {
		t.Errorf("unlimited output should not truncate")
	}
	if !strings.Contains(out, "Func59") {
		t.Error("unlimited output should contain every function")
	}
}

func TestMarkdown_SelectivityAndCollapse(t *testing.T) {
	sum := &Summary{
		Root: "/x",
		Packages: map[string]*processor.Result{
			"pkg": {Package: "pkg", Functions: []processor.Function{
				{Name: "helperOne", Signature: "func()"},
				{Name: "Exported", Signature: "func()"},
				{Name: "helperTwo", Signature: "func()"},
			}},
		},
	}

	sum.Detail = detailHigh
	high, _ := Markdown(sum)
	expIdx := strings.Index(high, "func Exported")
	helpIdx := strings.Index(high, "func helperOne")
	if expIdx < 0 || helpIdx < 0 {
		t.Fatalf("expected both exported and unexported funcs at high detail:\n%s", high)
	}
	if expIdx > helpIdx {
		t.Error("exported functions should be listed before unexported ones")
	}

	sum.Detail = detailMedium
	med, _ := Markdown(sum)
	if strings.Contains(med, "func helperOne") {
		t.Error("unexported helpers should be collapsed at medium detail")
	}
	if !strings.Contains(med, "2 unexported helper") {
		t.Errorf("expected collapse summary line, got:\n%s", med)
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
