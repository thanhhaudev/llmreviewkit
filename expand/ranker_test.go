package expand

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestRank_StrategyOrdering(t *testing.T) {
	c := []Candidate{
		{File: "a.go", Strategy: "test", SymbolMatches: 1},
		{File: "b.go", Strategy: "type_def", SymbolMatches: 1},
		{File: "c.go", Strategy: "caller", SymbolMatches: 1},
		{File: "d.go", Strategy: "def_match", SymbolMatches: 1},
	}
	got := Rank(c, DefaultRankerWeights())
	if got[0].File != "d.go" {
		t.Fatalf("def_match should rank first, got %s", got[0].File)
	}
	if got[1].File != "c.go" {
		t.Fatalf("caller should rank second, got %s", got[1].File)
	}
	if got[2].File != "b.go" {
		t.Fatalf("type_def should rank third, got %s", got[2].File)
	}
	if got[3].File != "a.go" {
		t.Fatalf("test should rank last, got %s", got[3].File)
	}
}

func TestRank_SymbolMatchesDominates(t *testing.T) {
	c := []Candidate{
		{File: "weak.go", Strategy: "def_match", SymbolMatches: 1},
		{File: "strong.go", Strategy: "test", SymbolMatches: 5},
	}
	got := Rank(c, DefaultRankerWeights())
	// 5 * 10 + 2 = 52 (test) beats 1 * 10 + 8 = 18 (def_match)
	if got[0].File != "strong.go" {
		t.Fatalf("strong (5 matches) should rank first over weak (1 match + def bonus), got %s", got[0].File)
	}
}

func TestRank_CustomWeights(t *testing.T) {
	c := []Candidate{
		{File: "a.go", Strategy: "test", SymbolMatches: 10},
		{File: "b.go", Strategy: "def_match", SymbolMatches: 1},
	}
	// Zero out SymbolMatch weight — only strategy bonus matters
	w := RankerWeights{
		SymbolMatch:  0,
		StrategyDef:  100,
		StrategyTest: 1,
	}
	got := Rank(c, w)
	if got[0].File != "b.go" {
		t.Fatalf("def_match with strong weight should win, got %s", got[0].File)
	}
}

func TestRank_StableSort(t *testing.T) {
	// Two candidates with identical scores — order should be deterministic.
	c := []Candidate{
		{File: "a.go", Strategy: "test", SymbolMatches: 1},
		{File: "b.go", Strategy: "test", SymbolMatches: 1},
	}
	got1 := Rank(append([]Candidate{}, c...), DefaultRankerWeights())
	got2 := Rank(append([]Candidate{}, c...), DefaultRankerWeights())
	if got1[0].File != got2[0].File || got1[1].File != got2[1].File {
		t.Fatal("Rank should be stable across calls with identical input")
	}
	// And alphabetical when scores tie (assert deterministic ordering)
	sort.SliceStable(c, func(i, j int) bool { return c[i].File < c[j].File })
	if got1[0].File != c[0].File {
		t.Fatalf("expected alphabetical tie-break (or at least stable input order), got %s vs %s", got1[0].File, c[0].File)
	}
}

func TestPack_FitsWithinBudget(t *testing.T) {
	ws := t.TempDir()
	mustWriteSized(t, ws, "a.go", 100)
	mustWriteSized(t, ws, "b.go", 200)
	mustWriteSized(t, ws, "c.go", 5000)

	ranked := []Candidate{
		{File: "a.go", Strategy: "def_match", SymbolMatches: 3},
		{File: "b.go", Strategy: "caller", SymbolMatches: 2},
		{File: "c.go", Strategy: "test", SymbolMatches: 1},
	}
	got := Pack(ranked, 500, ws)
	if got.UsedBytes > 500 {
		t.Fatalf("UsedBytes=%d exceeds budget 500", got.UsedBytes)
	}
	// Total attached + dropped must equal input
	if len(got.Attached)+len(got.Dropped) != 3 {
		t.Fatalf("attached + dropped = %d, want 3", len(got.Attached)+len(got.Dropped))
	}
}

func TestPack_PopulatesByStrategy(t *testing.T) {
	ws := t.TempDir()
	mustWriteSized(t, ws, "a.go", 50)
	mustWriteSized(t, ws, "b.go", 50)
	mustWriteSized(t, ws, "c.go", 50)

	ranked := []Candidate{
		{File: "a.go", Strategy: "def_match"},
		{File: "b.go", Strategy: "caller"},
		{File: "c.go", Strategy: "test"},
	}
	got := Pack(ranked, 10000, ws)
	if got.ByStrategy["def_match"] != 1 {
		t.Errorf("ByStrategy[def_match]=%d, want 1", got.ByStrategy["def_match"])
	}
	if got.ByStrategy["caller"] != 1 {
		t.Errorf("ByStrategy[caller]=%d, want 1", got.ByStrategy["caller"])
	}
	if got.ByStrategy["test"] != 1 {
		t.Errorf("ByStrategy[test]=%d, want 1", got.ByStrategy["test"])
	}
}

func TestPack_MissingFileSkipped(t *testing.T) {
	ws := t.TempDir()
	mustWriteSized(t, ws, "exists.go", 100)

	ranked := []Candidate{
		{File: "exists.go", Strategy: "def_match"},
		{File: "missing.go", Strategy: "caller"},
	}
	got := Pack(ranked, 10000, ws)
	if len(got.Attached) != 1 {
		t.Fatalf("want 1 attached (only exists.go), got %d", len(got.Attached))
	}
	if got.Attached[0].File != "exists.go" {
		t.Fatalf("want exists.go, got %s", got.Attached[0].File)
	}
}

func TestPack_EmptyInputReturnsEmptyResult(t *testing.T) {
	got := Pack(nil, 1000, t.TempDir())
	if len(got.Attached) != 0 || len(got.Dropped) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
	if got.BudgetBytes != 1000 {
		t.Fatalf("BudgetBytes=%d, want 1000", got.BudgetBytes)
	}
}

func mustWriteSized(t *testing.T, ws, rel string, size int) {
	t.Helper()
	full := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	for i := range data {
		data[i] = 'a'
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
