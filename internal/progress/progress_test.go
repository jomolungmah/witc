package progress

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestReporter_DisabledIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, false)
	r.Step("Parsing", 1, 10)
	r.Done("done")
	stop := r.Spin("working")
	stop()
	if buf.Len() != 0 {
		t.Errorf("disabled reporter wrote %q, want nothing", buf.String())
	}
}

func TestReporter_NilIsNoOp(t *testing.T) {
	var r *Reporter
	// Must not panic.
	r.Step("x", 1, 2)
	r.Done("x")
	stop := r.Spin("x")
	stop()
}

func TestReporter_StepRendersBarAndCount(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)
	r.Step("Parsing files", 5, 10)

	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Errorf("expected carriage return prefix, got %q", out)
	}
	if !strings.Contains(out, "Parsing files") {
		t.Errorf("missing label, got %q", out)
	}
	if !strings.Contains(out, "5/10") {
		t.Errorf("missing count, got %q", out)
	}
	if !strings.Contains(out, "█") || !strings.Contains(out, "░") {
		t.Errorf("expected a half-filled bar, got %q", out)
	}
}

func TestReporter_StepClampsAndHandlesZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)
	// Over-100% and zero-total must not panic or produce a negative pad.
	r.Step("x", 20, 10)
	r.Step("x", 1, 0)
}

func TestReporter_DoneWritesNewline(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)
	r.Step("Parsing", 1, 10)
	r.Done("Analyzed 1 file")

	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("expected trailing newline, got %q", out)
	}
	if !strings.Contains(out, "Analyzed 1 file") {
		t.Errorf("missing final message, got %q", out)
	}
}

func TestReporter_SpinStops(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)
	stop := r.Spin("working")
	stop()
	// Second call is safe (no panic, no hang).
	stop()
}

func TestReporter_LongerLineClearsPrevious(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)
	r.render("a-fairly-long-status-line")
	buf.Reset()
	r.render("short")
	out := buf.String()
	// Padding spaces must follow the shorter line to erase leftovers.
	if !strings.Contains(out, "short ") {
		t.Errorf("expected padding after shorter line, got %q", out)
	}
}

func TestIsTerminal_Pipe(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer rd.Close()
	defer wr.Close()
	if IsTerminal(wr) {
		t.Error("a pipe should not be reported as a terminal")
	}
	if IsTerminal(nil) {
		t.Error("nil file should not be reported as a terminal")
	}
}
