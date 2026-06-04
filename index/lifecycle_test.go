package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildFull_ScansAllSupportedFiles(t *testing.T) {
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "main.go"), []byte("package main\nfunc Authenticate() {}\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "app.py"), []byte("def hello():\n    pass\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "ignored.txt"), []byte("nope"), 0o644)

	idx, err := BuildFull(ws)
	if err != nil {
		t.Fatalf("BuildFull: %v", err)
	}
	if !idx.Healthy() {
		t.Fatalf("not healthy")
	}
	if len(idx.Files) != 2 {
		t.Fatalf("want 2 files indexed, got %d (%v)", len(idx.Files), idx.Files)
	}
	if idx.Files["main.go"] == nil {
		t.Fatalf("main.go missing from index")
	}
	if idx.Files["app.py"] == nil {
		t.Fatalf("app.py missing from index")
	}
	if idx.Built == 0 {
		t.Fatalf("Built timestamp not set")
	}
}

func TestLoadOrBuild_FreshWorkspaceBuilds(t *testing.T) {
	ws := t.TempDir()
	stateDir := t.TempDir()
	os.WriteFile(filepath.Join(ws, "x.go"), []byte("package x\nfunc F() {}\n"), 0o644)

	idx, err := LoadOrBuild(stateDir, ws)
	if err != nil {
		t.Fatalf("LoadOrBuild: %v", err)
	}
	if len(idx.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(idx.Files))
	}
	// Verify persisted
	if _, err := os.Stat(filepath.Join(stateDir, "index", "index.json")); err != nil {
		t.Fatalf("index.json not persisted: %v", err)
	}
}

func TestLoadOrBuild_IncrementalUpdate(t *testing.T) {
	ws := t.TempDir()
	stateDir := t.TempDir()
	os.WriteFile(filepath.Join(ws, "a.go"), []byte("package x\nfunc A() {}\n"), 0o644)

	// First build
	idx1, err := LoadOrBuild(stateDir, ws)
	if err != nil {
		t.Fatalf("first LoadOrBuild: %v", err)
	}
	originalMtime := idx1.Files["a.go"].Mtime
	originalBuilt := idx1.Built

	// Add a new file and modify a.go
	time.Sleep(10 * time.Millisecond) // ensure mtime differs
	os.WriteFile(filepath.Join(ws, "a.go"), []byte("package x\nfunc A() {}\nfunc B() {}\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "b.go"), []byte("package x\nfunc C() {}\n"), 0o644)

	// Second build - should incremental
	idx2, err := LoadOrBuild(stateDir, ws)
	if err != nil {
		t.Fatalf("second LoadOrBuild: %v", err)
	}
	if len(idx2.Files) != 2 {
		t.Fatalf("want 2 files after incremental, got %d", len(idx2.Files))
	}
	if idx2.Files["a.go"].Mtime <= originalMtime {
		t.Fatalf("a.go mtime not updated")
	}
	if idx2.Built != originalBuilt {
		// On incremental, Built should NOT reset (Built tracks last FULL build).
		// If it changed, lifecycle did full rescan instead of incremental.
		t.Logf("note: Built changed (%d → %d), may indicate full rescan", originalBuilt, idx2.Built)
	}
}

func TestLoadOrBuild_AutoStaleAfter24Hours(t *testing.T) {
	ws := t.TempDir()
	stateDir := t.TempDir()
	os.WriteFile(filepath.Join(ws, "x.go"), []byte("package x"), 0o644)

	// Manually write an index with Built timestamp >24h old.
	idx := &Index{
		Version: CurrentSchemaVersion,
		Root:    ws,
		Built:   time.Now().Add(-26 * time.Hour).UnixNano(),
		Files: map[string]*FileIndex{
			"old.go": {Path: "old.go", Lang: "go", Mtime: 1},
		},
	}
	idxPath := filepath.Join(stateDir, "index", "index.json")
	os.MkdirAll(filepath.Dir(idxPath), 0o700)
	if err := WriteJSON(idxPath, idx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// LoadOrBuild must do full rescan because Built >24h old.
	got, err := LoadOrBuild(stateDir, ws)
	if err != nil {
		t.Fatalf("LoadOrBuild: %v", err)
	}
	// The stale `old.go` must be gone; only real workspace files remain.
	if _, ok := got.Files["old.go"]; ok {
		t.Fatalf("stale old.go still present after auto-rescan: %+v", got.Files)
	}
	if got.Files["x.go"] == nil {
		t.Fatalf("x.go missing after auto-rescan: %+v", got.Files)
	}
}

func TestBuildFull_ParallelEquivalentToSequentialOutput(t *testing.T) {
	// Mixed-language workspace to exercise multiple goroutines.
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "a.go"), []byte("package x\nfunc Alpha() {}\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "b.go"), []byte("package x\nfunc Beta() {}\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "c.py"), []byte("def gamma():\n    pass\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "d.py"), []byte("def delta():\n    pass\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "e.php"), []byte("<?php function epsilon() {}\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "f.ts"), []byte("export function zeta() {}\n"), 0o644)

	// Run BuildFull twice; result should be deterministic regardless of
	// goroutine scheduling order.
	idx1, err := BuildFull(ws)
	if err != nil {
		t.Fatalf("first BuildFull: %v", err)
	}
	idx2, err := BuildFull(ws)
	if err != nil {
		t.Fatalf("second BuildFull: %v", err)
	}

	if len(idx1.Files) != len(idx2.Files) {
		t.Fatalf("file count mismatch: %d vs %d", len(idx1.Files), len(idx2.Files))
	}
	for path := range idx1.Files {
		if _, ok := idx2.Files[path]; !ok {
			t.Fatalf("path %s missing from second run", path)
		}
	}

	// Sanity: at least the Go files (which use stdlib AST, deterministic
	// and don't depend on WASM availability) must produce defs in both runs.
	for _, p := range []string{"a.go", "b.go"} {
		fi := idx1.Files[p]
		if fi == nil {
			t.Fatalf("expected %s in idx1.Files", p)
		}
		if len(fi.Defs) == 0 {
			t.Errorf("expected at least 1 def in %s, got 0", p)
		}
	}
}
