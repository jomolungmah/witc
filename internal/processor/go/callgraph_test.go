package goparser

import (
	"context"
	"strings"
	"testing"
)

func TestCallInfo_TracksParentFunction(t *testing.T) {
	src := `
package pkg
	
func Outer() {
    Inner()
}

func Inner() {}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Check that ParentFunc is set correctly
	calls, ok := result.CallGraph["Inner"]
	if !ok || len(calls) == 0 {
		t.Fatal("Expected Inner to have callers")
	}

	if calls[0].ParentFunc != "Outer" {
		t.Errorf("ParentFunc = %q, want Outer", calls[0].ParentFunc)
	}
}

func TestCallInfo_TracksLocation(t *testing.T) {
	src := `
package pkg
	
func Process() {
    helper() // line 4
}

func helper() {}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	calls, ok := result.CallGraph["helper"]
	if !ok || len(calls) == 0 {
		t.Fatal("Expected helper to have callers")
	}

	// Check that Line is non-zero (actual line number may vary due to package declaration)
	if calls[0].Line == 0 {
		t.Errorf("Line = %d, want non-zero", calls[0].Line)
	}
}

func TestCallInfo_MultipleFunctions(t *testing.T) {
	src := `
package pkg
	
func A() {
    B()
    C()
}

func B() {}
func C() {
    D()
}

func D() {}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Check A calls B and C with ParentFunc = A
	callsB, ok := result.CallGraph["B"]
	if !ok || len(callsB) == 0 {
		t.Fatal("Expected B to have callers")
	}
	if callsB[0].ParentFunc != "A" {
		t.Errorf("B.ParentFunc = %q, want A", callsB[0].ParentFunc)
	}

	// Check C calls D with ParentFunc = C
	callsD, ok := result.CallGraph["D"]
	if !ok || len(callsD) == 0 {
		t.Fatal("Expected D to have callers")
	}
	if callsD[0].ParentFunc != "C" {
		t.Errorf("D.ParentFunc = %q, want C", callsD[0].ParentFunc)
	}
}

func TestCallerIdentification_SimpleFunction(t *testing.T) {
	src := `
package pkg
	
func Process() {
    helper()
}

func helper() {}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	calls, ok := result.CallGraph["helper"]
	if !ok || len(calls) == 0 {
		t.Fatal("Expected helper to have callers")
	}

	// Caller should be "Process" (the parent function)
	if calls[0].ParentFunc != "Process" {
		t.Errorf("ParentFunc = %q, want Process", calls[0].ParentFunc)
	}
}

func TestCallerIdentification_MethodCall(t *testing.T) {
	src := `
package pkg
	
type Calculator struct{}

func (c *Calculator) Process() {
    c.add(1, 2)
}

func (c *Calculator) add(a, b int) int { return a + b }
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Check that method call is identified with receiver info (c.add)
	calls, ok := result.CallGraph["c.add"]
	if !ok || len(calls) == 0 {
		t.Fatal("Expected c.add to have callers")
	}

	if calls[0].ParentFunc != "Process" {
		t.Errorf("ParentFunc = %q, want Process", calls[0].ParentFunc)
	}

	// Verify caller includes receiver info
	if calls[0].CallerName != "c.add" {
		t.Errorf("CallerName = %q, want c.add", calls[0].CallerName)
	}
}

func TestCallerIdentification_PackageFunction(t *testing.T) {
	src := `
package pkg
	
func Helper() {}

func Process() {
    Helper()
}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Check that Helper is properly identified as callee
	calls, ok := result.CallGraph["Helper"]
	if !ok || len(calls) == 0 {
		t.Fatal("Expected Helper to have callers")
	}

	if calls[0].ParentFunc != "Process" {
		t.Errorf("ParentFunc = %q, want Process", calls[0].ParentFunc)
	}
}

func TestCallerIdentification_AnonymousCall(t *testing.T) {
	src := `
package pkg
	
func Process() {
    // Call an anonymous function directly
    func() {}()
}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Anonymous function call should be identified by position
	// The callee will contain the FuncLit representation and caller will have position info
	foundAnonymousCall := false
	for callee, calls := range result.CallGraph {
		if strings.Contains(callee, "func()") {
			for _, call := range calls {
				if strings.Contains(call.CallerName, "anonymous_function") {
					foundAnonymousCall = true
					t.Logf("Found anonymous function call: callee=%q, caller=%q", callee, call.CallerName)
				}
			}
		}
	}

	if !foundAnonymousCall {
		t.Error("Expected to find anonymous function call with position-based caller")
	}
}

func TestCallerIdentification_ChainedMethod(t *testing.T) {
	src := `
package pkg
	
import "strings"

func Process() {
    strings.Trim(" test ", " ").Upper()
}
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Check that chained calls are handled (Upper or Trim depending on implementation)
	hasCaller := false
	for _, callList := range result.CallGraph {
		for _, call := range callList {
			if strings.Contains(call.CalleeName, "Trim") || strings.Contains(call.CalleeName, "Upper") {
				hasCaller = true
				break
			}
		}
	}

	if !hasCaller {
		t.Error("Expected to find chained method calls")
	}
}

func TestCallerIdentification_NestedCall(t *testing.T) {
	src := `
package pkg
	
func Process() {
    result := getAndProcess()
}

func getAndProcess() string { return "" }
`
	proc := Processor{}
	result, err := proc.Process(context.Background(), "test.go", []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	calls, ok := result.CallGraph["getAndProcess"]
	if !ok || len(calls) == 0 {
		t.Fatal("Expected getAndProcess to have callers")
	}

	if calls[0].ParentFunc != "Process" {
		t.Errorf("ParentFunc = %q, want Process", calls[0].ParentFunc)
	}
}
