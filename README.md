# witc

A Go CLI that summarizes a codebase for LLM coding agents. It builds a
**type-checked call graph**, extracts the API surface **with doc comments**,
derives package-level architecture, computes metrics, and renders it all as
markdown or a versioned JSON schema — with **token budgeting** so the output
fits a context window.

Understands Go and TypeScript/JavaScript (including React: `.tsx`/`.jsx`).
Go gets the full type-checked call graph. TS/JS gets the API surface (classes,
interfaces, type aliases, enums, functions with TSDoc) and an import-resolved
call graph: relative imports, barrel re-exports, and tsconfig `baseUrl`/`paths`
aliases connect calls, `new` expressions, and JSX render edges across files,
with npm packages tracked as external dependencies. Building from source
requires a C compiler (`cgo`) for the tree-sitter parsers.

## Installation

Install the binary via

```bash
go install github.com/jomolungmah/witc/cmd/witc@latest
```

Building requires a C compiler on the PATH (the TypeScript/JavaScript parsers
use cgo). Make sure `$(go env GOPATH)/bin` (usually `~/go/bin`) is in your
`$PATH` so the installed binary is accessible in your shell.

Then copy the skill file to the agent folder of your choosing; here's an
example for OpenCode:

```bash
mkdir -p ~/.config/opencode/skills/witc && \
  curl -sSL https://raw.githubusercontent.com/jomolungmah/witc/refs/heads/main/.opencode/skill/witc/SKILL.md \
    -o ~/.config/opencode/skills/witc/SKILL.md
```

## Usage

```bash
witc summarize [path]                  # default: current directory
witc summarize ./myproject -o summary.md
witc summarize . --format json         # versioned JSON (see docs/json-schema.md)
witc summarize . --detail low          # exported API surface only
witc summarize . --max-tokens 4000     # cap output to ~4000 tokens
witc summarize . --no-structure        # omit the file tree
witc summarize . --include-tests       # include test files (_test.go, *.test.*, *.spec.*)
```

## Querying without loading the full summary

`summarize` is for initial orientation, but pasting the whole summary into an
agent's context is wasteful when it only needs to find one symbol. Instead,
build a cached index once and run targeted queries that return just the matching
slice — the search happens in the CLI, not the context window:

```bash
witc index                  # build/refresh .witc/index.json (no-op if unchanged)

witc find Scan              # symbol(s): file:line, signature, first-sentence doc
witc where Markdown         # just the file:line (cheapest possible)
witc callers ComputeKey     # in-module functions that call it
witc callees runIndex       # in-module functions it calls
witc package scanner        # one package's API surface
```

Queries auto-build the index if it is missing or stale, so `index` is optional.
Names match exactly, as `pkg.Name`, or as a case-insensitive substring; all
matches are listed when ambiguous. Add `--json` for structured output. The
`.witc/` directory is local and should be git-ignored.

| Command | Returns |
|---------|---------|
| `witc index [path]` | Build/refresh the cached index (`--force` to rebuild) |
| `witc find <name>` | Matching symbols with `file:line`, signature, and doc |
| `witc where <name>` | Bare `file:line` for each match |
| `witc callers <func>` | Functions that call it |
| `witc callees <func>` | Functions it calls |
| `witc package <path>` | One package's API surface |

## Features

### Type-checked call graph
Built with `go/packages` and full type information, so calls resolve to their
declared functions and methods:

- Callees resolve to a single shared node — `scanner.Scan()` and the declared
  `Scan` are one function, not two.
- Methods are identified by receiver, e.g. `pkg.(*Processor).Process`.
- Standard-library and third-party calls are classified by import path and
  counted as external, instead of guessed from names.

(If a module can't be type-checked, witc falls back to an AST-only graph.)

### API surface with documentation
- Structs (fields + methods), interfaces, and functions with their signatures.
- The **first sentence of each doc comment** (package, type, function, method),
  which is usually worth more to an agent than the signature alone.
- Exported symbols are listed first; unexported helpers are collapsed at lower
  detail levels.

### Architecture overview
- Entry points (functions nothing in the module calls, plus `main`).
- A per-package line with its doc, type/function counts, and the in-module
  packages it depends on (a call-derived package dependency graph).
- External package dependencies per package.

### Metrics
- Total functions and internal/external call counts.
- Average fan-out, most-called function (max fan-in), highest fan-out.
- Longest call chain (cycle-guarded) and high-coupling functions.

### Output control for context windows
- `--detail low|medium|high` selects how much to emit.
- `--max-tokens N` caps the estimated size; the least important sections are
  dropped first and the API surface is truncated by call-graph centrality
  (types and exported functions kept first), with a note of what was omitted.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output`, `-o` | stdout | Write output to a file |
| `--format` | `markdown` | Output format: `markdown` or `json` |
| `--detail` | `high` | `low` (API only), `medium` (+call graph, metrics, architecture), `high` (everything) |
| `--max-tokens` | `0` | Cap estimated output size in tokens (0 = unlimited) |
| `--no-structure` | `false` | Omit the file tree |
| `--include-tests` | `false` | Include test files (`_test.go`, `*.test.*`, `*.spec.*`, `__tests__/`) |
| `--exclude-generated` | `false` | Skip Go files marked as generated |
| `--no-progress` | `false` | Disable the stderr progress bar/spinner (auto-off when stderr isn't a terminal) |
| `--verbose`, `-v` | `false` | Increase verbosity (repeatable). `-v` logs call-graph build phase timings and per-package counts to stderr; `-vv` also traces the `go/packages` driver (`go list`) invocations and timing |

### Debugging a slow build

The call-graph step loads the module with full type information. Dependencies are
read from compiled export data (not re-type-checked from source), so it stays fast
even on large dependency trees and slow filesystems. Use `-v` to see where time
goes — package load time and the size of the import graph:

```
witc: loading packages from /path/to/repo ...
witc: loaded 6 module package(s) (172 in import graph) in 83ms
witc:   walked .../internal/formatter: 4 file(s), +75 edge(s), +42 func node(s) in 0s
witc: built call graph: 92 func node(s), 142 edge(s), 332 external call(s) in 0s
```

For the most granular view, `-vv` additionally traces every underlying `go list`
invocation with its individual timing, so you can see which driver call is slow.

## Output sections

Markdown output (at `--detail high`) contains, in order:

1. **Structure** — file tree (unless `--no-structure`)
2. **Architecture** — entry points and the package dependency overview
3. **Packages** — API surface with docs and inline calls
4. **Call Graph** — function relationships, entry points, leaf functions
5. **Metrics** — counts, fan-in/out, coupling
6. **Call Flow Summary / Execution Flow** — natural-language traces
7. **Package Dependencies** — external packages used per package

The JSON format emits the same information under a documented, versioned schema
— see [`docs/json-schema.md`](docs/json-schema.md).

## Skill

This repository ships a skill for coding agents at
[`.opencode/skill/witc/SKILL.md`](.opencode/skill/witc/SKILL.md) (opencode). It
tells an agent to run `witc summarize` to orient itself in an unfamiliar
codebase, then prefer `witc find`/`where` for targeted lookups so it can locate
code without loading the whole summary into context.
