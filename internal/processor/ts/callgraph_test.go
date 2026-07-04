package tsparser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jomolungmah/witc/internal/callgraph"
)

func TestStripJSONC(t *testing.T) {
	in := `{
  // line comment
  "compilerOptions": {
    /* block
       comment */
    "baseUrl": ".",
    "homepage": "http://example.com", // not a comment
    "paths": {
      "@/*": ["src/*"],
    },
  },
}`
	var cfg tsconfig
	stripped := stripJSONC([]byte(in))
	if err := json.Unmarshal(stripped, &cfg); err != nil {
		t.Fatalf("stripped JSONC does not parse: %v\n%s", err, stripped)
	}
	if cfg.CompilerOptions.BaseURL != "." {
		t.Errorf("baseUrl = %q, want %q", cfg.CompilerOptions.BaseURL, ".")
	}
	if got := cfg.CompilerOptions.Paths["@/*"]; len(got) != 1 || got[0] != "src/*" {
		t.Errorf(`paths["@/*"] = %v, want [src/*]`, got)
	}
	// String contents with "//" must survive comment stripping.
	var raw map[string]map[string]any
	if err := json.Unmarshal(stripped, &raw); err != nil {
		t.Fatal(err)
	}
	if url := raw["compilerOptions"]["homepage"]; url != "http://example.com" {
		t.Errorf("homepage = %v, want the URL intact", url)
	}
}

func TestResolver(t *testing.T) {
	root := t.TempDir()
	writeTSFile(t, root, "tsconfig.json", `{
  "compilerOptions": {
    // path aliases
    "baseUrl": ".",
    "paths": { "@/*": ["src/*"] },
  }
}`)
	files := []string{
		"src/api/client.ts",
		"src/components/UserCard.tsx",
		"src/components/index.ts",
		"src/app.tsx",
	}
	r := newResolver(root, files)

	tests := []struct {
		fromDir, spec string
		wantFile      string
		wantPkg       string
	}{
		{"src/components", "../api/client", "src/api/client.ts", ""},
		{"src/app", "./api/client", "", ""}, // relative miss: unscanned target
		{"src", "./components/UserCard", "src/components/UserCard.tsx", ""},
		{"src", "./components", "src/components/index.ts", ""}, // directory index
		{"src/components", "@/api/client", "src/api/client.ts", ""},
		{".", "src/api/client", "src/api/client.ts", ""}, // baseUrl lookup
		{"src", "react", "", "react"},
		{"src", "react-dom/client", "", "react-dom"},
		{"src", "@tanstack/react-query/core", "", "@tanstack/react-query"},
	}
	for _, tc := range tests {
		file, pkg := r.resolve(tc.fromDir, tc.spec)
		if file != tc.wantFile || pkg != tc.wantPkg {
			t.Errorf("resolve(%q, %q) = (%q, %q), want (%q, %q)",
				tc.fromDir, tc.spec, file, pkg, tc.wantFile, tc.wantPkg)
		}
	}
}

func writeTSFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// buildFixture creates a small React-shaped module exercising every
// resolution path: named/default/namespace imports, barrel re-exports,
// star exports, tsconfig aliases, this-calls, static calls, and JSX.
func buildFixture(t *testing.T) *callgraph.CallGraph {
	t.Helper()
	root := t.TempDir()
	writeTSFile(t, root, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["src/*"]}}}`)
	writeTSFile(t, root, "src/util/math.ts", `
export function double(x: number): number { return x * 2; }
export function unused(): void {}
`)
	writeTSFile(t, root, "src/api/client.ts", `
import { double } from "../util/math";

export default class ApiClient {
  constructor() { this.reset(); }
  reset(): void {}
  getUser(id: string) {
    this.reset();
    return fetch("/u/" + double(Number(id)));
  }
  static fromEnv(): ApiClient { return new ApiClient(); }
}
`)
	writeTSFile(t, root, "src/components/UserCard.tsx", `
