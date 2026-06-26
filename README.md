# witc

A Go CLI that summarizes a codebase for LLM coding agents. It builds a
**type-checked call graph**, extracts the API surface **with doc comments**,
derives package-level architecture, computes metrics, and renders it all as
markdown or a versioned JSON schema — with **token budgeting** so the output
fits a context window.

Go only, for now.

## Usage

```bash
witc summarize [path]                  # default: current directory
witc summarize ./myproject -o summary.md
witc summarize . --format json         # versioned JSON (see docs/json-schema.md)
witc summarize . --detail low          # exported API surface only
witc summarize . --max-tokens 4000     # cap output to ~4000 tokens
witc summarize . --no-structure        # omit the file tree
witc summarize . --include-tests       # include _test.go files
```

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
| `--include-tests` | `false` | Include `_test.go` files |
| `--exclude-generated` | `false` | Skip Go files marked as generated |

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

## Installation

```bash
go install github.com/jomolungmah/witc/cmd/witc@latest
```

## Skill

This repository ships a skill for coding agents at
[`.opencode/skill/witc/SKILL.md`](.opencode/skill/witc/SKILL.md) (opencode). It
tells an agent to run `witc summarize` to orient itself in an unfamiliar Go
codebase before reading or editing.
