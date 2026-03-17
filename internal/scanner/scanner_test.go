package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	tmp := t.TempDir()

	// Create dir structure
	os.MkdirAll(filepath.Join(tmp, "cmd", "app"), 0755)
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0755)
	os.MkdirAll(filepath.Join(tmp, "vendor", "x"), 0755)
	os.MkdirAll(filepath.Join(tmp, "node_modules", "y"), 0755)

	os.WriteFile(filepath.Join(tmp, "cmd", "app", "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmp, "pkg", "foo.go"), []byte("package pkg"), 0644)
	os.WriteFile(filepath.Join(tmp, "vendor", "x", "ignored.go"), []byte("package x"), 0644)
	os.WriteFile(filepath.Join(tmp, "node_modules", "y", "ignored.go"), []byte("package y"), 0644)

	files, err := Scan(tmp)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	found := make(map[string]bool)
	for _, f := range files {
		found[f.Path] = true
	}

	if !found["cmd/app/main.go"] {
		t.Error("expected cmd/app/main.go to be found")
	}
	if !found["pkg/foo.go"] {
		t.Error("expected pkg/foo.go to be found")
	}
	if found["vendor/x/ignored.go"] {
		t.Error("vendor files should be skipped")
	}
	if found["node_modules/y/ignored.go"] {
		t.Error("node_modules files should be skipped")
	}
}
