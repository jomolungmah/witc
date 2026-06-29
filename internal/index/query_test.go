package index

import "testing"

const fixture = `{
  "schemaVersion": "1.1",
  "root": "/tmp/m",
  "packages": [
    {
      "importPath": "internal/scanner",
      "doc": "Package scanner discovers files.",
      "structs": [
        { "name": "File", "location": {"file":"internal/scanner/scanner.go","line":12} }
      ],
      "functions": [
        { "name": "Scan", "doc": "Scan walks the tree.", "signature": "func(root string) ([]File, error)", "location": {"file":"internal/scanner/scanner.go","line":28} }
      ]
    },
    {
      "importPath": "internal/processor",
      "structs": [
        {
          "name": "Processor",
          "location": {"file":"internal/processor/processor.go","line":5},
          "methods": [
            { "receiver": "*Processor", "name": "Process", "signature": "func() error", "location": {"file":"internal/processor/processor.go","line":9} }
          ]
        }
      ],
      "functions": [
        { "name": "Scan", "signature": "func() error", "location": {"file":"internal/processor/scan.go","line":3} }
      ]
    }
  ],
  "callGraph": {
    "functions": [
      { "name": "scanner.Scan", "package": "m/internal/scanner", "callers": ["main.run"], "callees": ["scanner.match"] },
      { "name": "processor.(*Processor).Process", "package": "m/internal/processor", "callees": ["scanner.Scan"] }
    ]
  }
}`

func mustParse(t *testing.T) *Index {
	t.Helper()
	ix, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return ix
}

func TestFind_ExactAndAmbiguous(t *testing.T) {
	ix := mustParse(t)

	// "Scan" is a function in two packages; both are returned, sorted by package.
	hits := ix.Find("Scan")
	if len(hits) != 2 {
		t.Fatalf("Find(Scan) = %d hits, want 2: %+v", len(hits), hits)
	}
	if hits[0].Package != "internal/processor" || hits[1].Package != "internal/scanner" {
		t.Errorf("hits not sorted by package: %+v", hits)
	}
}

func TestFind_PkgQualified(t *testing.T) {
	ix := mustParse(t)

	hits := ix.Find("scanner.Scan")
	if len(hits) != 1 || hits[0].Package != "internal/scanner" {
		t.Fatalf("Find(scanner.Scan) = %+v, want single internal/scanner hit", hits)
	}
}

func TestFind_SubstringFallbackOnlyWhenNoExact(t *testing.T) {
	ix := mustParse(t)

	// Exact match exists ("Process"); the substring "Proc" must NOT also pull it
	// in when an exact query is used — but a non-exact query falls back.
	if hits := ix.Find("Process"); len(hits) != 1 || hits[0].Kind != "method" {
		t.Fatalf("Find(Process) = %+v, want one method", hits)
	}
	// Substring "proc" matches both Processor (struct) and Process (method),
	// returned sorted by location within the package.
	hits := ix.Find("proc")
	if len(hits) != 2 || hits[0].Name != "Processor" || hits[1].Name != "Process" {
		t.Fatalf("Find(proc) substring = %+v, want [Processor, Process]", hits)
	}
	if hits := ix.Find("Nope"); len(hits) != 0 {
		t.Errorf("Find(Nope) = %+v, want none", hits)
	}
}

func TestGraphFuncs_QualifiedAndBare(t *testing.T) {
	ix := mustParse(t)

	if gf := ix.GraphFuncs("Scan"); len(gf) != 1 || gf[0].Name != "scanner.Scan" {
		t.Fatalf("GraphFuncs(Scan) = %+v, want scanner.Scan", gf)
	}
	// Bare trailing identifier resolves a receiver-qualified node.
	gf := ix.GraphFuncs("Process")
	if len(gf) != 1 || gf[0].Name != "processor.(*Processor).Process" {
		t.Fatalf("GraphFuncs(Process) = %+v, want the Process method node", gf)
	}
	if got := gf[0].Callees; len(got) != 1 || got[0] != "scanner.Scan" {
		t.Errorf("callees = %v, want [scanner.Scan]", got)
	}
}

func TestPackage_ByBaseAndFullPath(t *testing.T) {
	ix := mustParse(t)

	if p := ix.Package("scanner"); p == nil || p.ImportPath != "internal/scanner" {
		t.Errorf("Package(scanner) = %+v, want internal/scanner", p)
	}
	if p := ix.Package("internal/processor"); p == nil || p.ImportPath != "internal/processor" {
		t.Errorf("Package(internal/processor) = %+v", p)
	}
	if p := ix.Package("missing"); p != nil {
		t.Errorf("Package(missing) = %+v, want nil", p)
	}
}
