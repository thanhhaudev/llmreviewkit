package diff

import "testing"

func TestAttachReferenced_FitsWithinBudget(t *testing.T) {
	b := Bundle{}
	refs := []refInput{
		{path: "a.go", excerpt: "func A() {}\n", syms: []string{"A"}},
		{path: "b.go", excerpt: "func B() {}\n", syms: []string{"B"}},
	}
	res := AttachReferenced(&b, toRefs(refs), 1024)
	if len(b.ReferencedFiles) != 2 {
		t.Fatalf("expected 2 attached, got %d", len(b.ReferencedFiles))
	}
	if res.Attached != 2 {
		t.Fatalf("res.Attached: want 2, got %d", res.Attached)
	}
}

func TestAttachReferenced_DropsLowPriorityOverBudget(t *testing.T) {
	b := Bundle{}
	big := make([]byte, 500)
	for i := range big {
		big[i] = 'x'
	}
	refs := []refInput{
		{path: "hot.go", excerpt: "func Hot(){}\n", syms: []string{"A", "B", "C"}}, // 3 matches, small
		{path: "big.go", excerpt: string(big), syms: []string{"D"}},                // 1 match, large
		{path: "med.go", excerpt: string(big[:200]), syms: []string{"E", "F"}},     // 2 matches, medium
	}
	res := AttachReferenced(&b, toRefs(refs), 600)
	// hot.go (3 syms, small) MUST be kept.
	// med.go (2 syms) MUST be kept if fits.
	// big.go likely dropped.
	got := pathsOf(b.ReferencedFiles)
	if !contains(got, "hot.go") {
		t.Fatalf("hot.go must be kept (highest priority); got %v", got)
	}
	if contains(got, "big.go") && !contains(got, "med.go") {
		t.Fatalf("med.go (more symbols) should beat big.go; got %v", got)
	}
	if res.Attached+res.Dropped != 3 {
		t.Fatalf("Attached+Dropped: want 3 total, got %d+%d", res.Attached, res.Dropped)
	}
}

func TestAttachReferenced_AppendsWarningWhenDropped(t *testing.T) {
	b := Bundle{}
	refs := []refInput{
		{path: "a.go", excerpt: "AAAAAAAAAA", syms: []string{"A"}}, // 10 bytes excerpt
		{path: "b.go", excerpt: "BBBBBBBBBB", syms: []string{"B"}}, // 10 bytes excerpt
	}
	// cost per file = len(excerpt) + 80 overhead = 90; budget 100 fits exactly 1
	res := AttachReferenced(&b, toRefs(refs), 100)
	if len(b.ReferencedFiles) != 1 {
		t.Fatalf("expected 1 kept, got %d", len(b.ReferencedFiles))
	}
	if res.Attached != 1 || res.Dropped != 1 {
		t.Fatalf("Attached/Dropped: want 1/1, got %d/%d", res.Attached, res.Dropped)
	}
	if len(b.Warnings) == 0 {
		t.Fatalf("expected warning when files dropped, got none")
	}
}

func TestAttachReferenced_DeterministicPriority(t *testing.T) {
	// Same input → same output regardless of original order.
	refs1 := []refInput{
		{path: "x.go", excerpt: "X", syms: []string{"X"}},
		{path: "y.go", excerpt: "Y", syms: []string{"Y", "Z"}},
	}
	refs2 := []refInput{
		{path: "y.go", excerpt: "Y", syms: []string{"Y", "Z"}},
		{path: "x.go", excerpt: "X", syms: []string{"X"}},
	}
	b1 := Bundle{}
	_ = AttachReferenced(&b1, toRefs(refs1), 1024)
	b2 := Bundle{}
	_ = AttachReferenced(&b2, toRefs(refs2), 1024)
	if pathsOf(b1.ReferencedFiles)[0] != pathsOf(b2.ReferencedFiles)[0] {
		t.Fatalf("priority sort not deterministic: %v vs %v",
			pathsOf(b1.ReferencedFiles), pathsOf(b2.ReferencedFiles))
	}
}

func TestAttachReferenced_MutatesInPlace(t *testing.T) {
	b := Bundle{}
	refs := []refInput{{path: "x.go", excerpt: "package x", syms: []string{"X"}}}
	res := AttachReferenced(&b, toRefs(refs), 1024)

	if len(b.ReferencedFiles) != 1 {
		t.Fatalf("expected 1 referenced file, got %d", len(b.ReferencedFiles))
	}
	if res.Attached != 1 {
		t.Fatalf("res.Attached: want 1, got %d", res.Attached)
	}
}

// Test helpers

type refInput struct {
	path    string
	excerpt string
	syms    []string
}

func toRefs(in []refInput) []ReferenceInput {
	out := make([]ReferenceInput, len(in))
	for i, r := range in {
		out[i] = ReferenceInput{Path: r.path, Excerpt: r.excerpt, Symbols: r.syms}
	}
	return out
}

func pathsOf(rs []ReferencedFile) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Path
	}
	return out
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

func TestAttachReferenced_LogEntryFilesPopulated(t *testing.T) {
	b := Bundle{}
	refs := []refInput{
		{path: "app/db.go", excerpt: "func FetchRow(){}\n", syms: []string{"FetchRow", "FetchAll"}},
		{path: "app/log.go", excerpt: "func Warn(){}\n", syms: []string{"Warn"}},
	}
	res := AttachReferenced(&b, toRefs(refs), 1024)

	if res.Attached != 2 {
		t.Fatalf("Attached: want 2, got %d", res.Attached)
	}
	if res.Dropped != 0 {
		t.Fatalf("Dropped: want 0, got %d", res.Dropped)
	}
	if res.BudgetBytes != 1024 {
		t.Fatalf("BudgetBytes: want 1024, got %d", res.BudgetBytes)
	}
	if res.UsedBytes <= 0 {
		t.Fatalf("UsedBytes: want >0, got %d", res.UsedBytes)
	}
	if len(res.Files) != 2 {
		t.Fatalf("Files: want 2, got %d", len(res.Files))
	}
	// Reason must be "def_match:<comma-joined-sorted-syms>" — deterministic.
	wantReason := "def_match:FetchAll,FetchRow" // sorted alphabetically
	gotReason := ""
	for _, f := range res.Files {
		if f.Path == "app/db.go" {
			gotReason = f.Reason
			break
		}
	}
	if gotReason != wantReason {
		t.Fatalf("Reason for app/db.go: want %q, got %q", wantReason, gotReason)
	}
}
