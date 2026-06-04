package statedir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve_DeterministicAndIsolated(t *testing.T) {
	base := t.TempDir()
	a, err := Resolve(base, "/path/to/projA")
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	a2, err := Resolve(base, "/path/to/projA")
	if err != nil {
		t.Fatalf("resolve A again: %v", err)
	}
	if a.Root != a2.Root {
		t.Fatalf("deterministic: want same Root, got %s vs %s", a.Root, a2.Root)
	}
	b, err := Resolve(base, "/other/path/projA")
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	if a.Root == b.Root {
		t.Fatalf("isolation: same basename diff paths must differ; both got %s", a.Root)
	}
	if !strings.HasPrefix(a.Root, base) {
		t.Fatalf("Root should be under base; got %s", a.Root)
	}
}

func TestWriteAtomic_RoundtripAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "child", "data.json")
	if err := WriteAtomic(path, []byte(`{"k":1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != `{"k":1}` {
		t.Fatalf("content: %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: want 0600, got %v", info.Mode().Perm())
	}
}
