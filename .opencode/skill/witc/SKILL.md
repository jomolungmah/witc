---
name: witc
description: Summarize a Go codebase (structure, API surface with docs, type-checked call graph, architecture, metrics) to get oriented before reading or editing. Use when you have little or no context about a Go project, or need to find where something lives. Supports a token budget so the summary fits your context window.
license: MIT
---

# witc

`witc` is a CLI that produces an LLM-friendly summary of a Go codebase: the file
structure, the API surface (structs, interfaces, functions) **with the first
sentence of each doc comment**, a **type-checked call graph**, a package-level
architecture overview, and metrics.

## When to use

- You're starting work in an unfamiliar Go repo and need orientation fast.
- You need to find which package/function is responsible for something.
- You want a compact, structured map of a project to load into context.

Prefer running `witc` once up front over reading many files blindly.

## How to use

Run it via the shell. The only command is `summarize`:

```
witc summarize [path] [flags]
```

- `path` is optional and defaults to the current directory. It may be a
  subdirectory of the module to summarize just that subtree.

### Flags

| Flag | Values | Default | What it does |
|------|--------|---------|--------------|
| `--output`, `-o` | file path | stdout | Write the summary to a file instead of stdout |
| `--format` | `markdown`, `json` | `markdown` | Output format. `json` follows a versioned schema |
| `--detail` | `low`, `medium`, `high` | `high` | How much to emit: `low` = exported API surface only; `medium` = + call graph, metrics, architecture; `high` = everything (inline calls, prose, execution flow) |
| `--max-tokens` | integer | `0` | Cap estimated output size in tokens (`0` = unlimited). Least-important sections are dropped first and low-centrality symbols are truncated so it fits a context window |
| `--no-structure` | (bool) | off | Omit the file-tree section |
| `--include-tests` | (bool) | off | Include `_test.go` files (excluded by default) |
| `--exclude-generated` | (bool) | off | Skip Go files marked as generated |

### Common invocations

```bash
witc summarize .                          # full summary of the current project
witc summarize . --detail low             # smallest: exported API + docs only
witc summarize . --detail medium          # + call graph, metrics, architecture
witc summarize . --max-tokens 4000        # bound the output to ~4000 tokens
witc summarize ./internal/server          # summarize one subdirectory
witc summarize . --format json -o out.json  # structured output to a file
witc summarize . --include-tests          # also cover _test.go files
```

Tip: when you only need to know what a project is and where things live, start
with `--detail medium` (or `--detail low --max-tokens N` for a strict budget).

## Reading the output (markdown)

The sections, in order, are: **Architecture** (entry points + which packages
depend on which), **Packages** (the API surface with docs), **Call Graph**
(who calls whom, entry points, leaf functions), **Metrics**, and natural-language
**Call Flow / Execution Flow** traces.

Start with **Architecture** to see the shape of the project and entry points,
then drill into the relevant package in **Packages**, and use the **Call Graph**
to trace how a function connects to the rest of the code.

## Notes

- Go projects only.
- The call graph is type-resolved (built from the compiled packages), so names
  are fully qualified, e.g. `pkg.Func` or `pkg.(*Type).Method`.
