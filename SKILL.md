---
name: witc
description: Summarize and query a codebase's structure, API surface, and call graph — Go, TypeScript/JavaScript, and React, including monorepos that mix them. Use when starting work in an unfamiliar repo, when asked where a symbol is defined, who calls a function, what a function calls, or how a project is organized, and before grepping many files for a definition — `witc find` answers in tens of tokens instead of whole files.
license: MIT
---

# witc

`witc` maps a codebase for you: file structure, the API surface (types,
interfaces, functions, with the first sentence of each doc comment), a
resolved call graph, package-level architecture, and metrics. It understands
Go and TypeScript/JavaScript (React `.tsx`/`.jsx` included) and handles
monorepos with both (e.g. `backend/` Go + `frontend/` React) in one run.

## Which command to use

| Situation | Run |
|-----------|-----|
| First contact with an unfamiliar repo | `witc summarize . --detail medium --max-tokens 4000` |
| "Where is X defined?" / about to grep for a definition | `witc find X` (or `witc where X` for just file:line) |
| "What breaks if I change X?" / "who uses X?" | `witc callers X` |
| "What does X depend on / do?" | `witc callees X` |
| Deep-dive into one package or directory | `witc package <dir>` |

Queries cost ~10–300 tokens; a full summary of a mid-size repo is tens of
thousands. Once oriented, never re-run `summarize` to find one thing.

## Targeted queries

Run via the shell from anywhere in the project (or pass the root as a second
argument, e.g. `witc find Scan ~/work/lms`). The first query builds a cached
index automatically and refreshes it when the source changes, so only the
first call is slow.

```bash
witc find Scan            # declaration + doc + location
witc where ComputeKey     # location only
witc callers ComputeKey   # in-module callers
witc callees runIndex     # in-module callees
witc package scanner      # one package's API surface
```

Output is one `file:line<TAB>declaration` per match, e.g.:

```
internal/scanner/scanner.go:70	func Scan(root string, opts Options) ([]File, error)
	Scan walks the directory tree and returns source files matching the extensions in opts.
```

```
index.ComputeKey called by:
  main.ensureIndex
  main.runIndex
```

Names match exactly, as `pkg.Name`, or as a case-insensitive substring; all
matches are listed when ambiguous. Methods match by bare name (`get` finds
`spaceApi.get`) or qualified (`ApiClient.getUser`). Add `--json` for
structured output.

**On a miss** ("no symbol matching", non-zero exit): try a shorter substring
or the bare method name before falling back to grep. `callers` on a symbol
that exists but is never called reports "no function matching … in the call
graph" — that usually means it's an entry point or only used dynamically,
which is itself the answer.

## Full summary

```bash
witc summarize .                          # everything (largest)
witc summarize . --detail medium          # API + call graph + architecture
witc summarize . --detail low             # exported API surface only
witc summarize . --max-tokens 4000        # hard token budget, keeps the most central symbols
witc summarize ./internal/server          # just one subtree
witc summarize . --format json -o out.json
witc summarize . --include-tests          # test files are excluded by default
```

Read it top-down: **Architecture** (entry points, package dependency lines)
tells you the shape; **Packages** holds the API surface with docs; **Call
Graph** shows who calls whom, entry points, and leaf functions.

## Notes

- Call-graph precision: Go is fully type-checked. TS/JS is type-checked too
  when `node` and the project's `node_modules/typescript` are available
  (calls through typed variables resolve); otherwise import resolution
  (relative paths, barrel re-exports, tsconfig `baseUrl`/`paths`) still
  connects cross-file calls, `new`, and JSX renders.
- Node names are `pkg.Func` / `pkg.Type.method`, where `pkg` is the package
  (Go) or directory base name (TS/JS).
- `witc index` pre-builds the query cache (stored in `.witc/`); useful before
  a batch of queries, otherwise unnecessary.
