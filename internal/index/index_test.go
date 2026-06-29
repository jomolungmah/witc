package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jomolungmah/witc/internal/scanner"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestComputeKey_ChangesWithContent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	writeFile(t, root, "pkg/b.go", "package pkg\n")
	files := []scanner.File{{Path: "a.go", Ext: ".go"}, {Path: "pkg/b.go", Ext: ".go"}}

	k1, err := ComputeKey(root, files)
	if err != nil {
		t.Fatalf("ComputeKey: %v", err)
	}

	// Identical inputs yield an identical key, regardless of slice order.
	reordered := []scanner.File{files[1], files[0]}
	if k2, _ := ComputeKey(root, reordered); k2 != k1 {
		t.Errorf("key should be order-independent: %s != %s", k1, k2)
	}

	// Editing a file (size + mtime change) must change the key.
	time.Sleep(10 * time.Millisecond)
	writeFile(t, root, "a.go", "package a\n\nvar X = 1\n")
	k3, _ := ComputeKey(root, files)
	if k3 == k1 {
		t.Error("key should change after a file is edited")
	}
}

func TestWriteFreshLoad_Roundtrip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	files := []scanner.File{{Path: "a.go", Ext: ".go"}}

	key, err := ComputeKey(root, files)
	if err != nil {
		t.Fatalf("ComputeKey: %v", err)
	}

	if Fresh(root, key) {
		t.Error("Fresh should be false before any index is written")
	}

	const payload = `{"schemaVersion":"1.1"}`
	if err := Write(root, payload, key); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !Fresh(root, key) {
		t.Error("Fresh should be true for the key the index was written with")
	}
	if Fresh(root, "different-key") {
		t.Error("Fresh should be false for a different key")
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != payload {
		t.Errorf("Load = %q, want %q", got, payload)
	}
}
