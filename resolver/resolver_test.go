package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/symbols"
)

func TestFindReferences_FindsGoFunc(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "auth.go"), `package x
func Authenticate(id int) error { return nil }
`)
	syms := []symbols.Symbol{
		{Name: "Authenticate", Kind: symbols.SymCall, File: "main.go"},
	}
	stats, err := FindReferences(syms, ws, []string{"main.go"}, 5, 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	refs := stats.Refs
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference, got %d (%+v)", len(refs), refs)
	}
	if filepath.Base(refs[0].File) != "auth.go" {
		t.Fatalf("expected auth.go, got %s", refs[0].File)
	}
	if refs[0].Excerpt == "" {
		t.Fatalf("expected non-empty excerpt")
	}
}

func TestFindReferences_SkipsStdlibSymbol(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "x.go"), "package x\nfunc Base() {}\n")
	syms := []symbols.Symbol{
		// File must be set so the language-scoped stdlib filter knows
		// this is a Go symbol (and `path` is the Go stdlib package).
		{Pkg: "path", Name: "Base", Kind: symbols.SymCall, File: "main.go"},
	}
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, 5, 8192)
	refs := stats.Refs
	// path.Base is stdlib → should NOT be resolved (skip), even if a local
	// func Base exists in the workspace.
	if len(refs) != 0 {
		t.Fatalf("expected 0 references for stdlib symbol, got %d (%+v)", len(refs), refs)
	}
}

func TestFindReferences_CapPerSymbol(t *testing.T) {
	ws := t.TempDir()
	for i := 0; i < 10; i++ {
		mustWrite(t,
			filepath.Join(ws, "file"+itoa(i)+".go"),
			"package x\nfunc Common() {}\n",
		)
	}
	syms := []symbols.Symbol{{Name: "Common", Kind: symbols.SymCall}}
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, 5, 8192)
	refs := stats.Refs
	if len(refs) > 5 {
		t.Fatalf("expected ≤5 refs (cap), got %d", len(refs))
	}
}

func TestFindReferences_ExcerptCappedBytes(t *testing.T) {
	ws := t.TempDir()
	// 100 lines of definition + filler — far more than the per-file budget.
	var big string
	big += "package x\nfunc Big() {\n"
	for i := 0; i < 500; i++ {
		big += "    println(\"line\")\n"
	}
	big += "}\n"
	mustWrite(t, filepath.Join(ws, "big.go"), big)
	syms := []symbols.Symbol{{Name: "Big", Kind: symbols.SymCall}}
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, 5, 512)
	refs := stats.Refs
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if len(refs[0].Excerpt) > 600 { // some slack for trailing context
		t.Fatalf("expected excerpt ≤~600B (cap 512 + slack), got %d", len(refs[0].Excerpt))
	}
}

func TestFindReferences_SkipsForbiddenDirs(t *testing.T) {
	ws := t.TempDir()
	// Put a matching definition INSIDE node_modules → must not be found.
	must := func(rel, content string) {
		full := filepath.Join(ws, rel)
		mustMkdir(t, filepath.Dir(full))
		mustWrite(t, full, content)
	}
	must("node_modules/foo/bar.go", "package x\nfunc Hidden() {}\n")
	syms := []symbols.Symbol{{Name: "Hidden", Kind: symbols.SymCall}}
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, 5, 8192)
	refs := stats.Refs
	if len(refs) != 0 {
		t.Fatalf("node_modules must be skipped; got refs: %+v", refs)
	}
}

func TestFindReferences_EmptySymbolListReturnsEmpty(t *testing.T) {
	stats, err := FindReferences(nil, t.TempDir(), nil, 5, 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(stats.Refs) != 0 {
		t.Fatalf("expected empty refs, got %+v", stats.Refs)
	}
}

func TestFindReferences_StatsTracksExtractedAndFiltered(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "x.go"), "package x\nfunc Real() {}\n")
	syms := []symbols.Symbol{
		// 1 stdlib (filtered out)
		{Pkg: "path", Name: "Base", Kind: symbols.SymCall, File: "main.go"},
		// 1 empty name (filtered out)
		{Name: "", Kind: symbols.SymCall, File: "main.go"},
		// 1 real (passes filter)
		{Name: "Real", Kind: symbols.SymCall, File: "main.go"},
	}
	stats, err := FindReferences(syms, ws, []string{"main.go"}, 5, 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if stats.ExtractedCount != 3 {
		t.Fatalf("ExtractedCount: want 3, got %d", stats.ExtractedCount)
	}
	if stats.FilteredCount != 1 {
		t.Fatalf("FilteredCount: want 1 (after stdlib+empty filter), got %d", stats.FilteredCount)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindReferences_StatsResolvedCountSemantics(t *testing.T) {
	// 1 symbol matched in 3 files → ResolvedCount=1, len(Refs)=3.
	// This is the key invariant for measurement: ResolvedCount counts
	// *distinct symbols matched*, not file hits.
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "a.go"), "package x\nfunc Shared() {}\n")
	mustWrite(t, filepath.Join(ws, "b.go"), "package x\nfunc Shared() {}\n")
	mustWrite(t, filepath.Join(ws, "c.go"), "package x\nfunc Shared() {}\n")
	syms := []symbols.Symbol{{Name: "Shared", Kind: symbols.SymCall, File: "main.go"}}

	stats, err := FindReferences(syms, ws, []string{"main.go"}, 5, 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if stats.ResolvedCount != 1 {
		t.Fatalf("ResolvedCount: want 1 (one distinct sym), got %d", stats.ResolvedCount)
	}
	if len(stats.Refs) != 3 {
		t.Fatalf("len(Refs): want 3 (three file hits), got %d", len(stats.Refs))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var s []byte
	for n > 0 {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
	}
	return string(s)
}
