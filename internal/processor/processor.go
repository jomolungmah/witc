package processor

import "context"

// Processor parses source files and extracts API surface (structs, interfaces, methods).
type Processor interface {
	Supports(ext string) bool
	Process(ctx context.Context, path string, src []byte) (*Result, error)
}

// Result contains extracted API surface from a single file.
type Result struct {
	Package    string
	ImportPath string // e.g. "internal/server" for display
	Structs    []Struct
	Interfaces []Interface
	Functions  []Function
}
