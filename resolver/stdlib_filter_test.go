package resolver

import (
	"testing"

	"github.com/thanhhaudev/llmreviewkit/symbols"
)

func TestIsStdlibSymbol_Go(t *testing.T) {
	cases := []struct {
		sym  symbols.Symbol
		want bool
	}{
		{symbols.Symbol{Pkg: "path", Name: "Base", File: "main.go"}, true},
		{symbols.Symbol{Pkg: "fmt", Name: "Println", File: "main.go"}, true},
		{symbols.Symbol{Pkg: "os", Name: "Open", File: "main.go"}, true},
		{symbols.Symbol{Pkg: "myapp", Name: "Foo", File: "main.go"}, false},
		{symbols.Symbol{Name: "Bar", File: "main.go"}, false}, // no pkg → not stdlib
	}
	for _, c := range cases {
		if got := IsStdlibSymbol(c.sym); got != c.want {
			t.Fatalf("sym=%+v: got %v want %v", c.sym, got, c.want)
		}
	}
}

func TestIsStdlibSymbol_Python(t *testing.T) {
	cases := []struct {
		sym  symbols.Symbol
		want bool
	}{
		{symbols.Symbol{Name: "os", Kind: symbols.SymImport, File: "main.py"}, true},
		{symbols.Symbol{Name: "sys", Kind: symbols.SymImport, File: "main.py"}, true},
		{symbols.Symbol{Name: "json", Kind: symbols.SymImport, File: "main.py"}, true},
		{symbols.Symbol{Name: "mypackage", Kind: symbols.SymImport, File: "main.py"}, false},
	}
	for _, c := range cases {
		if got := IsStdlibSymbol(c.sym); got != c.want {
			t.Fatalf("sym=%+v: got %v want %v", c.sym, got, c.want)
		}
	}
}

func TestIsStdlibSymbol_LanguageScoped(t *testing.T) {
	// A Go project package literally named `util` must NOT be filtered
	// just because Node has a util module. v0.12 bug fix.
	goUtil := symbols.Symbol{Pkg: "util", Name: "UnixMillis", Kind: symbols.SymCall, File: "internal/auth/auth.go"}
	if IsStdlibSymbol(goUtil) {
		t.Fatalf("Go package util must NOT be filtered as TypeScript util module")
	}
	// Conversely, a TypeScript util.foo() reference SHOULD be filtered.
	tsUtil := symbols.Symbol{Pkg: "util", Name: "format", Kind: symbols.SymCall, File: "src/index.ts"}
	if !IsStdlibSymbol(tsUtil) {
		t.Fatalf("TypeScript util module SHOULD be filtered as stdlib")
	}
}

func TestIsStdlibSymbol_UnknownExtensionFailsOpen(t *testing.T) {
	// Unknown source language → return false (let resolver search).
	sym := symbols.Symbol{Pkg: "os", Name: "Open", File: "build.gradle"}
	if IsStdlibSymbol(sym) {
		t.Fatalf("unknown ext should fail-open (return false)")
	}
}

func TestIsStdlibSymbol_Python_v0_12_3_Adds(t *testing.T) {
	// New stdlib pkg entries shipped in v0.12.3.
	stdlibAdds := []string{
		"argparse", "tempfile", "shutil", "pickle", "hashlib",
		"base64", "random", "math", "decimal", "weakref", "copy",
	}
	// New third-party pkg entries (frequently emitted by AST extraction).
	thirdPartyAdds := []string{
		"flask", "django", "requests", "numpy", "pandas",
		"sqlalchemy", "pydantic", "fastapi", "starlette", "redis", "celery",
	}
	for _, name := range append(stdlibAdds, thirdPartyAdds...) {
		sym := symbols.Symbol{Name: name, Kind: symbols.SymImport, File: "main.py"}
		if !IsStdlibSymbol(sym) {
			t.Errorf("expected Python pkg %q to be filtered as stdlib", name)
		}
		// Also test Pkg-keyed: e.g. a call to sqlalchemy.Session
		sym2 := symbols.Symbol{Name: "anything", Pkg: name, Kind: symbols.SymCall, File: "main.py"}
		if !IsStdlibSymbol(sym2) {
			t.Errorf("expected Python Pkg=%q to be filtered as stdlib", name)
		}
	}
}

