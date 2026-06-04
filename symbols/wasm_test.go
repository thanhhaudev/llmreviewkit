//go:build !lite

package symbols

import "testing"

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
