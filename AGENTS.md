# AGENTS.md

Guide for agentic coding assistants working on this repository.

## Build/Test Commands

### Build
```bash
# Build the CLI tool
go build -o witc ./cmd/witc/main.go

# Install globally
go install github.com/ai-suite/witc/cmd/witc@latest
```

### Test
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with race detection
go test -race ./...

# Run a single test function
# Example: run TestProcessor_Process
go test -v -run TestProcessor_Process ./internal/processor/go/

# Run a single test file
go test -v ./internal/processor/go/goparser_test.go

# Run tests in a specific package
go test -v ./internal/formatter/
```

### Lint
```bash
# Run go vet
go vet ./...

# Run gofmt check (shows formatting issues)
gofmt -l .

# Run static analysis
go vet -all ./...

# Run all checks
go vet -all ./... && gofmt -l . | grep -v '^$'

# Run tests with race detection
go test -race ./...

# Run tests with coverage
go test -cover ./...

# Run tests with coverage profile
go test -coverprofile=coverage.out ./...

# Generate coverage report
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

# Security linting
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

# Use staticcheck for additional analysis
# go install honnef.co/go/tools/cmd/staticcheck@latest
# staticcheck ./...
```

## Code Style Guidelines

### Imports
- Use explicit import paths (full module path: `github.com/ai-suite/witc/...`)
- Group imports: standard library first, then third-party, then local
- Sort imports alphabetically within groups
- Use blank imports only for `//go:build` directives and `init()` functions
- Example:
  ```go
  import (
      "context"
      "fmt"
      "github.com/ai-suite/witc/internal/formatter"
      "github.com/spf13/cobra"
  )
  ```

### Formatting
- Use `go fmt` for formatting
- Line width: default Go formatter (79-88 chars)
- Two spaces for indentation (Go default)

### Types
- Prefer `struct` for grouping related data
- Use pointer receivers for methods that mutate state
- Use value receivers for methods that don't mutate
- Export types that are part of the public API
- Use lowercase for private types

### Naming Conventions
- Package names: lowercase, no underscores
- Exported types/functions/vars: capitalize first letter
- Private types/functions/vars: lowercase
- Test files: suffix with `_test.go`
- Test functions: prefix with `Test` (e.g., `TestProcessor_Process`)
- Constants: ALL_CAPS with underscores

### Error Handling
- Use `%w` for wrapping errors: `return fmt.Errorf("op failed: %w", err)`
- Use `%v` for errors you want to display as-is
- Return errors from functions that can fail
- Propagate errors up the call chain
- Don't silently ignore errors

### Context
- Use `context.Context` for cancellation and timeouts
- Pass `context.Background()` at entry points
- Create derived contexts: `ctx, cancel := context.WithTimeout(parent, 10*time.Second)`

### Comments
- Block comments for package-level documentation
- Single-line comments for inline explanations
- Document public types and functions

### Project Structure
```
./cmd/witc/main.go          # CLI entry point
./internal/processor/        # Core processing logic
./internal/processor/go/     # Go-specific processor
./internal/scanner/          # File discovery
./internal/formatter/        # Output formatters
./testdata/                  # Test data (excluded from scans)
```

### Testing Best Practices
- Test external interfaces, not implementations
- Use `t.TempDir()` for temporary test directories
- Use `t.Fatalf()` for fatal errors in tests
- Use `t.Errorf()` for non-fatal assertion failures
- Tests should be deterministic and isolated
- Include test data in `testdata/` directories

### Linting & Security
- Run `go vet` before each commit
- Use `gofmt -l .` to check formatting consistency
- Regular static analysis with `go vet -all ./...`
- Security scanning: `govulncheck ./...` for vulnerability checks
- Consider adding `staticcheck` for deeper analysis
- Fix all lint issues before merging PRs
- Coverage should be above 80% for critical paths

### Code Quality
- Keep functions under 50 lines when possible
- Extract complex logic into smaller helper functions
- Use table-driven tests for parameterized scenarios
- Add examples to documented functions
- Use `gomodifyimports` to keep imports tidy
- Prefer `interface{}` over `any` for backward compatibility
- Use named return values only when they improve readability
- Avoid godoc-style comments; use Go doc comments instead

### Additional Notes
- This repository is a Go CLI tool called `witc`
- Test data in `testdata/` should not be scanned
- All Go code must pass `go vet` before commit
- The CLI entry point is at `cmd/witc/main.go`
- Module path: `github.com/ai-suite/witc`
- PRs should include test coverage updates
- Documentation in block comments for public APIs
- Use `context.Context` for all I/O operations
- Error wrapping is mandatory in all error paths