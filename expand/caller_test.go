package expand

import (
	"sort"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

func TestCallers_AttachesFilesReferencingDiffSyms(t *testing.T) {
	idx := buildIndex(map[string]struct {
		Defs []index.Location
		Refs []index.Location
	}{
		"main.go": {Refs: []index.Location{
			{Name: "Authenticate", File: "main.go", Line: 12, Kind: index.SymCall},
		}},
		"caller2.go": {Refs: []index.Location{
			{Name: "Authenticate", File: "caller2.go", Line: 34, Kind: index.SymCall},
		}},
		"auth.go": {Defs: []index.Location{
			{Name: "Authenticate", File: "auth.go", Line: 42, Kind: index.SymDef},
		}},
	})

	diffSyms := []symbols.Symbol{mkSym("Authenticate", "", symbols.SymDef)}
	diffFiles := map[string]bool{"auth.go": true}

	got := Callers(diffSyms, idx, diffFiles)
	sort.Slice(got, func(i, j int) bool { return got[i].File < got[j].File })

	if len(got) != 2 {
		t.Fatalf("want 2 caller candidates (main.go + caller2.go), got %d: %+v", len(got), got)
	}
	if got[0].File != "caller2.go" || got[1].File != "main.go" {
		t.Fatalf("unexpected files: %+v", got)
	}
	for _, c := range got {
		if c.Strategy != "caller" {
			t.Errorf("want Strategy=caller, got %q", c.Strategy)
		}
		if c.SymbolMatches < 1 {
			t.Errorf("want SymbolMatches >= 1, got %d", c.SymbolMatches)
		}
	}
}

func TestCallers_ExcludesDiffFiles(t *testing.T) {
	idx := buildIndex(map[string]struct {
		Defs []index.Location
		Refs []index.Location
	}{
		"auth.go": {Refs: []index.Location{
			{Name: "Authenticate", File: "auth.go", Line: 99, Kind: index.SymCall},
		}},
	})
	diffSyms := []symbols.Symbol{mkSym("Authenticate", "", symbols.SymDef)}
	diffFiles := map[string]bool{"auth.go": true}

	got := Callers(diffSyms, idx, diffFiles)
	if len(got) != 0 {
		t.Fatalf("expected 0 candidates when only refs are in diff files, got %+v", got)
	}
}

func TestCallers_MergesMultipleSymbolsPointingToSameFile(t *testing.T) {
	idx := buildIndex(map[string]struct {
		Defs []index.Location
		Refs []index.Location
	}{
		"main.go": {Refs: []index.Location{
			{Name: "Authenticate", File: "main.go", Line: 12, Kind: index.SymCall},
			{Name: "Logout", File: "main.go", Line: 20, Kind: index.SymCall},
		}},
	})
	diffSyms := []symbols.Symbol{
		mkSym("Authenticate", "", symbols.SymDef),
		mkSym("Logout", "", symbols.SymDef),
	}
	diffFiles := map[string]bool{}

	got := Callers(diffSyms, idx, diffFiles)
	if len(got) != 1 {
		t.Fatalf("want 1 merged candidate, got %d: %+v", len(got), got)
	}
	if got[0].SymbolMatches != 2 {
		t.Fatalf("want SymbolMatches=2, got %d", got[0].SymbolMatches)
	}
}
