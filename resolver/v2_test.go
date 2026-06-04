package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

func TestFindReferencesV2_HitsDefInIndex(t *testing.T) {
	ws := t.TempDir()
	// Write the file that holds the def, so V2 can read excerpt
	defContent := "package x\nfunc Authenticate(u string) error { return nil }\n"
	if err := os.WriteFile(filepath.Join(ws, "auth.go"), []byte(defContent), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	idx := &index.Index{
		Version: index.CurrentSchemaVersion,
		Root:    ws,
		Files: map[string]*index.FileIndex{
			"auth.go": {
				Path: "auth.go", Lang: "go",
				Defs: []index.Location{
					{Name: "Authenticate", File: "auth.go", Line: 2, Kind: index.SymDef},
				},
			},
		},
	}
	idx.RebuildLookups()

	syms := []symbols.Symbol{
		{Name: "Authenticate", Kind: symbols.SymCall, File: "main.go"},
	}
	stats, err := FindReferencesV2(syms, ws, idx, []string{"main.go"}, 5)
	if err != nil {
		t.Fatalf("FindReferencesV2: %v", err)
	}
	if stats.ResolverPath != "v2" {
		t.Fatalf("ResolverPath: want v2, got %s", stats.ResolverPath)
	}
	if stats.IndexHits != 1 {
		t.Fatalf("IndexHits: want 1, got %d", stats.IndexHits)
	}
	if stats.IndexMisses != 0 {
		t.Fatalf("IndexMisses: want 0, got %d", stats.IndexMisses)
	}
	if stats.ResolvedCount != 1 {
		t.Fatalf("ResolvedCount: want 1, got %d", stats.ResolvedCount)
	}
	if len(stats.Refs) != 1 {
		t.Fatalf("len(Refs): want 1, got %d", len(stats.Refs))
	}
	if stats.Refs[0].File != "auth.go" {
		t.Fatalf("Refs[0].File: want auth.go, got %s", stats.Refs[0].File)
	}
}

func TestFindReferencesV2_MissesAreTracked(t *testing.T) {
	ws := t.TempDir()
	idx := &index.Index{
		Version: index.CurrentSchemaVersion,
		Root:    ws,
		Files:   map[string]*index.FileIndex{},
	}
	idx.RebuildLookups()

	syms := []symbols.Symbol{
		{Name: "NotInIndex", Kind: symbols.SymCall, File: "main.go"},
	}
	stats, _ := FindReferencesV2(syms, ws, idx, nil, 5)
	if stats.IndexHits != 0 {
		t.Fatalf("IndexHits: want 0, got %d", stats.IndexHits)
	}
	if stats.IndexMisses != 1 {
		t.Fatalf("IndexMisses: want 1, got %d", stats.IndexMisses)
	}
	if stats.ResolvedCount != 0 {
		t.Fatalf("ResolvedCount: want 0, got %d", stats.ResolvedCount)
	}
}

func TestFindReferencesV2_StdlibFiltered(t *testing.T) {
	ws := t.TempDir()
	idx := &index.Index{Version: index.CurrentSchemaVersion, Files: map[string]*index.FileIndex{}}
	idx.RebuildLookups()

	syms := []symbols.Symbol{
		{Pkg: "path", Name: "Base", Kind: symbols.SymCall, File: "main.go"}, // stdlib
		{Name: "Real", Kind: symbols.SymCall, File: "main.go"},
	}
	stats, _ := FindReferencesV2(syms, ws, idx, nil, 5)
	if stats.ExtractedCount != 2 {
		t.Fatalf("ExtractedCount: want 2, got %d", stats.ExtractedCount)
	}
	if stats.FilteredCount != 1 {
		t.Fatalf("FilteredCount: want 1 (stdlib dropped), got %d", stats.FilteredCount)
	}
}

func TestResolveStatsV2_ToV1Compat(t *testing.T) {
	v2 := ResolveStatsV2{
		Refs:           []Reference{{File: "a.go"}},
		ExtractedCount: 5, FilteredCount: 3, ResolvedCount: 2,
		IndexHits: 2, IndexMisses: 1, ResolverPath: "v2",
	}
	v1 := v2.ToV1()
	if v1.ExtractedCount != 5 || v1.FilteredCount != 3 || v1.ResolvedCount != 2 {
		t.Fatalf("ToV1 counts mismatch: %+v", v1)
	}
	if len(v1.Refs) != 1 {
		t.Fatalf("ToV1 Refs lost: %d", len(v1.Refs))
	}
}
