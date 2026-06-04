package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFile_GoExtractsDefsAndRefs(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`package x

func Authenticate(user string) error {
	return nil
}

func caller() {
	Authenticate("foo")
}
`)
	path := filepath.Join(dir, "auth.go")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := ScanFile(dir, "auth.go")
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if fi.Path != "auth.go" {
		t.Fatalf("Path: want auth.go, got %s", fi.Path)
	}
	if fi.Lang != "go" {
		t.Fatalf("Lang: want go, got %s", fi.Lang)
	}
	if fi.Mtime == 0 {
		t.Fatalf("Mtime: want non-zero, got 0")
	}

	// Defs should include "Authenticate" (and "caller").
	defNames := names(fi.Defs)
	if !contains(defNames, "Authenticate") {
		t.Fatalf("Defs missing Authenticate: %v", defNames)
	}

	// Refs should include the call to Authenticate inside caller().
	refNames := names(fi.Refs)
	if !contains(refNames, "Authenticate") {
		t.Fatalf("Refs missing Authenticate call: %v", refNames)
	}
}

func TestScanFile_UnsupportedExtensionSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	fi, err := ScanFile(dir, "notes.txt")
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if fi != nil {
		t.Fatalf("expected nil FileIndex for unsupported ext, got %+v", fi)
	}
}

func TestScanFile_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ScanFile(dir, "no-such-file.go")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestWalkWorkspace_FindsSupportedFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.go":              "package main",
		"sub/util.go":          "package sub",
		"sub/util_test.go":     "package sub",
		"scripts/build.sh":     "#!/bin/bash",     // not indexed
		"node_modules/foo.js":  "x",               // skipped dir
		".git/HEAD":            "ref: ...",        // skipped dir
		"vendor/lib/x.go":      "package lib",     // skipped dir
		"app/auth.py":          "def Foo(): pass",
		"src/User.php":         "<?php class User {}",
		"web/component.tsx":    "export const X = 1;",
	}
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}

	paths, err := WalkWorkspace(dir)
	if err != nil {
		t.Fatalf("WalkWorkspace: %v", err)
	}

	// Expected: main.go, sub/util.go, sub/util_test.go, app/auth.py, src/User.php, web/component.tsx
	wantPresent := []string{"main.go", "sub/util.go", "sub/util_test.go", "app/auth.py", "src/User.php", "web/component.tsx"}
	for _, p := range wantPresent {
		if !contains(paths, p) {
			t.Errorf("WalkWorkspace missing %s; got %v", p, paths)
		}
	}

	// Expected absent
	for _, p := range []string{"scripts/build.sh", "node_modules/foo.js", ".git/HEAD", "vendor/lib/x.go"} {
		if contains(paths, p) {
			t.Errorf("WalkWorkspace should skip %s; got %v", p, paths)
		}
	}
}

// Test helpers
func names(locs []Location) []string {
	out := make([]string, len(locs))
	for i, l := range locs {
		out[i] = l.Name
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
