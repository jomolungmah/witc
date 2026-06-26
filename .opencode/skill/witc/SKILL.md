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

Run it via the shell from the directory you want to summarize (or pass a path):

```bash
witc summarize .
```

Useful options:

- `witc summarize . --detail low` — exported API surface only (smallest).
- `witc summarize . --detail medium` — adds call graph, metrics, and architecture.
- `witc summarize . --max-tokens 4000` — cap the output to ~4000 tokens; the
  least important sections are dropped and low-centrality symbols are truncated
  so it fits your context window.
- `witc summarize . --format json` — structured output (versioned schema) for
  programmatic use.
- `witc summarize ./somepkg` — summarize a subdirectory.
- `witc summarize . -o summary.md` — write to a file instead of stdout.

`_test.go` files are excluded by default; add `--include-tests` to include them.

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
