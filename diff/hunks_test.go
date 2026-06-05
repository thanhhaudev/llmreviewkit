package diff

import "testing"

func TestHunksByFile_Standard(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -10,3 +10,5 @@
 ctx
+new1
+new2
 ctx
@@ -50,1 +52,2 @@
-old
+new3
+new4
diff --git a/bar.go b/bar.go
--- a/bar.go
+++ b/bar.go
@@ -1 +1,3 @@
-old
+new
+more
`
	got := HunksByFile(diff)
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(got), got)
	}
	if len(got["foo.go"]) != 2 {
		t.Errorf("foo.go: expected 2 hunks, got %d", len(got["foo.go"]))
	}
	if got["foo.go"][0].FileLineStart != 10 || got["foo.go"][0].FileLineCount != 5 {
		t.Errorf("foo.go hunk 0 = %+v, want {10, 5}", got["foo.go"][0])
	}
	if got["foo.go"][1].FileLineStart != 52 || got["foo.go"][1].FileLineCount != 2 {
		t.Errorf("foo.go hunk 1 = %+v, want {52, 2}", got["foo.go"][1])
	}
	if len(got["bar.go"]) != 1 || got["bar.go"][0].FileLineStart != 1 || got["bar.go"][0].FileLineCount != 3 {
		t.Errorf("bar.go = %+v, want [{1, 3}]", got["bar.go"])
	}
}

func TestHunksByFile_NoCountDefaultsToOne(t *testing.T) {
	// Per unified diff spec, "@@ -A +C @@" (no count) means count=1.
	diff := `+++ b/foo.go
@@ -10 +12 @@
-old
+new
`
	got := HunksByFile(diff)
	if got["foo.go"][0].FileLineCount != 1 {
		t.Errorf("expected count=1, got %d", got["foo.go"][0].FileLineCount)
	}
	if got["foo.go"][0].FileLineStart != 12 {
		t.Errorf("expected start=12, got %d", got["foo.go"][0].FileLineStart)
	}
}

func TestHunksByFile_LegacyNoBSlash(t *testing.T) {
	diff := `+++ foo.go
@@ -1 +1,2 @@
+new
`
	got := HunksByFile(diff)
	if len(got["foo.go"]) != 1 {
		t.Errorf("expected legacy '+++ foo.go' form parsed, got %v", got)
	}
}

func TestFindingInHunk_Overlap(t *testing.T) {
	hunks := map[string][]Hunk{
		"foo.go": {{FileLineStart: 10, FileLineCount: 5}}, // covers 10-14
	}
	cases := []struct {
		ls, le int
		want   bool
		name   string
	}{
		{10, 10, true, "start of hunk"},
		{14, 14, true, "end of hunk"},
		{12, 12, true, "inside hunk"},
		{5, 12, true, "range starts before, ends inside"},
		{12, 20, true, "range starts inside, ends after"},
		{5, 20, true, "range spans whole hunk"},
		{9, 9, false, "just before hunk"},
		{15, 15, false, "just after hunk"},
		{1, 5, false, "range entirely before"},
		{20, 30, false, "range entirely after"},
	}
	for _, c := range cases {
		got := FindingInHunk(hunks, "foo.go", c.ls, c.le)
		if got != c.want {
			t.Errorf("%s: FindingInHunk(foo.go, %d, %d) = %v, want %v", c.name, c.ls, c.le, got, c.want)
		}
	}
}

func TestFindingInHunk_FileNotInDiff(t *testing.T) {
	hunks := map[string][]Hunk{
		"foo.go": {{FileLineStart: 1, FileLineCount: 1}},
	}
	if FindingInHunk(hunks, "bar.go", 1, 10) {
		t.Error("expected false for file not in diff")
	}
}
