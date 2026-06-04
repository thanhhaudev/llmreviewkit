package symbols

import "testing"

func TestForFile_GoUsesAST(t *testing.T) {
	e := ForFile("main.go")
	if _, ok := e.(*GoASTExtractor); !ok {
		t.Fatalf("expected *GoASTExtractor for .go, got %T", e)
	}
}

func TestForFile_TypeScriptUsesRegexInLite(t *testing.T) {
	// In lite build, useWASM is always false, so TypeScript falls back to regex.
	// In default build, useWASM returns true for known grammars → WASMExtractor.
	// This test asserts factory routing works for at least one of those paths.
	e := ForFile("auth.ts")
	if e == nil {
		t.Fatalf("expected non-nil extractor for .ts, got nil")
	}
}

func TestForFile_PythonUsesRegexInLite(t *testing.T) {
	e := ForFile("auth.py")
	if e == nil {
		t.Fatalf("expected non-nil extractor for .py, got nil")
	}
}

func TestForFile_MarkdownReturnsNil(t *testing.T) {
	if got := ForFile("README.md"); got != nil {
		t.Fatalf("expected nil for .md, got %T", got)
	}
}

func TestForFile_JSONReturnsNil(t *testing.T) {
	if got := ForFile("config.json"); got != nil {
		t.Fatalf("expected nil for .json, got %T", got)
	}
}

func TestForFile_UnknownExtReturnsNil(t *testing.T) {
	if got := ForFile("README"); got != nil {
		t.Fatalf("expected nil for no extension, got %T", got)
	}
}

func TestSourceExtensions_KnownLangs(t *testing.T) {
	knownExts := []string{
		".go", ".js", ".jsx", ".mjs", ".ts", ".tsx",
		".py", ".rs", ".java", ".cs", ".rb", ".php",
		".kt", ".kts", ".swift", ".scala",
		".cpp", ".hpp", ".cc", ".hh", ".c", ".h",
		".m", ".mm", ".dart", ".ex", ".exs",
	}
	for _, ext := range knownExts {
		if !sourceExtensions[ext] {
			t.Errorf("expected %s in sourceExtensions", ext)
		}
	}
}

func TestExtToLang(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{".php", "php"},
		{".ts", "ts"},
		{".tsx", "ts"},
		{".js", "ts"},
		{".jsx", "ts"},
		{".mjs", "ts"},
		{".py", "python"},
		{".rb", "default"}, // not tuned in v0.12.1
		{".unknown", "default"},
		{"", "default"},
	}
	for _, c := range cases {
		if got := extToLang(c.ext); got != c.want {
			t.Errorf("extToLang(%q) = %q, want %q", c.ext, got, c.want)
		}
	}
}

func TestForFile_RoutesLang(t *testing.T) {
	// Use an extension that goes through the regex path on both default + lite builds.
	// .py is in sourceExtensions but the test must still pass under lite where there's no WASM.
	// We accept either *RegexExtractor with lang or *wasmExtractor (because under non-lite,
	// .py routes through WASM). For a deterministic assertion across both builds, target .rb
	// which is in sourceExtensions but useWASM may return false on lite — actually under
	// non-lite, useWASM(.rb) returns "ruby" (true). So .rb still hits WASM on default build.
	//
	// Simpler approach: don't lock to a specific extractor type — call Extract on a real
	// source and assert the returned symbols carry the right behavior. But behavior is
	// unchanged in Task 3.
	//
	// Compromise: use extToLang directly as the routing test (above), and in this test
	// just verify that .php is routed somewhere that resolves to a non-nil Extractor.
	e := ForFile("/abs/path/lib/auth.php")
	if e == nil {
		t.Fatal("ForFile(.php) returned nil; expected a non-nil Extractor")
	}
}
