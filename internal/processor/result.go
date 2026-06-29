package processor

// Location is the source position of a symbol, relative to the summary root.
// A zero Line means the location is unknown (e.g. a struct known only through
// its methods, with no declaration in the scanned files).
type Location struct {
	File   string // slash-separated path relative to the summary root
	Line   int
	Column int
}

// Struct represents a struct type.
type Struct struct {
	Name    string
	Doc     string // first sentence of the doc comment, if any
	Loc     Location
	Fields  []Field
	Methods []Method
}

// Interface represents an interface type.
type Interface struct {
	Name    string
	Doc     string // first sentence of the doc comment, if any
	Loc     Location
	Methods []Method
}

// Function represents a top-level function.
type Function struct {
	Name      string
	Doc       string // first sentence of the doc comment, if any
	Loc       Location
	Signature string
}

// Method represents a method (has receiver).
type Method struct {
	Receiver  string
	Name      string
	Doc       string // first sentence of the doc comment, if any
	Loc       Location
	Signature string
}

// Field represents a struct or interface field.
type Field struct {
	Name string
	Type string
}

// CallInfo contains information about a function call.
type CallInfo struct {
	CallerName string // The name of the code that makes the call (e.g., "append")
	CalleeName string // The name/function being called
	File       string // Source file path where call occurs
	Line       int    // Line number in source
	Column     int    // Column number in source
	ParentFunc string // Name of function/method containing this call (e.g., "Process")
}
