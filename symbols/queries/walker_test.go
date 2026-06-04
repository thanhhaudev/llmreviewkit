//go:build !lite

package queries

import (
	"context"
	"os"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/symbols"
	"github.com/thanhhaudev/llmreviewkit/symbols/treesitter"
)

func TestCaptureToSymbol_Mapping(t *testing.T) {
	src := []byte("<?php\nfunction login() {}\n")
	cases := []struct {
		captureName string
		startByte   uint32
		endByte     uint32
		wantKind    symbols.SymbolKind
		wantName    string
	}{
		{"name.definition.function", 15, 20, symbols.SymDef, "login"},
		{"name.reference.call", 15, 20, symbols.SymCall, "login"},
		{"name.reference.import", 15, 20, symbols.SymImport, "login"},
		{"name.reference.type", 15, 20, symbols.SymTypeRef, "login"},
	}
	for _, c := range cases {
		cap := treesitter.Capture{
			Name: c.captureName, StartByte: c.startByte, EndByte: c.endByte,
		}
		sym, ok := CaptureToSymbol(cap, src, "auth.php", 1)
		if !ok {
			t.Errorf("CaptureToSymbol(%q) returned ok=false", c.captureName)
			continue
		}
		if sym.Kind != c.wantKind || sym.Name != c.wantName {
			t.Errorf("CaptureToSymbol(%q): got Kind=%v Name=%q, want Kind=%v Name=%q",
				c.captureName, sym.Kind, sym.Name, c.wantKind, c.wantName)
		}
	}
}

func TestCaptureToSymbol_UnrecognizedName_ReturnsFalse(t *testing.T) {
	cap := treesitter.Capture{Name: "name.something.else", StartByte: 0, EndByte: 4}
	if _, ok := CaptureToSymbol(cap, []byte("test"), "x", 1); ok {
		t.Fatal("expected ok=false for unrecognized capture name")
	}
}

func TestCaptureToSymbol_OutOfBoundsByteRange_ReturnsFalse(t *testing.T) {
	src := []byte("hi")
	cap := treesitter.Capture{Name: "name.definition.function", StartByte: 0, EndByte: 100}
	if _, ok := CaptureToSymbol(cap, src, "x", 1); ok {
		t.Fatal("expected ok=false for out-of-bounds end")
	}
	cap2 := treesitter.Capture{Name: "name.definition.function", StartByte: 5, EndByte: 3}
	if _, ok := CaptureToSymbol(cap2, src, "x", 1); ok {
		t.Fatal("expected ok=false for start >= end")
	}
}

func TestScanCaptures_ComputesLines(t *testing.T) {
	src := []byte("line1\nline2\nfunc foo() {}\n")
	caps := []treesitter.Capture{
		// "foo" starts after "line1\nline2\nfunc " = 17 bytes, ends at 20
		{Name: "name.definition.function", StartByte: 17, EndByte: 20},
	}
	syms := ScanCaptures(caps, src, "x.go")
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %+v", len(syms), syms)
	}
	if syms[0].Name != "foo" {
		t.Errorf("expected name=foo, got %q", syms[0].Name)
	}
	if syms[0].Line != 3 {
		t.Errorf("expected line=3, got %d", syms[0].Line)
	}
	if syms[0].File != "x.go" {
		t.Errorf("expected file=x.go, got %q", syms[0].File)
	}
}

func TestScanCaptures_DropsUnknown(t *testing.T) {
	caps := []treesitter.Capture{
		{Name: "name.definition.function", StartByte: 0, EndByte: 3},
		{Name: "unknown.capture", StartByte: 0, EndByte: 3},
	}
	syms := ScanCaptures(caps, []byte("foo"), "x")
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol (unknown dropped), got %d", len(syms))
	}
}

func loadPhpGrammar(t *testing.T) []byte {
	t.Helper()
	path := "../../../test-fixtures/tree-sitter-php.wasm"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("php grammar fixture not present: %v", err)
	}
	return data
}

