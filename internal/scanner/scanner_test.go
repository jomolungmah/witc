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
	os.WriteFile(filepath.Join(tmp, "pkg", "foo_test.go"), []byte("package pkg"), 0644)
	os.WriteFile(filepath.Join(tmp, "vendor", "x", "ignored.go"), []byte("package x"), 0644)
	os.WriteFile(filepath.Join(tmp, "node_modules", "y", "ignored.go"), []byte("package y"), 0644)

	files, err := Scan(tmp, Options{Extensions: []string{".go"}})
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
	if found["pkg/foo_test.go"] {
		t.Error("_test.go files should be skipped by default")
	}
	if found["vendor/x/ignored.go"] {
		t.Error("vendor files should be skipped")
	}
	if found["node_modules/y/ignored.go"] {
		t.Error("node_modules files should be skipped")
	}
}

func TestScan_IncludeTests(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "foo.go"), []byte("package pkg"), 0644)
	os.WriteFile(filepath.Join(tmp, "foo_test.go"), []byte("package pkg"), 0644)

	files, err := Scan(tmp, Options{Extensions: []string{".go"}, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := make(map[string]bool)
	for _, f := range files {
		found[f.Path] = true
	}
	if !found["foo_test.go"] {
		t.Error("expected _test.go to be included when includeTests is true")
	}
}

func TestScan_MultipleExtensions(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "src", "__tests__"), 0755)

	os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "App.tsx"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "util.ts"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "util.test.ts"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "App.spec.tsx"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "__tests__", "helper.ts"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "src", "styles.css"), []byte(""), 0644)

	files, err := Scan(tmp, Options{Extensions: []string{".go", ".ts", ".tsx"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := make(map[string]bool)
	for _, f := range files {
		found[f.Path] = true
	}

	for _, want := range []string{"main.go", "src/App.tsx", "src/util.ts"} {
		if !found[filepath.FromSlash(want)] {
			t.Errorf("expected %s to be found", want)
		}
	}
	for _, skip := range []string{"src/util.test.ts", "src/App.spec.tsx", "src/__tests__/helper.ts", "src/styles.css"} {
		if found[filepath.FromSlash(skip)] {
			t.Errorf("expected %s to be skipped", skip)
		}
	}
}

func TestScan_SkipsGeneratedJS(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "dist"), 0755)
	os.MkdirAll(filepath.Join(tmp, ".next"), 0755)
	os.MkdirAll(filepath.Join(tmp, "coverage"), 0755)

	os.WriteFile(filepath.Join(tmp, "app.ts"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "types.d.ts"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "bundle.min.js"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "dist", "app.js"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, ".next", "page.js"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "coverage", "report.js"), []byte(""), 0644)

	files, err := Scan(tmp, Options{Extensions: []string{".ts", ".js"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := make(map[string]bool)
	for _, f := range files {
		found[f.Path] = true
	}

	if !found["app.ts"] {
		t.Error("expected app.ts to be found")
	}
	for _, skip := range []string{"types.d.ts", "bundle.min.js", "dist/app.js", ".next/page.js", "coverage/report.js"} {
		if found[filepath.FromSlash(skip)] {
			t.Errorf("expected %s to be skipped", skip)
		}
	}
}

func TestScan_IncludeTestsKeepsJSConventions(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "util.ts"), []byte("export {}"), 0644)
	os.WriteFile(filepath.Join(tmp, "util.test.ts"), []byte("export {}"), 0644)

	files, err := Scan(tmp, Options{Extensions: []string{".ts"}, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := make(map[string]bool)
	for _, f := range files {
		found[f.Path] = true
	}
	if !found["util.test.ts"] {
		t.Error("expected util.test.ts to be included when IncludeTests is true")
	}
}
