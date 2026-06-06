package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/provider/mock"
)

func TestShouldEnrich_ScopedPathsBypassWorkspaceFileCap(t *testing.T) {
	ws := t.TempDir()
	mustRunInDir(t, ws, "git", "init")
	mustRunInDir(t, ws, "git", "config", "user.email", "test@example.com")
	mustRunInDir(t, ws, "git", "config", "user.name", "Test")

	for i := 0; i < 25; i++ {
		p := filepath.Join(ws, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRunInDir(t, ws, "git", "add", ".")
	mustRunInDir(t, ws, "git", "commit", "-m", "init")

	e, err := New(Config{
		Provider:         mock.New("", 0, 0),
		WorkspaceRoot:    ws,
		WorkspaceFileCap: 10,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	bundle := diff.Bundle{Diff: "--- a/x\n+++ b/x\n"}

	if e.shouldEnrich(bundle, nil) {
		t.Fatalf("unscoped: expected shouldEnrich=false (cap should fire)")
	}
	if !e.shouldEnrich(bundle, []string{"."}) {
		t.Fatalf("scoped: expected shouldEnrich=true (cap should be bypassed)")
	}
}

func mustRunInDir(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