func TestIsStdlibSymbol_PHP(t *testing.T) {
	cases := []struct {
		sym  symbols.Symbol
		want bool
	}{
		{symbols.Symbol{Name: "Symfony", Kind: symbols.SymImport, File: "src/Order.php"}, true},
		{symbols.Symbol{Name: "Laravel", Kind: symbols.SymImport, File: "src/Order.php"}, true},
		{symbols.Symbol{Pkg: "Doctrine", Name: "EntityManager", Kind: symbols.SymCall, File: "src/Order.php"}, true},
		{symbols.Symbol{Pkg: "GuzzleHttp", Name: "Client", Kind: symbols.SymCall, File: "src/Order.php"}, true},
		{symbols.Symbol{Pkg: "App", Name: "OrderService", Kind: symbols.SymCall, File: "src/Order.php"}, false},
		{symbols.Symbol{Name: "OrderService", File: "src/Order.php"}, false}, // no pkg, no import → false
	}
	for _, c := range cases {
		if got := IsStdlibSymbol(c.sym); got != c.want {
			t.Errorf("sym=%+v: got %v want %v", c.sym, got, c.want)
		}
	}
}

func TestIsStdlibSymbol_Python_BareNameBuiltins(t *testing.T) {
	builtins := []string{
		"print", "len", "range", "str", "int", "isinstance",
		"enumerate", "zip", "super", "type", "any", "all",
	}
	for _, name := range builtins {
		// SymCall with empty Pkg → filtered
		sym := symbols.Symbol{Name: name, Kind: symbols.SymCall, File: "main.py"}
		if !IsStdlibSymbol(sym) {
			t.Errorf("expected bare-name builtin %q (SymCall) to be filtered", name)
		}
		// SymDef MUST NOT be filtered (user could shadow `print`)
		symDef := symbols.Symbol{Name: name, Kind: symbols.SymDef, File: "main.py"}
		if IsStdlibSymbol(symDef) {
			t.Errorf("SymDef %q must NOT be filtered (user may shadow builtin)", name)
		}
		// SymCall with non-empty Pkg → NOT filtered (user code, distinguishable)
		symPkg := symbols.Symbol{Name: name, Pkg: "myobj", Kind: symbols.SymCall, File: "main.py"}
		if IsStdlibSymbol(symPkg) {
			t.Errorf("attribute call %s.%q must NOT be filtered as builtin", "myobj", name)
		}
	}
}

func TestIsStdlibSymbol_PHP_BareNameBuiltins(t *testing.T) {
	builtins := []string{
		"count", "strlen", "array_map", "is_array", "isset",
		"json_encode", "sprintf", "explode", "trim",
	}
	for _, name := range builtins {
		sym := symbols.Symbol{Name: name, Kind: symbols.SymCall, File: "src/main.php"}
		if !IsStdlibSymbol(sym) {
			t.Errorf("expected PHP bare-name builtin %q (SymCall) to be filtered", name)
		}
		symDef := symbols.Symbol{Name: name, Kind: symbols.SymDef, File: "src/main.php"}
		if IsStdlibSymbol(symDef) {
			t.Errorf("PHP SymDef %q must NOT be filtered", name)
		}
		symPkg := symbols.Symbol{Name: name, Pkg: "Whatever", Kind: symbols.SymCall, File: "src/main.php"}
		if IsStdlibSymbol(symPkg) {
			t.Errorf("PHP attribute call Whatever::%q must NOT be filtered as builtin", name)
		}
	}
}
