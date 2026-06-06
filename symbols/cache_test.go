//go:build !lite

package symbols

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractCache_MissThenHit(t *testing.T) {
	tmp := t.TempDir()
	cache := NewExtractCache(tmp)

	src := []byte("<?php class Foo {}")
	calls := 0
	extractor := func(path string, content []byte) []Symbol {
		calls++
		return []Symbol{{Name: "Foo", Kind: SymDef, File: path, Line: 1}}
	}

	// First call → miss
	syms1, hit1, err := cache.GetOrExtract("Foo.php", src, 0, extractor)
	if err != nil {
		t.Fatalf("first call err: %v", err)
	}
	if hit1 {
		t.Error("first call should be a miss")
	}
	if calls != 1 {
		t.Errorf("extractor calls after miss: got %d, want 1", calls)
	}
	if len(syms1) != 1 {
		t.Fatalf("first call syms: %+v", syms1)
	}

	// Second call → hit
	syms2, hit2, err := cache.GetOrExtract("Foo.php", src, 0, extractor)
	if err != nil {
		t.Fatalf("second call err: %v", err)
	}
	if !hit2 {
		t.Error("second call should be a hit")
	}
	if calls != 1 {
		t.Errorf("extractor calls after hit: got %d, want 1 (cache should not re-extract)", calls)
	}
	if len(syms2) != len(syms1) || syms2[0].Name != syms1[0].Name {
		t.Errorf("cached syms differ: got %+v vs %+v", syms2, syms1)
	}

	// Verify entry on disk exists.
	pattern := filepath.Join(tmp, "*", "*.json")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		t.Error("no cache entry on disk")
	}
}

func TestExtractCache_MissesOnContentChange(t *testing.T) {
	tmp := t.TempDir()
	cache := NewExtractCache(tmp)
	calls := 0
	extractor := func(path string, content []byte) []Symbol {
		calls++
		return []Symbol{{Name: "X", Kind: SymDef, File: path, Line: 1}}
	}

	_, hit1, _ := cache.GetOrExtract("F.php", []byte("<?php class F {}"), 0, extractor)
	if hit1 {
		t.Error("first call should miss")
	}
	_, hit2, _ := cache.GetOrExtract("F.php", []byte("<?php class G {}"), 0, extractor)
	if hit2 {
		t.Error("changed content should miss")
	}
	if calls != 2 {
		t.Errorf("extractor calls: got %d, want 2 (both should be misses)", calls)
	}
}

func TestExtractCache_MissesOnStrategyChange(t *testing.T) {
	tmp := t.TempDir()
	cache := NewExtractCache(tmp)
	calls := 0
	extractor := func(path string, content []byte) []Symbol {
		calls++
		return nil
	}
	src := []byte("<?php class F {}")

	_, _, _ = cache.GetOrExtract("F.php", src, 0, extractor)    // strategy=0
	_, hit, _ := cache.GetOrExtract("F.php", src, 2, extractor) // strategy=2 → must miss
	if hit {
		t.Error("strategy change should miss")
	}
	if calls != 2 {
		t.Errorf("extractor calls: got %d, want 2", calls)
	}
}

func TestExtractCache_HandlesUnwritableRoot(t *testing.T) {
	// Create the root as a file (not a directory) to make MkdirAll fail.
	tmpFile, err := os.CreateTemp("", "blocker-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cache := NewExtractCache(tmpFile.Name())
	extractor := func(path string, content []byte) []Symbol {
		return []Symbol{{Name: "X"}}
	}
	syms, hit, err := cache.GetOrExtract("F.php", []byte("<?php class F {}"), 0, extractor)
	if err == nil {
		t.Skip("OS allowed mkdir over file — skipping (test depends on POSIX semantics)")
	}
	if hit {
		t.Error("should not hit on unwritable cache")
	}
	if len(syms) == 0 {
		t.Error("extractor result must still be returned despite cache error")
	}
}
