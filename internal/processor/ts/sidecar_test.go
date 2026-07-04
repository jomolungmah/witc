package tsparser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jomolungmah/witc/internal/callgraph"
)

// requireSidecar skips the test when the node sidecar can't run here: node
// must be installed and a typescript package locatable. Set WITC_TSLIB to any
// node_modules/typescript directory to run these tests on machines where
// typescript isn't resolvable from the working directory.
func requireSidecar(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	if os.Getenv("WITC_TSLIB") != "" {
		return
	}
	out, err := exec.Command("node", "-p", "require.resolve('typescript')").Output()
	if err != nil {
		t.Skip("typescript not resolvable; set WITC_TSLIB to a node_modules/typescript directory")
	}
	t.Setenv("WITC_TSLIB", strings.TrimSpace(filepath.Dir(string(out))))
}

// TestBuildTypedCallGraph_TypeInference covers what the import-resolving
// builder cannot do: member calls through typed local variables and aliases.
func TestBuildTypedCallGraph_TypeInference(t *testing.T) {
	requireSidecar(t)

	root := t.TempDir()
	writeFile(t, root, "api/client.ts", `
export class ApiClient {
  getUser(id: string): string { return id; }
}
export const spaceApi = {
  get(id: string): string { return id; },
};
export function helper(): number { return 1; }
`)
	writeFile(t, root, "src/hooks.ts", `
import { ApiClient, spaceApi, helper } from "../api/client";

const client = new ApiClient();

export function useUser(id: string): string {
  return client.getUser(id);
}

export function useSpace(id: string): string {
  const api = spaceApi;
  return api.get(id);
}

export const useCount = () => helper();
`)

	g, err := BuildTypedCallGraph(root, []string{"api/client.ts", "src/hooks.ts"}, t.Logf)
	if err != nil {
		t.Fatalf("BuildTypedCallGraph: %v", err)
	}

	// client.getUser(id): the receiver is a module-level const whose type the
	// checker infers; the import builder cannot resolve this.
	wantEdge(t, g, "src.useUser", "api.ApiClient.getUser")
	// api.get(id): the receiver is a *local* alias of an imported object const.
	wantEdge(t, g, "src.useSpace", "api.spaceApi.get")
	// helper(): plain imported identifier call from an arrow const.
	wantEdge(t, g, "src.useCount", "api.helper")

	// Declared functions are nodes even when uncalled, with their file attached.
	fn := g.Functions["src.useUser"]
	if fn == nil || len(fn.Files) == 0 || fn.Files[0] != "src/hooks.ts" {
		t.Fatalf("src.useUser node missing or without file: %+v", fn)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantEdge(t *testing.T, g *callgraph.CallGraph, caller, callee string) {
	t.Helper()
	for _, e := range g.Edges {
		if e.Caller == caller && e.Callee == callee {
			return
		}
	}
	t.Errorf("missing edge %s -> %s; edges: %v", caller, callee, g.Edges)
}
