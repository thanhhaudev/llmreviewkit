package resolver

import (
	"os"
	"path/filepath"
	"strings"
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
	stats, err := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 8192)
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
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 8192)
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
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 8192)
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
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 512)
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
	stats, _ := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 8192)
	refs := stats.Refs
	if len(refs) != 0 {
		t.Fatalf("node_modules must be skipped; got refs: %+v", refs)
	}
}

func TestFindReferences_EmptySymbolListReturnsEmpty(t *testing.T) {
	stats, err := FindReferences(nil, t.TempDir(), nil, nil, 5, 8192)
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
	stats, err := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 8192)
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

	stats, err := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 8192)
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

func TestFindReferences_ScopePaths_IgnoresDefsOutsideScope(t *testing.T) {
	ws := t.TempDir()
	mustMkdir(t, filepath.Join(ws, "app"))
	mustWrite(t, filepath.Join(ws, "app", "auth.go"), "package x\nfunc Authenticate(){}\n")
	// Use "lib" (not "vendor") so the unscoped walk doesn't auto-skip it.
	mustMkdir(t, filepath.Join(ws, "lib"))
	mustWrite(t, filepath.Join(ws, "lib", "auth.go"), "package lib\nfunc Authenticate(){}\n")

	syms := []symbols.Symbol{{Name: "Authenticate", Kind: symbols.SymCall, File: "main.go"}}

	statsScoped, err := FindReferences(syms, ws, []string{"main.go"}, []string{"app"}, 5, 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(statsScoped.Refs) != 1 {
		t.Fatalf("scoped: want 1 ref (app/auth.go only), got %d: %+v", len(statsScoped.Refs), statsScoped.Refs)
	}
	if filepath.Base(statsScoped.Refs[0].File) != "auth.go" || !strings.Contains(statsScoped.Refs[0].File, "app") {
		t.Fatalf("scoped: expected app/auth.go, got %s", statsScoped.Refs[0].File)
	}

	statsFull, err := FindReferences(syms, ws, []string{"main.go"}, nil, 5, 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(statsFull.Refs) != 2 {
		t.Fatalf("unscoped: want 2 refs (both auth.go), got %d", len(statsFull.Refs))
	}
}

func TestCollectTiers_NilScope_WalksFullWorkspace(t *testing.T) {
	ws := t.TempDir()
	mustMkdir(t, filepath.Join(ws, "app"))
	mustMkdir(t, filepath.Join(ws, "other"))
	mustWrite(t, filepath.Join(ws, "app", "x.go"), "package x\n")
	mustWrite(t, filepath.Join(ws, "other", "y.go"), "package y\n")

	tier0Dirs := map[string]bool{"app": true}
	t0, t1, t2, err := collectTiers(ws, tier0Dirs, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(t0) != 1 {
		t.Fatalf("tier0: want 1 (app/x.go), got %d: %v", len(t0), t0)
	}
	// other/y.go is a sibling subtree of app → tier1 (not tier2) per
	// resolver tier logic. The point is: when scope is nil, it MUST be
	// visited and classified somewhere.
	if len(t1)+len(t2) != 1 {
		t.Fatalf("tier1+tier2: want 1 (other/y.go visible when scope is nil), got t1=%v t2=%v", t1, t2)
	}
}

func TestCollectTiers_ScopePaths_LimitsWalkToScopeSubtrees(t *testing.T) {
	ws := t.TempDir()
	mustMkdir(t, filepath.Join(ws, "app", "Http"))
	mustMkdir(t, filepath.Join(ws, "vendor", "lib"))
	mustMkdir(t, filepath.Join(ws, "other"))
	mustWrite(t, filepath.Join(ws, "app", "Http", "Controller.go"), "package x\n")
	mustWrite(t, filepath.Join(ws, "vendor", "lib", "big.go"), "package vendor\n")
	mustWrite(t, filepath.Join(ws, "other", "z.go"), "package other\n")

	tier0Dirs := map[string]bool{"app/Http": true}
	t0, t1, t2, err := collectTiers(ws, tier0Dirs, []string{"app"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, p := range append(append(append([]string{}, t0...), t1...), t2...) {
		rel, _ := filepath.Rel(ws, p)
		if !strings.HasPrefix(rel, "app") {
			t.Fatalf("scope leak: %s is outside scope=[app]", rel)
		}
	}
	if len(t0) != 1 {
		t.Fatalf("tier0: want 1 (app/Http/Controller.go), got %d: %v", len(t0), t0)
	}
}

func TestCollectTiers_ScopePaths_OverlappingPathsDedupedAndNoDoubleVisit(t *testing.T) {
	ws := t.TempDir()
	mustMkdir(t, filepath.Join(ws, "app", "Http"))
	mustWrite(t, filepath.Join(ws, "app", "Http", "x.go"), "package x\n")

	tier0Dirs := map[string]bool{"app/Http": true}
	t0, _, _, err := collectTiers(ws, tier0Dirs, []string{"app", "app/Http"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(t0) != 1 {
		t.Fatalf("tier0: want 1 (deduped), got %d: %v", len(t0), t0)
	}
}

func TestCollectTiers_EmptyStringScopeEntriesFiltered(t *testing.T) {
	ws := t.TempDir()
	mustMkdir(t, filepath.Join(ws, "app"))
	mustMkdir(t, filepath.Join(ws, "other"))
	mustWrite(t, filepath.Join(ws, "app", "x.go"), "package x\n")
	mustWrite(t, filepath.Join(ws, "other", "y.go"), "package y\n")

	tier0Dirs := map[string]bool{"app": true}

	// Pure empty-string scope → equivalent to nil scope (walks full ws).
	t0, t1, t2, err := collectTiers(ws, tier0Dirs, []string{""})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	total := len(t0) + len(t1) + len(t2)
	if total != 2 {
		t.Fatalf("empty-string scope: want full walk (2 files), got %d (t0=%v t1=%v t2=%v)", total, t0, t1, t2)
	}

	// Mixed scope: ["", "app"] → "" filtered, behavior matches scope=["app"].
	t0b, t1b, t2b, err := collectTiers(ws, tier0Dirs, []string{"", "app"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	totalB := len(t0b) + len(t1b) + len(t2b)
	if totalB != 1 {
		t.Fatalf("mixed empty+app scope: want scope=[app] (1 file), got %d (t0=%v t1=%v t2=%v)", totalB, t0b, t1b, t2b)
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
