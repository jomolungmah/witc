package processor

// Struct represents a struct type.
type Struct struct {
	Name    string
	Fields  []Field
	Methods []Method
}

// Interface represents an interface type.
type Interface struct {
	Name    string
	Methods []Method
}

// Function represents a top-level function.
type Function struct {
	Name      string
	Signature string
}

// Method represents a method (has receiver).
type Method struct {
	Receiver  string
	Name      string
	Signature string
}

// Field represents a struct or interface field.
type Field struct {
	Name string
	Type string
}
