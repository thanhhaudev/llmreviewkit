package diff

import (
	"reflect"
	"testing"
)

func TestPaths_EmptyBundle(t *testing.T) {
	got := Paths(Bundle{})
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestPaths_SingleFile(t *testing.T) {
	b := Bundle{Diff: `diff --git a/internal/auth/auth.go b/internal/auth/auth.go
index abc..def 100644
--- a/internal/auth/auth.go
+++ b/internal/auth/auth.go
@@ -1,1 +1,1 @@
-old
+new
`}
	got := Paths(b)
	want := []string{"internal/auth/auth.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPaths_MultipleFilesDeduped(t *testing.T) {
	b := Bundle{Diff: `diff --git a/cmd/main.go b/cmd/main.go
+++ b/cmd/main.go
diff --git a/internal/api/auth.go b/internal/api/auth.go
+++ b/internal/api/auth.go
diff --git a/internal/admin/auth.go b/internal/admin/auth.go
+++ b/internal/admin/auth.go
diff --git a/cmd/main.go b/cmd/main.go
+++ b/cmd/main.go
`}
	got := Paths(b)
	// Sorted unique, including duplicate cmd/main.go collapsed.
	want := []string{"cmd/main.go", "internal/admin/auth.go", "internal/api/auth.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPaths_IgnoresDevNull(t *testing.T) {
	b := Bundle{Diff: `diff --git a/old.go b/old.go
deleted file mode 100644
--- a/old.go
+++ /dev/null
diff --git a/new.go b/new.go
+++ b/new.go
`}
	got := Paths(b)
	want := []string{"new.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPaths_IncludesUntracked(t *testing.T) {
	b := Bundle{
		Diff: `diff --git a/tracked.go b/tracked.go
+++ b/tracked.go
`,
		Untracked: []UntrackedFile{
			{Path: "scripts/new-tool.sh"},
			{Path: "tracked.go"}, // duplicate against diff — must dedupe
		},
	}
	got := Paths(b)
	want := []string{"scripts/new-tool.sh", "tracked.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPaths_HandlesPathsWithSpaces(t *testing.T) {
	// Git uses quoting for paths with spaces, but in unified diff the b/-prefix
	// path is the literal name. Cover the common case.
	b := Bundle{Diff: `+++ b/src/app folder/file.tsx
`}
	got := Paths(b)
	want := []string{"src/app folder/file.tsx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPaths_NoBPrefixLines(t *testing.T) {
	// Diff without proper +++ b/ headers (e.g., malformed) → empty.
	b := Bundle{Diff: "no diff headers here\njust prose\n"}
	got := Paths(b)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestDiffOnlyPaths_ExcludesUntracked(t *testing.T) {
	// Untracked-only file MUST NOT appear in DiffOnlyPaths.
	// A path that is BOTH in the diff AND untracked must still appear once
	// (from the diff side).
	b := Bundle{
		Diff: `diff --git a/tracked.go b/tracked.go
+++ b/tracked.go
diff --git a/both.go b/both.go
+++ b/both.go
`,
		Untracked: []UntrackedFile{
			{Path: "scripts/new-tool.sh"}, // untracked only — must be EXCLUDED
			{Path: "both.go"},             // both diff + untracked — must appear once
		},
	}
	got := DiffOnlyPaths(b)
	want := []string{"both.go", "tracked.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffOnlyPaths got %v want %v", got, want)
	}
}

func TestDiffOnlyPaths_EmptyBundle(t *testing.T) {
	got := DiffOnlyPaths(Bundle{})
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}
