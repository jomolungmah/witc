# witc

A Go CLI tool for summarizing codebases for usage in LLM coding agents. Builds a short summary of a codebase's file structure as well as structures and methods.

## Usage

```bash
witc summarize [path]              # default: current directory
witc summarize ./myproject -o summary.md
witc summarize . --format json
witc summarize . --no-structure    # API only, no file tree
witc summarize . --exclude-generated
```

## Flags

- `--output`, `-o` - Write output to file (default: stdout)
- `--format` - Output format: markdown, json (default: markdown)
- `--no-structure` - Omit file structure, output API surface only
- `--exclude-generated` - Skip Go files marked as generated

## Installation

```bash
go install github.com/ai-suite/witc/cmd/witc@latest
```
