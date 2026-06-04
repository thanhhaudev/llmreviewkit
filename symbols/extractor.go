package symbols

// SymbolKind classifies a symbol reference for the resolver.
type SymbolKind int

const (
	SymCall    SymbolKind = iota + 1 // pkg.Func() or obj.method()
	SymTypeRef                       // Foo in `var x Foo` or `: Foo`
	SymImport                        // import "x" / from x import y
	SymDef                           // func/class/type definition
)

func (k SymbolKind) String() string {
	switch k {
	case SymCall:
		return "call"
	case SymTypeRef:
		return "typeref"
	case SymImport:
		return "import"
	case SymDef:
		return "def"
	default:
		return "unknown"
	}
}

// Symbol is a single identifier reference extracted from source code.
type Symbol struct {
	Name string // identifier
	Pkg  string // pkg prefix if pkg.Func form, else ""
	Kind SymbolKind
	File string // source file (debug + dedup)
	Line int
}

// Extractor extracts symbol references from source content.
// Implementations are language-specific (Go AST, WASM grammar, regex).
type Extractor interface {
	Extract(path string, content []byte) []Symbol
}
