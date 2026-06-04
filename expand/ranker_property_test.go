package expand

import (
	"os"
	"path/filepath"
	"testing"
	"testing/quick"
)

// TestRank_NeverDropsCandidates: input slice length equals output length.
func TestRank_NeverDropsCandidates(t *testing.T) {
	f := func(strategyByte byte, syms uint8) bool {
		if syms > 20 {
			syms = 20
		}
		c := []Candidate{
			{File: "a", Strategy: pickStrategy(strategyByte), SymbolMatches: int(syms)},
			{File: "b", Strategy: pickStrategy(strategyByte + 1), SymbolMatches: int(syms / 2)},
			{File: "c", Strategy: pickStrategy(strategyByte + 2), SymbolMatches: 1},
		}
		out := Rank(c, DefaultRankerWeights())
		return len(out) == len(c)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

// TestPack_NeverOvershootsBudget: UsedBytes always ≤ BudgetBytes.
func TestPack_NeverOvershootsBudget(t *testing.T) {
	ws := t.TempDir()
	// Pre-create 5 files of various sizes
	for i, size := range []int{100, 500, 2000, 5000, 50} {
		mustWriteSized(t, ws, filepathJoin("f", i), size)
	}
	f := func(budget uint16) bool {
		ranked := []Candidate{
			{File: "f0", Strategy: "def_match"},
			{File: "f1", Strategy: "caller"},
			{File: "f2", Strategy: "type_def"},
			{File: "f3", Strategy: "test"},
			{File: "f4", Strategy: "def_match"},
		}
		got := Pack(ranked, int(budget), ws)
		return got.UsedBytes <= int(budget)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

// TestPack_AttachedPlusDroppedEqualsInputForReadableFiles.
func TestPack_AttachedPlusDroppedEqualsInputForReadableFiles(t *testing.T) {
	ws := t.TempDir()
	for i := 0; i < 5; i++ {
		mustWriteSized(t, ws, filepathJoin("f", i), 100*(i+1))
	}
	ranked := []Candidate{
		{File: "f0", Strategy: "def_match"},
		{File: "f1", Strategy: "caller"},
		{File: "f2", Strategy: "type_def"},
		{File: "f3", Strategy: "test"},
		{File: "f4", Strategy: "def_match"},
	}
	got := Pack(ranked, 200, ws) // small budget so most drop
	if len(got.Attached)+len(got.Dropped) != 5 {
		t.Fatalf("want 5 total, got %d attached + %d dropped",
			len(got.Attached), len(got.Dropped))
	}
}

func pickStrategy(b byte) string {
	switch b % 4 {
	case 0:
		return "def_match"
	case 1:
		return "caller"
	case 2:
		return "type_def"
	default:
		return "test"
	}
}

// filepathJoin builds "f0" / "f1" relative names.
func filepathJoin(prefix string, i int) string {
	return prefix + string(rune('0'+i))
}

// Avoid unused-import warning when running tests on macOS.
var _ = os.PathSeparator
var _ = filepath.Separator
