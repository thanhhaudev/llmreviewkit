package expand

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestTests_GoConvention(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, ws, "auth/auth.go", "package auth")
	mustWrite(t, ws, "auth/auth_test.go", "package auth")

	got := Tests([]string{"auth/auth.go"}, ws)
	if len(got) != 1 {
		t.Fatalf("want 1 test candidate, got %d: %+v", len(got), got)
	}
	if got[0].File != "auth/auth_test.go" {
		t.Fatalf("want auth/auth_test.go, got %s", got[0].File)
	}
	if got[0].Strategy != "test" {
		t.Fatalf("want Strategy=test, got %q", got[0].Strategy)
	}
}

func TestTests_PythonConventions(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, ws, "app/util.py", "")
	mustWrite(t, ws, "app/test_util.py", "")
	mustWrite(t, ws, "tests/test_util.py", "")
	mustWrite(t, ws, "app/util_test.py", "")

	got := Tests([]string{"app/util.py"}, ws)
	sort.Slice(got, func(i, j int) bool { return got[i].File < got[j].File })
	wantSet := map[string]bool{
		"app/test_util.py":   true,
		"app/util_test.py":   true,
		"tests/test_util.py": true,
	}
	for _, c := range got {
		if !wantSet[c.File] {
			t.Errorf("unexpected: %s", c.File)
		}
		delete(wantSet, c.File)
	}
	if len(wantSet) > 0 {
		t.Errorf("missing test files: %v", wantSet)
	}
}

func TestTests_PHPConvention(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, ws, "src/User.php", "")
	mustWrite(t, ws, "tests/UserTest.php", "")

	got := Tests([]string{"src/User.php"}, ws)
	if len(got) != 1 || got[0].File != "tests/UserTest.php" {
		t.Fatalf("want tests/UserTest.php, got %+v", got)
	}
}

func TestTests_TSConventions(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, ws, "web/Foo.ts", "")
	mustWrite(t, ws, "web/Foo.test.ts", "")
	mustWrite(t, ws, "web/Foo.spec.ts", "")

	got := Tests([]string{"web/Foo.ts"}, ws)
	sort.Slice(got, func(i, j int) bool { return got[i].File < got[j].File })
	if len(got) != 2 {
		t.Fatalf("want 2 (test + spec), got %d: %+v", len(got), got)
	}
}

func TestTests_NoMatchReturnsEmpty(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, ws, "auth.go", "")
	// no test file

	got := Tests([]string{"auth.go"}, ws)
	if len(got) != 0 {
		t.Fatalf("want 0, got %d", len(got))
	}
}

func TestTests_UnknownExtensionIgnored(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, ws, "readme.md", "")
	got := Tests([]string{"readme.md"}, ws)
	if len(got) != 0 {
		t.Fatalf("want 0 for .md, got %d", len(got))
	}
}

func mustWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