// TestPhpQuery_ExtractsKnownSymbols verifies the PHP grammar can extract
// the expected definitions using the cursor-walk pipeline. As of v0.12.2,
// production no longer calls ts_query_new for PHP because the 0.24.2
// grammar reliably traps with OOB in our wasm runtime — see
// extractPHPViaWalk in internal/symbols/wasm.go for the rationale.
//
// This test uses an isolated NewRuntime so it does not share the singleton
// with the TS / Python tests; a failure here must not block the other
// language tests.
func TestPhpQuery_ExtractsKnownSymbols(t *testing.T) {
	ctx := context.Background()
	r, err := treesitter.NewRuntime(ctx)
	if err != nil {
		t.Skipf("runtime unavailable: %v", err)
	}
	defer r.Close(ctx)
	lang, err := r.LoadGrammar(ctx, "php", loadPhpGrammar(t))
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	fnDefID := lang.SymbolIDForName(ctx, "function_definition", true)
	methodDeclID := lang.SymbolIDForName(ctx, "method_declaration", true)
	classDeclID := lang.SymbolIDForName(ctx, "class_declaration", true)
	nameFieldID := lang.FieldIDForName(ctx, "name")
	if fnDefID == 0 || classDeclID == 0 || methodDeclID == 0 || nameFieldID == 0 {
		t.Fatalf("missing symbol/field ids fn=%d method=%d class=%d nameField=%d",
			fnDefID, methodDeclID, classDeclID, nameFieldID)
	}

	src := []byte(`<?php
function login() {}
class AuthService {
    public function authenticate() {}
}
`)
	tree, err := lang.Parse(ctx, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Close(ctx)

	defs, err := lang.WalkNamedChildren(ctx, tree, []uint16{fnDefID, methodDeclID, classDeclID}, nameFieldID)
	if err != nil {
		t.Fatalf("WalkNamedChildren: %v", err)
	}
	defSet := map[string]bool{}
	for _, d := range defs {
		if d.NameEnd > d.NameStart && int(d.NameEnd) <= len(src) {
			defSet[string(src[d.NameStart:d.NameEnd])] = true
		}
	}
	for _, want := range []string{"login", "AuthService", "authenticate"} {
		if !defSet[want] {
			t.Errorf("missing def %q in %v", want, defs)
		}
	}
	_ = symbols.SymDef // keep the symbols import used.
}

// TestPythonQuery_ExtractsKnownSymbols verifies that the Python grammar
// pipeline — LoadGrammar → Parse → WalkNamedChildren — extracts the 4
// known definitions from a small Python fixture.
//
// Python uses cursor-based tree walking (WalkNamedChildren) instead of
// NewQuery+Exec because tree-sitter-python@0.23.x with web-tree-sitter
// 0.26.9 causes ts_query_new to corrupt the runtime's dlmalloc when the
// grammar is small (430 KB). This is the same pipeline used by the
// production wasm.go extractPythonViaWalk path.
//
// Note: NewQuery BEFORE Parse is still the contract for grammars that use
// it (PHP, TypeScript). Python bypasses that contract entirely.
func TestPythonQuery_ExtractsKnownSymbols(t *testing.T) {
	ctx := context.Background()
	r, err := treesitter.NewRuntime(ctx)
	if err != nil {
		t.Skipf("runtime: %v", err)
	}
	defer r.Close(ctx)
	path := "../../../test-fixtures/tree-sitter-python.wasm"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("python grammar fixture not present: %v", err)
	}
	lang, err := r.LoadGrammar(ctx, "python", data)
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	// Look up symbol IDs and the "name" field ID — same as extractPythonViaWalk.
	fnDefID := lang.SymbolIDForName(ctx, "function_definition", true)
	classDefID := lang.SymbolIDForName(ctx, "class_definition", true)
	nameFieldID := lang.FieldIDForName(ctx, "name")

	src := []byte(`
def login(): pass
async def refresh(): pass
class AuthService:
    def authenticate(self): pass
`)

	// Parse the source (no NewQuery before Parse since we use WalkNamedChildren).
	tree, err := lang.Parse(ctx, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Close(ctx)

	// Walk the tree to extract named definitions.
	defs, err := lang.WalkNamedChildren(ctx, tree, []uint16{fnDefID, classDefID}, nameFieldID)
	if err != nil {
		t.Fatalf("WalkNamedChildren: %v", err)
	}

	defSet := map[string]bool{}
	for _, d := range defs {
		if d.NameEnd > d.NameStart && int(d.NameEnd) <= len(src) {
			defSet[string(src[d.NameStart:d.NameEnd])] = true
		}
	}
	for _, w := range []string{"login", "refresh", "AuthService", "authenticate"} {
		if !defSet[w] {
			t.Errorf("missing def %q in %v", w, defs)
		}
	}
}

func TestTSQuery_ExtractsKnownSymbols(t *testing.T) {
	ctx := context.Background()
	r, err := treesitter.NewRuntime(ctx)
	if err != nil {
		t.Skipf("runtime unavailable: %v", err)
	}
	defer r.Close(ctx)
	path := "../../../test-fixtures/tree-sitter-typescript.wasm"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("ts grammar fixture not present: %v (run test-fixtures/fetch.sh)", err)
	}
	lang, err := r.LoadGrammar(ctx, "typescript", data)
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	// IMPORTANT: NewQuery before Parse.
	q, err := lang.NewQuery(ctx, TypescriptTags)
	if err != nil {
		t.Fatalf("NewQuery: %v", err)
	}
	defer q.Close(ctx)

	src := []byte(`
export function classic() {}
export class Service {}
interface I {}
`)
	tree, err := lang.Parse(ctx, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Close(ctx)

	caps, err := q.Exec(ctx, tree.RootNode(ctx))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	syms := ScanCaptures(caps, src, "x.ts")

	defSet := map[string]bool{}
	for _, s := range syms {
		if s.Kind == symbols.SymDef {
			defSet[s.Name] = true
		}
	}
	for _, w := range []string{"classic", "Service", "I"} {
		if !defSet[w] {
			t.Errorf("missing def %q in %v", w, syms)
		}
	}
}
