package expand

import (
	"testing"

	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

func TestTypeDefs_AttachesTypeDefinitionFiles(t *testing.T) {
	idx := buildIndex(map[string]struct {
		Defs []index.Location
		Refs []index.Location
	}{
		"models/user.go": {Defs: []index.Location{
			{Name: "User", File: "models/user.go", Line: 5, Kind: index.SymDef},
		}},
		"main.go": {Defs: []index.Location{
			{Name: "main", File: "main.go", Line: 1, Kind: index.SymDef},
		}},
	})
	diffSyms := []symbols.Symbol{
		{Name: "User", Pkg: "", Kind: symbols.SymTypeRef},
	}
	diffFiles := map[string]bool{"main.go": true}

	got := TypeDefs(diffSyms, idx, diffFiles)
	if len(got) != 1 {
		t.Fatalf("want 1 type_def candidate (models/user.go), got %d: %+v", len(got), got)
	}
	if got[0].File != "models/user.go" {
		t.Fatalf("want models/user.go, got %s", got[0].File)
	}
	if got[0].Strategy != "type_def" {
		t.Fatalf("want Strategy=type_def, got %q", got[0].Strategy)
	}
}

func TestTypeDefs_IgnoresNonTypeRefKinds(t *testing.T) {
	idx := buildIndex(map[string]struct {
		Defs []index.Location
		Refs []index.Location
	}{
		"models/user.go": {Defs: []index.Location{
			{Name: "User", File: "models/user.go", Line: 5, Kind: index.SymDef},
		}},
	})
	// SymCall, not SymTypeRef — should be ignored even though name matches.
	diffSyms := []symbols.Symbol{
		{Name: "User", Pkg: "", Kind: symbols.SymCall},
	}
	diffFiles := map[string]bool{}

	got := TypeDefs(diffSyms, idx, diffFiles)
	if len(got) != 0 {
		t.Fatalf("want 0 (SymCall is not a TypeRef), got %d: %+v", len(got), got)
	}
}

func TestTypeDefs_ExcludesDiffFiles(t *testing.T) {
	idx := buildIndex(map[string]struct {
		Defs []index.Location
		Refs []index.Location
	}{
		"user.go": {Defs: []index.Location{
			{Name: "User", File: "user.go", Line: 5, Kind: index.SymDef},
		}},
	})
	diffSyms := []symbols.Symbol{{Name: "User", Kind: symbols.SymTypeRef}}
	diffFiles := map[string]bool{"user.go": true}

	got := TypeDefs(diffSyms, idx, diffFiles)
	if len(got) != 0 {
		t.Fatalf("want 0 (def is in diff file), got %d", len(got))
	}
}
