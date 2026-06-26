# witc Usefulness Roadmap

Status as of this writing: the call graph is type-checked and correct (see
`plans/callgraph/PLAN.md` for the now-completed callgraph work). The items below
are the next steps to make `witc` genuinely useful for its stated purpose —
**summarizing codebases to fit LLM context windows, optimized for AI
understanding**.

Each item lists the problem, the evidence in the code, a proposed approach, and
acceptance criteria so it can be picked up independently. They are ordered by
value-to-effort. Start at the top.

> **Progress:** Items 1 (doc comments), 2 (token/size budgeting), 3 (default
> selectivity), and 4 (package-level orientation) are done. Next up: item 5
> (stable JSON schema).

> Out of scope for now (deliberately deferred): multi-language support. The
> `processor.Processor` interface is generic but only Go is wired. This is a
> separate, larger effort and is not part of this roadmap.

---

## 1. Capture doc comments (highest signal-per-token win)

**Problem.** The single most useful content for an LLM understanding code — the
doc comments — is parsed and then discarded. Output gives a signature like
`func Process(ctx, path, src) (*Result, error)` but not "*Process parses Go
source and extracts the API surface.*" The second line is worth more than the
signature and is already available in the AST.

**Evidence.**
- `internal/processor/go/go.go:30` parses with `parser.ParseComments`, but no
  `.Doc` field is ever read.
- `internal/processor/result.go` — `Struct`, `Interface`, `Function`, `Method`
  carry no doc field.

**Approach.**
- Add a `Doc string` field to `Function`, `Method`, `Struct`, `Interface` in
  `internal/processor/result.go` (and a package-level doc on `Result`).
- In `go.go`, read `FuncDecl.Doc`, `GenDecl.Doc`/`TypeSpec.Doc`, and the package
  doc (`ast.File.Doc` / `ast.Package` doc). Trim to the first sentence or first
  line by default to control size (full text only at higher detail levels — see
  item 2).
- Render the doc in `internal/formatter/markdown.go` under each symbol, and
  include it in JSON.

**Acceptance.**
- `witc summarize .` shows a one-line doc under exported symbols that have one.
- Symbols without docs render unchanged.
- A test asserts the doc text is extracted for an exported function.

---

## 2. Token / size budgeting (the core promise)

**Problem.** There is no control over output size, yet "optimized for context
windows" is the whole point. On *witc itself* (~2,000 lines) the markdown output
is already ~26 KB (~6,500 tokens). This scales roughly linearly, so a real
100k-line repo produces output that blows any context window.

**Evidence.**
- `cmd/witc/main.go` writes the full markdown/JSON unconditionally; no size flag.
- `internal/formatter/markdown.go` emits every package, function, call-graph row,
  and metrics block with no cap.

**Approach.**
- Add `--max-tokens N` (and/or `--detail {low,medium,high}`).
- Define **tiers** of content and drop/summarize from the bottom as the budget
  tightens:
  1. Always: package list + exported API surface (signatures + first-line docs).
  2. Then: call-graph relationships and entry points.
  3. Then: per-function inline calls, metrics, execution-flow traces.
- Use the **importance signals we already compute** (fan-in/fan-out from
  `internal/processor/go/callgraph_metrics.go`) to rank which functions survive
  when truncating — keep high-fan-in/fan-out nodes, drop leaf trivia.
- Token counting can start as a cheap heuristic (chars/4) and be refined later.

**Acceptance.**
- `witc summarize . --max-tokens 2000` produces output under the budget that
  still contains the package list and exported API.
- Truncation is deterministic and notes what was omitted (e.g. a trailing
  "… N more functions omitted" line).

---

## 3. Default selectivity — signal over noise

**Problem.** Test files and unexported helpers are dumped into the API surface by
default, burning tokens on low-value content. An agent wants the exported API
first.

**Evidence.**
- `internal/scanner/scanner.go:17-22` skips only the `testdata` directory, not
  `_test.go` files — so test functions and helpers (e.g. `makeGraph`,
  `TestXxx`) appear in the API surface.
- `--exclude-tests` (`internal/processor/go/go.go:101`) only filters
  `func TestXxx(*testing.T)`, not test helpers or whole `_test.go` files, and is
  off by default.

**Approach.**
- Exclude `_test.go` files by default in the scanner; add `--include-tests` to
  opt back in.
- Lead the API surface with exported symbols; demote or collapse unexported
  internals (e.g. under a "Internal helpers (N)" summary line) unless detail
  level is high.

**Acceptance.**
- By default, output contains no `Test*` functions or `_test.go`-only helpers.
- Exported symbols are listed before unexported ones.

---

## 4. Package-level orientation

**Problem.** Output jumps straight to per-function detail. An agent dropped into a
repo first needs "what is this package, and what depends on what" — the
high-altitude map — before function-level callee lists.

**Evidence.**
- `internal/formatter/markdown.go` renders packages as flat symbol lists; there
  is no package role summary or package→package dependency graph.
- The data now exists: `CallGraph.ExternalDeps` and the per-node `Package` field
  in `internal/processor/go/callgraph_aggregate.go`.

**Approach.**
- Add a short per-package summary: its doc (from item 1), symbol counts, and the
  set of in-module packages it calls into (derive an internal package→package
  DAG from the typed call graph the same way `ExternalDeps` is built).
- Render a top-level "Architecture" section: the package dependency DAG plus
  entry points, before the per-package detail.

**Acceptance.**
- Output has an architecture/overview section showing package → package
  dependencies for the analyzed module.

---

## 5. Stable, documented JSON schema

**Problem.** `JSON()` marshals internal Go structs directly, so the
"machine-readable" output is whatever the internal types happen to be today —
brittle for any tool or agent consuming it.

**Evidence.**
- `internal/formatter/json.go` just calls `json.MarshalIndent(sum, ...)` over
  `formatter.Summary`, which embeds `processor.Result` and `goparser.CallGraph`
  internals.

**Approach.**
- Define an explicit, versioned output schema (a dedicated set of DTO types with
  a `schemaVersion` field) decoupled from internal structs.
- Document it (fields, meaning, stability guarantees) in this repo.

**Acceptance.**
- JSON output has a `schemaVersion` and a documented shape that does not change
  when internal types are refactored.

---

## Lower priority / later

- **Targeted queries.** `witc summarize ./somepkg` (analyze a subtree) and
  reverse lookups like "who calls function X" — agents often want a focused
  slice, not a whole-repo dump.
- **Large-repo performance.** `BuildTypedCallGraph` (`internal/processor/go/typed.go`)
  loads the entire module into memory via `go/packages`. For large monorepos this
  needs scoping (load only the requested subtree) and/or streaming output.
- **Slim the AST fallback.** With the typed graph as primary, the AST
  `CallVisitor` path now exists only as a fallback; it could be trimmed once the
  typed path is proven across more repos.

---

## Context for whoever picks this up

- The type-checked call graph is the source of truth for relationships, metrics,
  and the prose summary. See `internal/processor/go/typed.go`.
- The CLI prefers the typed graph and falls back to the AST aggregate
  (`internal/processor/go/callgraph_aggregate.go`) when a module can't be
  type-checked — keep that fallback working.
- Run `go test ./...` and `go vet ./...` after changes; `go run ./cmd/witc
  summarize .` is the quickest way to eyeball output quality on this repo itself.
