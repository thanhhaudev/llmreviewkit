package glossary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLoad_NoFile_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	g, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Path != "" || g.Content != "" {
		t.Fatalf("expected empty Glossary, got %+v", g)
	}
}

func TestLoad_PriorityKizunaxFirst(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".kizunax"))
	mustWrite(t, filepath.Join(dir, ".kizunax", "glossary.md"), "kizunax-wins")
	mustMkdir(t, filepath.Join(dir, "docs"))
	mustWrite(t, filepath.Join(dir, "docs", "glossary.md"), "docs-loses")
	mustWrite(t, filepath.Join(dir, "GLOSSARY.md"), "upper-loses")

	g, err := Load(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if g.Content != "kizunax-wins" {
		t.Fatalf("expected kizunax-wins, got %q", g.Content)
	}
	if !strings.HasSuffix(g.Path, ".kizunax/glossary.md") {
		t.Fatalf("unexpected path: %s", g.Path)
	}
}

func TestLoad_PriorityDocsBeforeUpper(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "docs"))
	mustWrite(t, filepath.Join(dir, "docs", "glossary.md"), "docs-wins")
	mustWrite(t, filepath.Join(dir, "GLOSSARY.md"), "upper-loses")

	g, _ := Load(dir)
	if g.Content != "docs-wins" {
		t.Fatalf("expected docs-wins, got %q", g.Content)
	}
}

func TestLoad_UpperFallback(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "GLOSSARY.md"), "upper-only")
	g, _ := Load(dir)
	if g.Content != "upper-only" {
		t.Fatalf("expected upper-only, got %q", g.Content)
	}
}

func TestLoad_TruncatesOver16KiB(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("a", maxGlossaryBytes+5_000)
	mustWrite(t, filepath.Join(dir, "GLOSSARY.md"), huge)
	g, err := Load(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(g.Content) != maxGlossaryBytes {
		t.Fatalf("expected %d bytes after truncation, got %d", maxGlossaryBytes, len(g.Content))
	}
	if !g.Truncated {
		t.Fatalf("expected Truncated=true")
	}
}

func TestLoad_TruncatesAtRuneBoundary_NotMidRune(t *testing.T) {
	dir := t.TempDir()
	// 3-byte rune "ế" repeated past the cap. A naive byte slice could split
	// the rune at byte 16384 and yield invalid UTF-8.
	huge := strings.Repeat("ế", maxGlossaryBytes)
	mustWrite(t, filepath.Join(dir, "GLOSSARY.md"), huge)

	g, err := Load(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !g.Truncated {
		t.Fatalf("expected Truncated=true")
	}
	if !utf8.ValidString(g.Content) {
		t.Fatalf("glossary content is not valid UTF-8 after truncation")
	}
	if len(g.Content) > maxGlossaryBytes {
		t.Fatalf("content %d bytes exceeds cap %d", len(g.Content), maxGlossaryBytes)
	}
}

func TestLoad_ZeroByteFile_ReturnsEmptyContent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "GLOSSARY.md"), "")
	g, _ := Load(dir)
	if g.Content != "" {
		t.Fatalf("expected empty content, got %q", g.Content)
	}
	if g.Path == "" {
		t.Fatalf("expected non-empty Path for 0-byte file (file existed)")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}
