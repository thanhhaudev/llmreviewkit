package index

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestLocation_JSONRoundtrip(t *testing.T) {
	loc := Location{
		Name: "Foo",
		File: "internal/auth/auth.go",
		Line: 42,
		Kind: SymDef,
		Pkg:  "auth",
	}
	b, err := json.Marshal(loc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Location
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(loc, got) {
		t.Fatalf("roundtrip: want %+v, got %+v", loc, got)
	}
}

func TestKind_StringRoundtrip(t *testing.T) {
	cases := []struct {
		k    Kind
		want string
	}{
		{SymDef, "def"},
		{SymCall, "call"},
		{SymImport, "import"},
		{SymTypeRef, "type_ref"},
	}
	for _, c := range cases {
		if c.k.String() != c.want {
			t.Errorf("Kind(%d).String() = %q, want %q", c.k, c.k.String(), c.want)
		}
	}
}

func TestIndex_CurrentSchemaVersion(t *testing.T) {
	if CurrentSchemaVersion < 1 {
		t.Fatalf("CurrentSchemaVersion must be ≥1, got %d", CurrentSchemaVersion)
	}
}

func TestIndex_LookupDefs_Basic(t *testing.T) {
	idx := &Index{
		Version: CurrentSchemaVersion,
		Files: map[string]*FileIndex{
			"a.go": {
				Path: "a.go", Lang: "go",
				Defs: []Location{
					{Name: "Foo", File: "a.go", Line: 10, Kind: SymDef},
					{Name: "Bar", File: "a.go", Line: 20, Kind: SymDef},
				},
			},
			"b.go": {
				Path: "b.go", Lang: "go",
				Defs: []Location{
					{Name: "Foo", File: "b.go", Line: 5, Kind: SymDef, Pkg: "subpkg"},
				},
			},
		},
	}
	idx.RebuildLookups()

	got := idx.LookupDefs("Foo", "")
	if len(got) != 2 {
		t.Fatalf("LookupDefs Foo (no pkg): want 2, got %d", len(got))
	}

	// Asymmetric pkg filter: caller pkg="subpkg" matches BOTH a.go (loc.Pkg
	// "" — unknown, loose-match wins) AND b.go (loc.Pkg "subpkg" — exact).
	// This mirrors v1 regex-grep behavior and avoids dropping intra-package
	// defs that the Go AST extractor records without a pkg label.
	gotPkg := idx.LookupDefs("Foo", "subpkg")
	if len(gotPkg) != 2 {
		t.Fatalf("LookupDefs Foo subpkg: want 2 (loose + exact), got %d", len(gotPkg))
	}

	// Caller pkg="otherpkg" still loose-matches a.go (Pkg="") but NOT b.go
	// (Pkg="subpkg" disagrees explicitly). Verifies the filter still bites
	// when both sides carry concrete pkg labels.
	gotOther := idx.LookupDefs("Foo", "otherpkg")
	if len(gotOther) != 1 || gotOther[0].File != "a.go" {
		t.Fatalf("LookupDefs Foo otherpkg: want 1 from a.go, got %+v", gotOther)
	}

	none := idx.LookupDefs("Missing", "")
	if len(none) != 0 {
		t.Fatalf("LookupDefs Missing: want 0, got %d", len(none))
	}
}

func TestIndex_LookupRefs_ReturnsAllNonDefOccurrences(t *testing.T) {
	idx := &Index{
		Version: CurrentSchemaVersion,
		Files: map[string]*FileIndex{
			"caller.go": {
				Path: "caller.go", Lang: "go",
				Refs: []Location{
					{Name: "Foo", File: "caller.go", Line: 30, Kind: SymCall},
				},
			},
		},
	}
	idx.RebuildLookups()

	got := idx.LookupRefs("Foo", "")
	if len(got) != 1 {
		t.Fatalf("LookupRefs Foo: want 1, got %d", len(got))
	}
}
