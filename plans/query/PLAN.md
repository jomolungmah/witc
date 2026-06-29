# Query Mode Implementation Plan

## Goal

Let an agent locate code with witc **without loading the whole summary into its
context window**. Today `witc summarize` emits a multi-thousand-token document
that the agent must paste in wholesale, even to answer "where is `Foo`?".

The fix moves the search out of the context window and into the CLI: witc
persists the full index to disk once, and the agent runs small, targeted queries
that each return only the matching slice (tens of tokens) — the same way it
already uses `grep`/`rg` instead of reading whole files.

## Prerequisite (highest leverage): symbol locations

The JSON contract has everything **except the one field that makes a search
result actionable — a `file:line` location**. `CallInfo` carries
`File`/`Line`/`Column` (`internal/processor/result.go:39`), but the API-surface
symbols (`Struct`, `Interface`, `Function`, `Method`) do not. For
`find Foo → internal/processor/go/typed.go:142` the agent needs that location to
jump straight to the code.

This is independently useful (it improves the existing full-dump output too) and
is a hard dependency for every query command, so it ships first.

---

## Phase 1: Track symbol locations

### Data model — `internal/processor/result.go`

Add a shared location to the symbol structs:

```go
// Location is a source position for a symbol or call.
type Location struct {
    File   string // path relative to summary root
    Line   int
    Column int
}
```

Add a `Loc Location` field to `Struct`, `Interface`, `Function`, and `Method`
(`result.go:4-31`). Keep `Field` location-free for now (low value, high churn).

### Extraction — `internal/processor/go/go.go`

In `Process()`, the `*token.FileSet` (`fset`) is already in scope, so positions
are free:

- For each `*ast.TypeSpec` (struct/interface) and `*ast.FuncDecl`, call
  `fset.Position(node.Pos())` and populate `Loc`.
- The file path passed to `Process` is already the source path; store it
  relative to the scan root (see Path normalization below) so output is stable
  across machines.

Both the struct branch (`go.go:~73`) and the `FuncDecl` branch (`go.go:~118`)
get the same treatment. The typed path (`internal/processor/go/typed.go`) must
populate the same fields — verify both code paths set `Loc`, since witc falls
back from typed to AST.

### Path normalization

Locations must be **relative to the summary root** (`Summary.Root`,
`formatter.go:10`) and slash-separated, so `find` output is deterministic and
the agent can open the path directly. Normalize once at the aggregation
boundary rather than in each processor.

### Surface in existing output

- **JSON** (`internal/formatter/json.go`): add `"location": "file:line"` (or a
  nested `{file,line}`) to `jsonStruct`, `jsonInterface`, `jsonMethod`,
  `jsonFunction` (`json.go:35-64`). **Bump `SchemaVersion`** from `1.0`
  (`json.go:12`) and document the change in `docs/json-schema.md`.
- **Markdown** (`internal/formatter/markdown.go`): append a dim ` — file:line`
  suffix to each symbol heading. Keep it terse to protect the token budget.

### Tests

- Extend `internal/processor/go/goparser_test.go` to assert `Loc` is populated
  for a struct, a method, and a function in `testdata/sample.go`.
- Add a JSON golden assertion for the new `location` field.
- Cover both the typed and AST-fallback paths.

---

## Phase 2: Persist a cached index

### New command — `witc index [path]`

Builds the summary (reusing the existing processor pipeline that `summarize`
drives from `cmd/witc/main.go`) and writes it to `.witc/index.json`.

- Reuse `formatter.JSON` — the on-disk index **is** the versioned JSON schema,
  so there is one contract, not two.
- Cache key: git `HEAD` rev when in a repo, else max file mtime across scanned
  files. Store it alongside the index (`.witc/index.meta`) so queries can detect
  staleness and re-running `index` on unchanged code is a no-op.
- Add `.witc/` to the repo's own `.gitignore`; document that consumers should
  too.

This isolates the expensive step — the type-checked `go/packages` load — to one
invocation instead of paying it per query.

### Auto-build on stale/missing index

Query commands (Phase 3) check `.witc/index.meta`; if missing or stale they
build the index first (honoring `--no-typed-callgraph`-style fast paths if
present), so the agent never has to remember to run `index` manually.

---

## Phase 3: Query subcommands

Each command loads `.witc/index.json` (building it first if needed) and prints
**only** the matching slice. New file `cmd/witc/query.go`; a small read-only
query layer in `internal/index/` that unmarshals the schema and answers lookups.

| Command | Returns |
|---|---|
| `witc find <name>` | matching symbol(s): package, `file:line`, signature, first-sentence doc |
| `witc where <name>` | bare `file:line` (cheapest possible) |
| `witc callers <func>` | functions that call it (edges only) |
| `witc callees <func>` | functions it calls |
| `witc package <path>` | one package's API surface |

Conventions:
- Default output is compact text tuned for token economy; `--format json` for
  structured consumers.
- `find`/`where` match exported and unexported symbols; support a substring or
  `pkg.Name` qualifier to disambiguate, and list all matches when ambiguous.
- Exit non-zero with a one-line message on no match, so the agent can branch.

### Tests

- `internal/index/` unit tests over a fixture index covering hit, miss, and
  ambiguous-match cases.
- A CLI-level test: `index` then `find` against `testdata/`, asserting the
  result contains the expected `file:line` and excludes unrelated packages.

---

## Agent workflow (the payoff)

```
witc index            # once per code change; expensive step amortized
witc find Process     # → internal/processor/go/go.go:35  func (p *Processor) Process(...)
witc callers Process  # → a few caller names
```

Each query is a few dozen tokens versus a full-dump of several thousand. The
call graph and metrics stay on disk and surface only when explicitly queried.

## Documentation

- README: new "Querying without loading the full summary" section + the command
  table.
- `docs/json-schema.md`: the `location` field and the `SchemaVersion` bump.
- `.opencode/skill/witc/SKILL.md` + `SKILL.md`: tell the agent to prefer
  `witc find`/`where` for targeted lookups and reserve `summarize` for initial
  orientation.

## Out of scope (note for later)

- MCP server exposing the same queries as structured tool-calls — heavier
  (protocol + long-running process); revisit if structured tool integration is
  wanted. The CLI design here composes with the shell tool agents already have.
- Field-level locations.
- Fuzzy/ranked search beyond substring + qualifier matching.

## Phase ordering

1. **Phase 1** first — it is a prerequisite for everything and improves the
   current output on its own, so it is shippable independently.
2. **Phase 2** (cached index) next.
3. **Phase 3** (query commands) last, depends on both.
