package formatter

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jomolungmah/witc/internal/callgraph"
	"github.com/jomolungmah/witc/internal/processor"
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
		CallGraph: &callgraph.CallGraph{
			Functions: map[string]*callgraph.FuncInfo{
				"pkg.Caller": {Name: "pkg.Caller", Package: "pkg", Callees: []callgraph.Callee{{Name: "pkg.callee"}}},
				"pkg.callee": {Name: "pkg.callee", Package: "pkg", Callers: []callgraph.Caller{{Name: "pkg.Caller"}}},
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
		CallGraph: &callgraph.CallGraph{
			Functions: map[string]*callgraph.FuncInfo{},
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
				{Name: "Exported", Exported: true, Signature: "func()"},
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

func TestMarkdown_ArchitectureSection(t *testing.T) {
	sum := &Summary{
		Root: "/x",
		Packages: map[string]*processor.Result{
			"cmd/app":      {Package: "main", Doc: "Command app does things.", Functions: []processor.Function{{Name: "main", Signature: "func()"}}},
			"internal/svc": {Package: "svc", Structs: []processor.Struct{{Name: "S"}}},
		},
		CallGraph: &callgraph.CallGraph{Functions: map[string]*callgraph.FuncInfo{
			"main.main": {Name: "main.main", Package: "example.com/m/cmd/app", Callees: []callgraph.Callee{{Name: "svc.Do"}}},
			"svc.Do":    {Name: "svc.Do", Package: "example.com/m/internal/svc", Callers: []callgraph.Caller{{Name: "main.main"}}},
		}},
	}

	sum.Detail = detailHigh
	out, _ := Markdown(sum)
	if !strings.Contains(out, "## Architecture") {
		t.Fatal("expected Architecture section")
	}
	if !strings.Contains(out, "Command app does things.") {
		t.Error("expected package doc in architecture line")
	}
	if !strings.Contains(out, "depends on: `internal/svc`") {
		t.Errorf("expected package dependency edge, got:\n%s", out)
	}
	// Architecture must precede the per-package detail.
	if strings.Index(out, "## Architecture") > strings.Index(out, "## Packages") {
		t.Error("architecture should appear before the Packages section")
	}

	sum.Detail = detailLow
	low, _ := Markdown(sum)
	if strings.Contains(low, "## Architecture") {
		t.Error("low detail should omit the architecture section")
	}
}

func TestJSON_Schema(t *testing.T) {
	sum := &Summary{
		Root: "/tmp/example",
		Packages: map[string]*processor.Result{
			"cmd/app": {
				Package:   "main",
				Doc:       "Package main is the entry point.",
				Functions: []processor.Function{{Name: "main", Doc: "main runs the app.", Signature: "func()"}},
			},
			"internal/svc": {Package: "svc", Structs: []processor.Struct{{Name: "S", Doc: "S serves."}}},
		},
		CallGraph: &callgraph.CallGraph{
			ExternalCalls: 3,
			Functions: map[string]*callgraph.FuncInfo{
				"main.main": {Name: "main.main", Package: "example.com/m/cmd/app", Callees: []callgraph.Callee{{Name: "svc.Do"}}},
				"svc.Do":    {Name: "svc.Do", Package: "example.com/m/internal/svc", Callers: []callgraph.Caller{{Name: "main.main"}}},
			},
		},
	}

	out, err := JSON(sum)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got struct {
		SchemaVersion string `json:"schemaVersion"`
		Root          string `json:"root"`
		Packages      []struct {
			ImportPath string `json:"importPath"`
			Doc        string `json:"doc"`
		} `json:"packages"`
		CallGraph *struct {
			Functions     []map[string]any `json:"functions"`
			ExternalCalls int              `json:"externalCalls"`
		} `json:"callGraph"`
		Metrics      map[string]any `json:"metrics"`
		Architecture struct {
			EntryPoints         []string            `json:"entryPoints"`
			PackageDependencies map[string][]string `json:"packageDependencies"`
		} `json:"architecture"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON for the documented schema: %v", err)
	}

	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", got.SchemaVersion, SchemaVersion)
	}
	if got.Root != "/tmp/example" {
		t.Errorf("root = %q", got.Root)
	}
	if len(got.Packages) != 2 || got.Packages[0].ImportPath != "cmd/app" {
		t.Errorf("packages not sorted/complete: %+v", got.Packages)
	}
	if got.CallGraph == nil || got.CallGraph.ExternalCalls != 3 {
		t.Errorf("callGraph externalCalls missing: %+v", got.CallGraph)
	}
	if got.Metrics == nil {
		t.Error("metrics missing")
	}
	deps := got.Architecture.PackageDependencies["cmd/app"]
	if len(deps) != 1 || deps[0] != "internal/svc" {
		t.Errorf("package dependencies = %v, want [internal/svc]", deps)
	}
}

func TestJSON_SymbolLocations(t *testing.T) {
	sum := &Summary{
		Root: "/tmp/example",
		Packages: map[string]*processor.Result{
			"internal/svc": {
				Package:   "svc",
				Functions: []processor.Function{{Name: "Do", Signature: "func()", Loc: processor.Location{File: "internal/svc/svc.go", Line: 12}}},
				// A struct known only through its methods has no declaration site;
				// its location must be omitted rather than emitted as a zero value.
				Structs: []processor.Struct{{Name: "S"}},
			},
		},
	}

	out, err := JSON(sum)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got struct {
		Packages []struct {
			Functions []struct {
				Name     string `json:"name"`
				Location *struct {
					File string `json:"file"`
					Line int    `json:"line"`
				} `json:"location"`
			} `json:"functions"`
			Structs []struct {
				Name     string              `json:"name"`
				Location *struct{ Line int } `json:"location"`
			} `json:"structs"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Packages) != 1 {
		t.Fatalf("len(packages) = %d, want 1", len(got.Packages))
	}
	p := got.Packages[0]
	if len(p.Functions) != 1 || p.Functions[0].Location == nil {
		t.Fatalf("function location missing: %+v", p.Functions)
	}
	if loc := p.Functions[0].Location; loc.File != "internal/svc/svc.go" || loc.Line != 12 {
		t.Errorf("function location = %s:%d, want internal/svc/svc.go:12", loc.File, loc.Line)
	}
	if len(p.Structs) != 1 || p.Structs[0].Location != nil {
		t.Errorf("struct with unknown location should omit it, got %+v", p.Structs)
	}
}

func TestMarkdown_TypeScriptRendering(t *testing.T) {
	sum := &Summary{
		Root:        "/tmp/app",
		NoStructure: true,
		Packages: map[string]*processor.Result{
			"src/components": {
				Package:  "components",
				Language: "typescript",
				Interfaces: []processor.Interface{
					{Name: "UserCardProps", Exported: true, Doc: "Props for the card.", Fields: []processor.Field{
						{Name: "name", Type: "string"},
						{Name: "age?", Type: "number"},
					}},
					{Name: "ID", Exported: true, Alias: "string | number"},
				},
				Structs: []processor.Struct{
					{Name: "Store", Exported: true, Fields: []processor.Field{{Name: "items", Type: "ID[]"}}, Methods: []processor.Method{
						{Receiver: "Store", Name: "add", Exported: true, Signature: "(item: ID): void"},
					}},
				},
				Functions: []processor.Function{
					{Name: "UserCard", Exported: true, Signature: "(props: UserCardProps)"},
				},
			},
		},
	}

	out, err := Markdown(sum)
	if err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	for _, want := range []string{
		"`interface UserCardProps { name: string; age?: number }`",
		"`type ID = string | number`",
		"`class Store { items: ID[] }`",
		"  - `add(item: ID): void`",
		"`function UserCard(props: UserCardProps)`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "func UserCard") || strings.Contains(out, "type Store struct") {
		t.Error("TypeScript package should not render with Go keywords")
	}
}

func TestJSON_InterfaceFieldsAndAlias(t *testing.T) {
	sum := &Summary{
		Root: "/tmp/app",
		Packages: map[string]*processor.Result{
			"src": {
				Package:  "src",
				Language: "typescript",
				Interfaces: []processor.Interface{
					{Name: "Props", Exported: true, Fields: []processor.Field{{Name: "id", Type: "string"}}},
					{Name: "ID", Exported: true, Alias: "string | number"},
				},
			},
		},
	}

	out, err := JSON(sum)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got struct {
		Packages []struct {
			Language   string `json:"language"`
			Interfaces []struct {
				Name   string `json:"name"`
				Alias  string `json:"alias"`
				Fields []struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"fields"`
			} `json:"interfaces"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	ifaces := got.Packages[0].Interfaces
	if got.Packages[0].Language != "typescript" {
		t.Errorf("language = %q", got.Packages[0].Language)
	}
	if len(ifaces) != 2 {
		t.Fatalf("interfaces = %+v", ifaces)
	}
	if len(ifaces[0].Fields) != 1 || ifaces[0].Fields[0].Name != "id" || ifaces[0].Fields[0].Type != "string" {
		t.Errorf("Props fields = %+v", ifaces[0].Fields)
	}
	if ifaces[1].Alias != "string | number" {
		t.Errorf("ID alias = %q", ifaces[1].Alias)
	}
}