import React from "react";
export function UserCard({ name }: { name: string }) {
  return <div>{name}</div>;
}
`)
	writeTSFile(t, root, "src/components/index.ts", `
export { UserCard } from "./UserCard";
export * from "../util/math";
`)
	writeTSFile(t, root, "src/app.tsx", `
import { useState } from "react";
import Client from "@/api/client";
import * as math from "./util/math";
import { UserCard, double } from "./components";

export function App() {
  const [n] = useState(0);
  const c = Client.fromEnv();
  math.double(n);
  double(n);
  return <UserCard name="x" />;
}
`)
	paths := []string{
		"src/util/math.ts",
		"src/api/client.ts",
		"src/components/UserCard.tsx",
		"src/components/index.ts",
		"src/app.tsx",
	}
	cg, err := BuildCallGraph(root, paths)
	if err != nil {
		t.Fatalf("BuildCallGraph: %v", err)
	}
	return cg
}

func calleeNames(info *callgraph.FuncInfo) map[string]bool {
	out := map[string]bool{}
	if info == nil {
		return out
	}
	for _, c := range info.Callees {
		out[c.Name] = true
	}
	return out
}

func TestBuildCallGraph_ResolvesImports(t *testing.T) {
	cg := buildFixture(t)

	app := cg.GetFunction("src.App")
	if app == nil {
		t.Fatalf("src.App node missing; have %v", cg.GetAllFunctions())
	}
	callees := calleeNames(app)
	for _, want := range []string{
		"api.ApiClient.fromEnv", // static call on aliased default import
		"util.double",           // namespace import AND barrel star re-export
		"components.UserCard",   // JSX render via barrel named re-export
	} {
		if !callees[want] {
			t.Errorf("App should call %s; callees = %v", want, callees)
		}
	}
	if callees["useState"] || callees["react.useState"] {
		t.Errorf("react import must be external, not a node; callees = %v", callees)
	}
}

func TestBuildCallGraph_ThisAndLocalEdges(t *testing.T) {
	cg := buildFixture(t)

	getUser := cg.GetFunction("api.ApiClient.getUser")
	if getUser == nil {
		t.Fatal("api.ApiClient.getUser node missing")
	}
	callees := calleeNames(getUser)
	if !callees["api.ApiClient.reset"] {
		t.Errorf("getUser should call reset via this.reset(); callees = %v", callees)
	}
	if !callees["util.double"] {
		t.Errorf("getUser should call util.double via relative import; callees = %v", callees)
	}

	fromEnv := cg.GetFunction("api.ApiClient.fromEnv")
	if fromEnv == nil || !calleeNames(fromEnv)["api.ApiClient"] {
		t.Errorf("fromEnv should reference the class via new ApiClient(); got %+v", fromEnv)
	}
}

func TestBuildCallGraph_NodesAndExternals(t *testing.T) {
	cg := buildFixture(t)

	unused := cg.GetFunction("util.unused")
	if unused == nil {
		t.Error("uncalled exported function should still be a node")
	} else if len(unused.Callers) != 0 {
		t.Errorf("util.unused callers = %v, want none", unused.Callers)
	}

	// useState (react) and fetch (runtime global) are external calls; only
	// the npm package shows up as a dependency.
	if cg.ExternalCalls < 2 {
		t.Errorf("ExternalCalls = %d, want at least 2 (useState, fetch)", cg.ExternalCalls)
	}
	appDeps := cg.ExternalDeps["src"]
	if len(appDeps) != 1 || appDeps[0] != "react" {
		t.Errorf(`ExternalDeps["src"] = %v, want [react]`, appDeps)
	}
	if deps := cg.ExternalDeps["src/api"]; len(deps) != 0 {
		t.Errorf(`ExternalDeps["src/api"] = %v, want none (fetch is a global)`, deps)
	}

	// Node packages carry the display dir so the formatter can map them.
	if pkg := cg.GetFunction("util.double").Package; pkg != "src/util" {
		t.Errorf("util.double package = %q, want src/util", pkg)
	}
}
