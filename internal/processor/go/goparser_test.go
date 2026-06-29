package goparser

import (
	"context"
	"testing"

	"github.com/jomolungmah/witc/internal/processor"
)

func TestProcessor_Process(t *testing.T) {
	src := `
package pkg

type Foo struct {
	Name string
}

func (f *Foo) Bar() int { return 0 }

type Reader interface {
	Read(p []byte) (n int, err error)
}

func NewFoo() *Foo { return nil }
`
	proc := Processor{}
	ctx := context.Background()
	result, err := proc.Process(ctx, "pkg/foo.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.Package != "pkg" {
		t.Errorf("Package = %q, want pkg", result.Package)
	}
	if len(result.Structs) != 1 {
		t.Fatalf("len(Structs) = %d, want 1", len(result.Structs))
	}
	if result.Structs[0].Name != "Foo" {
		t.Errorf("Struct name = %q, want Foo", result.Structs[0].Name)
	}
	if len(result.Structs[0].Methods) != 1 {
		t.Errorf("len(Methods) = %d, want 1", len(result.Structs[0].Methods))
	}
	if result.Structs[0].Methods[0].Name != "Bar" {
		t.Errorf("Method name = %q, want Bar", result.Structs[0].Methods[0].Name)
	}
	if len(result.Interfaces) != 1 {
		t.Fatalf("len(Interfaces) = %d, want 1", len(result.Interfaces))
	}
	if result.Interfaces[0].Name != "Reader" {
		t.Errorf("Interface name = %q, want Reader", result.Interfaces[0].Name)
	}
	if len(result.Functions) != 1 {
		t.Fatalf("len(Functions) = %d, want 1", len(result.Functions))
	}
	if result.Functions[0].Name != "NewFoo" {
		t.Errorf("Function name = %q, want NewFoo", result.Functions[0].Name)
	}
}

func TestProcess_TracksSymbolLocations(t *testing.T) {
	src := `package pkg

type Foo struct {
	Name string
}

func (f *Foo) Bar() int { return 0 }

type Reader interface {
	Read(p []byte) (n int, err error)
}

func NewFoo() *Foo { return nil }
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "pkg/foo.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// The location's File is the path passed to Process, slash-normalized.
	const wantFile = "pkg/foo.go"

	if len(result.Structs) != 1 {
		t.Fatalf("len(Structs) = %d, want 1", len(result.Structs))
	}
	s := result.Structs[0]
	if s.Loc.File != wantFile || s.Loc.Line != 3 {
		t.Errorf("struct Loc = %s:%d, want %s:3", s.Loc.File, s.Loc.Line, wantFile)
	}
	if len(s.Methods) != 1 {
		t.Fatalf("len(Methods) = %d, want 1", len(s.Methods))
	}
	if m := s.Methods[0]; m.Loc.File != wantFile || m.Loc.Line != 7 {
		t.Errorf("method Loc = %s:%d, want %s:7", m.Loc.File, m.Loc.Line, wantFile)
	}

	if len(result.Interfaces) != 1 {
		t.Fatalf("len(Interfaces) = %d, want 1", len(result.Interfaces))
	}
	if iface := result.Interfaces[0]; iface.Loc.File != wantFile || iface.Loc.Line != 9 {
		t.Errorf("interface Loc = %s:%d, want %s:9", iface.Loc.File, iface.Loc.Line, wantFile)
	}

	if len(result.Functions) != 1 {
		t.Fatalf("len(Functions) = %d, want 1", len(result.Functions))
	}
	if fn := result.Functions[0]; fn.Loc.File != wantFile || fn.Loc.Line != 13 {
		t.Errorf("function Loc = %s:%d, want %s:13", fn.Loc.File, fn.Loc.Line, wantFile)
	}
}

func TestFormatFuncType_GroupedParamsAndNamedResults(t *testing.T) {
	src := `
package pkg

func Merge(dst, src *int) (n int, err error) { return 0, nil }
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "x.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(result.Functions) != 1 {
		t.Fatalf("len(Functions) = %d, want 1", len(result.Functions))
	}
	got := result.Functions[0].Signature
	want := "func(dst, src *int) (n int, err error)"
	if got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
}

func TestProcess_SkipsStdlibKeepsLocalCalls(t *testing.T) {
	src := `
package pkg

import (
	"fmt"
	"path/filepath"
)

func Run() {
	fmt.Println("hi")
	_ = filepath.Base("/a/b") // path/filepath was previously misclassified as local
	helper()
}

func helper() {}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "x.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if _, ok := result.CallGraph["fmt.Println"]; ok {
		t.Error("fmt.Println should be skipped as standard library")
	}
	if _, ok := result.CallGraph["filepath.Base"]; ok {
		t.Error("filepath.Base should be skipped as standard library")
	}
	calls, ok := result.CallGraph["helper"]
	if !ok || len(calls) == 0 {
		t.Fatal("local call helper should be recorded")
	}
	if calls[0].ParentFunc != "Run" {
		t.Errorf("helper ParentFunc = %q, want Run", calls[0].ParentFunc)
	}
}

func TestProcess_ExtractsDocComments(t *testing.T) {
	src := `// Package widget does widget things.
package widget

// Thing is a thing. It has more detail on a second sentence.
type Thing struct {
	Name string
}

// Do performs the thing.
func (t *Thing) Do() {}

// Reader reads things.
type Reader interface {
	Read() error
}

// New builds a Thing.
func New() *Thing { return nil }
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "widget/widget.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if result.Doc != "Package widget does widget things." {
		t.Errorf("package doc = %q", result.Doc)
	}

	// Synopsis keeps only the first sentence.
	if len(result.Structs) == 0 || result.Structs[0].Doc != "Thing is a thing." {
		t.Errorf("struct doc = %q, want first sentence only", structDoc(result))
	}
	var method processor.Method
	for _, s := range result.Structs {
		for _, m := range s.Methods {
			if m.Name == "Do" {
				method = m
			}
		}
	}
	if method.Doc != "Do performs the thing." {
		t.Errorf("method doc = %q", method.Doc)
	}
	if len(result.Interfaces) == 0 || result.Interfaces[0].Doc != "Reader reads things." {
		t.Errorf("interface doc = %q", ifaceDoc(result))
	}
	if len(result.Functions) == 0 || result.Functions[0].Doc != "New builds a Thing." {
		t.Errorf("function doc = %q", fnDoc(result))
	}
}

func structDoc(r *processor.Result) string {
	if len(r.Structs) > 0 {
		return r.Structs[0].Doc
	}
	return ""
}

func ifaceDoc(r *processor.Result) string {
	if len(r.Interfaces) > 0 {
		return r.Interfaces[0].Doc
	}
	return ""
}

func fnDoc(r *processor.Result) string {
	if len(r.Functions) > 0 {
		return r.Functions[0].Doc
	}
	return ""
}

func TestProcessor_ExcludeGenerated(t *testing.T) {
	src := `// Code generated by something. DO NOT EDIT.

package gen

type X struct{}
`
	proc := Processor{ExcludeGenerated: true}
	ctx := context.Background()
	result, err := proc.Process(ctx, "gen/gen.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.Package != "gen" {
		t.Errorf("Package = %q, want gen", result.Package)
	}
	if len(result.Structs) != 0 {
		t.Errorf("len(Structs) = %d, want 0 (generated should be skipped)", len(result.Structs))
	}
}
