package symbols

import (
	"testing"

	"github.com/thanhhaudev/llmreviewkit/diff"
)

func TestExtractFromBundle_GoDiff(t *testing.T) {
	bundle := diff.Bundle{
		Diff: `diff --git a/main.go b/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
 package main
+
+import "path"
+
+func main() { _ = path.Base("/a") }
`,
	}
	syms := ExtractFromBundle(bundle)
	if len(syms) == 0 {
		t.Fatalf("expected symbols extracted, got 0")
	}
	names := symbolNames(syms, SymCall)
	wantContains(t, names, "Base")
}

func TestExtractFromBundle_Empty(t *testing.T) {
	syms := ExtractFromBundle(diff.Bundle{})
	if len(syms) != 0 {
		t.Fatalf("expected empty, got %+v", syms)
	}
}

func TestExtractFromBundle_DedupSameFile(t *testing.T) {
	// Same +++ b/file.go appearing twice in diff (split hunks) should
	// result in symbols deduped by (Name, Pkg, Kind, File, Line).
	bundle := diff.Bundle{
		Diff: `diff --git a/x.go b/x.go
+++ b/x.go
@@ -1 +1 @@
+func A() {}
diff --git a/x.go b/x.go
+++ b/x.go
@@ -10 +10 @@
+func B() {}
`,
	}
	syms := ExtractFromBundle(bundle)
	defs := symbolNames(syms, SymDef)
	wantContains(t, defs, "A", "B")
}

func TestExtractFromBundle_UntrackedFile(t *testing.T) {
	bundle := diff.Bundle{
		Untracked: []diff.UntrackedFile{
			{
				Path:    "newfile.go",
				Content: "package x\nfunc New() {}\n",
			},
		},
	}
	syms := ExtractFromBundle(bundle)
	defs := symbolNames(syms, SymDef)
	wantContains(t, defs, "New")
}
