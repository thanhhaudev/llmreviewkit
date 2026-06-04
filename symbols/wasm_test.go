//go:build !lite

package symbols

import (
	"context"
	"os"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/symbols/treesitter"
)

func TestUseWASM_ReturnsTrueForKnownGrammar(t *testing.T) {
	// Even without compiled grammars present, useWASM should report which
	// extensions WOULD use WASM. (The actual WASM call falls back to regex
	// when grammar file is missing.)
	cases := []string{".ts", ".tsx", ".py", ".rs", ".java"}
	for _, ext := range cases {
		if !useWASM(ext) {
			t.Fatalf("expected useWASM(%q)=true (default build, grammar bundled or stubbed), got false", ext)
		}
	}
}

func TestUseWASM_ReturnsFalseForUnknownExt(t *testing.T) {
	if useWASM(".unknown") {
		t.Fatalf("expected useWASM(unknown)=false")
	}
}

func TestWASMExtractor_FallsBackWhenGrammarMissing(t *testing.T) {
	// Grammar files not yet committed (Task 14 compiles them). The extractor
	// must gracefully fall back to regex behavior — never panic, never block.
	e := newWASMExtractor(".ts")
	syms := e.Extract("auth.ts", []byte(`export function authenticate() {}`))
	// Should return SOME symbols (via regex fallback) even with no grammar.
	if len(syms) == 0 {
		t.Fatalf("expected fallback regex to extract symbols when grammar missing")
	}
}

func TestSplitDottedPath(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantPkg  string
	}{
		{"app", "app", ""},
		{"app.route", "route", "app"},
		{"a.b.c", "c", "a.b"},
		{"", "", ""},
		{".leading", "leading", ""},
		{"trailing.", "", "trailing"},
	}
	for _, c := range cases {
		gotName, gotPkg := splitDottedPath(c.in)
		if gotName != c.wantName || gotPkg != c.wantPkg {
			t.Errorf("splitDottedPath(%q) = (%q, %q); want (%q, %q)",
				c.in, gotName, gotPkg, c.wantName, c.wantPkg)
		}
	}
}

func TestPythonDecoratorReceiver(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// @app.route("/login")  → receiver "app.route"
		{`@app.route("/login")`, "app.route"},
		// @router.get("/users") → receiver "router.get"
		{`@router.get("/users")`, "router.get"},
		// @a.b.c(arg)           → receiver "a.b.c"
		{`@a.b.c(arg)`, "a.b.c"},
		// Bare decorator (no call) → no receiver to extract
		{`@staticmethod`, ""},
		{`@property`, ""},
		// Decorator that is a plain call (not attribute) → empty (the bare
		// identifier "deco" is already captured by the existing callID branch)
		{`@deco()`, ""},
		// Edge: whitespace inside parens shouldn't matter
		{`@app.route( "/x" )`, "app.route"},
		// Edge: multi-line decorator
		{"@app.route(\n  \"/x\",\n)", "app.route"},
		// Empty input
		{"", ""},
	}
	for _, c := range cases {
		got := pythonDecoratorReceiver(c.in)
		if got != c.want {
			t.Errorf("pythonDecoratorReceiver(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestExtractPythonViaWalk_EmitsSymTypeRef(t *testing.T) {
	ctx := context.Background()
	r, err := treesitter.NewRuntime(ctx)
	if err != nil {
		t.Skipf("runtime: %v", err)
	}
	defer r.Close(ctx)
	wasmPath := "../test-fixtures/tree-sitter-python.wasm"
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Skipf("python grammar fixture not present: %v", err)
	}
	lang, err := r.LoadGrammar(ctx, "python", data)
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	src := []byte(`def f(x: Foo, y: Bar) -> Baz:
    z: Bar = None
    return z
`)
	syms := extractPythonViaWalk(ctx, lang, src, "f.py")

	names := map[string]int{}
	for _, s := range syms {
		if s.Kind == SymTypeRef {
			names[s.Name]++
		}
	}
	for _, name := range []string{"Foo", "Bar", "Baz"} {
		if names[name] == 0 {
			t.Errorf("expected SymTypeRef for %s, got names=%v", name, names)
		}
	}
}

func TestExtractPHPViaWalk_EmitsSymTypeRef(t *testing.T) {
	ctx := context.Background()
	r, err := treesitter.NewRuntime(ctx)
	if err != nil {
		t.Skipf("runtime: %v", err)
	}
	defer r.Close(ctx)
	wasmPath := "../test-fixtures/tree-sitter-php.wasm"
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Skipf("php grammar fixture not present: %v", err)
	}
	lang, err := r.LoadGrammar(ctx, "php", data)
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	src := []byte(`<?php
function f(Foo $x, ?Bar $y): Baz {
    $z = new Bar();
    return new Baz();
}
`)
	syms := extractPHPViaWalk(ctx, lang, src, "f.php")

	names := map[string]int{}
	for _, s := range syms {
		if s.Kind == SymTypeRef {
			names[s.Name]++
		}
	}
	for _, name := range []string{"Foo", "Bar", "Baz"} {
		if names[name] == 0 {
			t.Errorf("expected SymTypeRef for %s, got names=%v", name, names)
		}
	}
}

func TestExtractTypeScriptQuery_EmitsSymTypeRef(t *testing.T) {
	ctx := context.Background()
	r, err := treesitter.NewRuntime(ctx)
	if err != nil {
		t.Skipf("runtime: %v", err)
	}
	defer r.Close(ctx)
	wasmPath := "../test-fixtures/tree-sitter-typescript.wasm"
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Skipf("typescript grammar fixture not present: %v", err)
	}
	lang, err := r.LoadGrammar(ctx, "typescript", data)
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	src := []byte(`function f(x: Foo, y: Bar): Baz {
  const z: Bar = null;
  return new Baz();
}
`)

	// Run the TS query path directly against this test's own runtime+grammar
	// instead of going through wasmExtractor (which caches grammars in a
	// package-level langCache that gets polluted by prior python/php loads
	// in the same process — same dlmalloc trap CLAUDE.md warned about).
	q, err := lang.NewQuery(ctx, typescriptTags)
	if err != nil {
		t.Fatalf("NewQuery: %v", err)
	}
	defer q.Close(ctx)
	tree, err := lang.Parse(ctx, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Close(ctx)
	caps, err := q.Exec(ctx, tree.RootNode(ctx))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	syms := scanCaptures(caps, src, "f.ts")

	names := map[string]int{}
	for _, s := range syms {
		if s.Kind == SymTypeRef {
			names[s.Name]++
		}
	}
	for _, name := range []string{"Foo", "Bar", "Baz"} {
		if names[name] == 0 {
			t.Errorf("expected SymTypeRef for %s, got names=%v", name, names)
		}
	}
}
