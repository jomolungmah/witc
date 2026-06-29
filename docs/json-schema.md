# witc JSON output schema

`witc summarize --format json` emits a structured summary that follows the
versioned schema described here. The shape is deliberately decoupled from witc's
internal types so they can be refactored without breaking consumers; any
breaking change to this shape bumps `schemaVersion`.

Current version: **`1.1`**

> **`1.1`** adds an optional `location` (`{ file, line }`) to each struct,
> interface, method, and function, so consumers can jump straight to a symbol's
> declaration. The change is additive; `1.0` consumers ignore the new field.

## Top level

| Field | Type | Notes |
|-------|------|-------|
| `schemaVersion` | string | Schema version, e.g. `"1.1"`. Check this before parsing. |
| `root` | string | Absolute path of the analyzed directory. |
| `packages` | array of [Package](#package) | Sorted by `importPath`. |
| `callGraph` | [CallGraph](#callgraph) \| absent | Omitted when no call graph is available. |
| `metrics` | [Metrics](#metrics) \| absent | Omitted when no call graph is available. |
| `architecture` | [Architecture](#architecture) \| absent | Omitted when no call graph is available. |

Display paths used as package identifiers (`importPath`, and the keys/values of
the architecture maps) are module-relative directories, e.g. `internal/scanner`.

## Package

| Field | Type | Notes |
|-------|------|-------|
| `importPath` | string | Module-relative package directory. |
| `doc` | string (optional) | First sentence of the package doc comment. |
| `structs` | array of [Struct](#struct) (optional) | |
| `interfaces` | array of [Interface](#interface) (optional) | |
| `functions` | array of [Function](#function) (optional) | Package-level functions. |

Test files (`_test.go`) are excluded unless `--include-tests` is passed.

### Struct
| Field | Type | Notes |
|-------|------|-------|
| `name` | string | |
| `doc` | string (optional) | First sentence of the doc comment. |
| `location` | [Location](#location) (optional) | Declaration site. Omitted for a struct known only through its methods. |
| `fields` | array of `{ name?, type }` (optional) | `name` omitted for embedded fields. |
| `methods` | array of [Method](#method) (optional) | |

### Interface
| Field | Type | Notes |
|-------|------|-------|
| `name` | string | |
| `doc` | string (optional) | |
| `location` | [Location](#location) (optional) | Declaration site. |
| `methods` | array of [Method](#method) (optional) | |

### Method
| Field | Type | Notes |
|-------|------|-------|
| `receiver` | string (optional) | e.g. `*Processor`. Empty for interface methods. |
| `name` | string | |
| `doc` | string (optional) | |
| `location` | [Location](#location) (optional) | Declaration site. |
| `signature` | string | e.g. `func(ctx context.Context) error`. |

### Function
| Field | Type | Notes |
|-------|------|-------|
| `name` | string | |
| `doc` | string (optional) | |
| `location` | [Location](#location) (optional) | Declaration site. |
| `signature` | string | |

### Location
| Field | Type | Notes |
|-------|------|-------|
| `file` | string | Slash-separated path relative to the analyzed root. |
| `line` | int | 1-based line of the symbol's name. |

## CallGraph

Built with full type information; nodes are fully-qualified
(`pkg.Func`, `pkg.(*T).Method`).

| Field | Type | Notes |
|-------|------|-------|
| `functions` | array of [GraphFunc](#graphfunc) | Sorted by `name`. |
| `externalCalls` | int | Total resolved calls into stdlib / third-party packages. |

### GraphFunc
| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Fully-qualified function/method name. |
| `package` | string (optional) | Full import path of the declaring package. |
| `callers` | array of string (optional) | Distinct in-module callers, sorted. |
| `callees` | array of string (optional) | Distinct in-module callees, sorted. |

## Metrics

| Field | Type |
|-------|------|
| `totalFunctions` | int |
| `totalCalls` | int |
| `internalCalls` | int |
| `externalCalls` | int |
| `avgCalleesPerFunction` | number |
| `maxFanInFunction` | string (optional) |
| `maxFanInCount` | int |
| `maxFanOutFunction` | string (optional) |
| `maxFanOutCount` | int |
| `maxCallDepth` | int |
| `highCouplingFunctions` | array of string (optional) |

## Architecture

| Field | Type | Notes |
|-------|------|-------|
| `entryPoints` | array of string (optional) | Functions with no in-module callers (plus `main`). |
| `packageDependencies` | object (optional) | display path → sorted display paths it calls into. |
| `externalDependencies` | object (optional) | display path → sorted external import paths it calls into. |

## Example (abridged)

```json
{
  "schemaVersion": "1.1",
  "root": "/path/to/project",
  "packages": [
    {
      "importPath": "internal/scanner",
      "doc": "Package scanner discovers source files.",
      "functions": [
        { "name": "Scan", "doc": "Scan walks the directory tree…", "location": { "file": "internal/scanner/scanner.go", "line": 28 }, "signature": "func(root string, includeTests bool) ([]File, error)" }
      ]
    }
  ],
  "callGraph": { "functions": [ { "name": "scanner.Scan", "package": "example.com/m/internal/scanner", "callers": ["main.runSummarize"] } ], "externalCalls": 294 },
  "metrics": { "totalFunctions": 82, "totalCalls": 419, "internalCalls": 125, "externalCalls": 294, "maxCallDepth": 7 },
  "architecture": {
    "entryPoints": ["main.main", "main.runSummarize"],
    "packageDependencies": { "cmd/witc": ["internal/formatter", "internal/scanner"] }
  }
}
```
