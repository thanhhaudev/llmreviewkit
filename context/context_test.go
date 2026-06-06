package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLoad_NoFile_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Path != "" || c.Content != "" {
		t.Fatalf("expected empty Context, got %+v", c)
	}
	if !c.ModTime.IsZero() {
		t.Fatalf("expected zero ModTime, got %v", c.ModTime)
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

func TestLoad_RespectsCallerProvidedPriority(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".kizunax"))
	mustWrite(t, filepath.Join(dir, ".kizunax", "review-context.md"), "kizunax-wins")
	mustMkdir(t, filepath.Join(dir, "docs"))
	mustWrite(t, filepath.Join(dir, "docs", "review-context.md"), "docs-loses")
	mustWrite(t, filepath.Join(dir, "REVIEW-CONTEXT.md"), "upper-loses")

	c, err := Load(dir, []string{
		filepath.Join(".kizunax", "review-context.md"),
		filepath.Join("docs", "review-context.md"),
		"REVIEW-CONTEXT.md",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.Content != "kizunax-wins" {
		t.Fatalf("expected kizunax-wins, got %q", c.Content)
	}
	if !strings.HasSuffix(c.Path, ".kizunax/review-context.md") {
		t.Fatalf("unexpected path: %s", c.Path)
	}
}

func TestLoad_PriorityDocsBeforeUpper(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "docs"))
	mustWrite(t, filepath.Join(dir, "docs", "review-context.md"), "docs-wins")
	mustWrite(t, filepath.Join(dir, "REVIEW-CONTEXT.md"), "upper-loses")

	c, _ := Load(dir, DefaultPaths())
	if c.Content != "docs-wins" {
		t.Fatalf("expected docs-wins, got %q", c.Content)
	}
}

func TestLoad_UpperFallback(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "REVIEW-CONTEXT.md"), "upper-only")
	c, _ := Load(dir, DefaultPaths())
	if c.Content != "upper-only" {
		t.Fatalf("expected upper-only, got %q", c.Content)
	}
}

func TestLoad_ModTimeExposed(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "REVIEW-CONTEXT.md"), "with-time")
	before := time.Now().Add(-time.Minute)

	c, _ := Load(dir, DefaultPaths())
	if c.ModTime.IsZero() {
		t.Fatalf("expected non-zero ModTime")
	}
	if c.ModTime.Before(before) {
		t.Fatalf("ModTime %v is before %v", c.ModTime, before)
	}
}

func TestLoad_TruncatesOver8KiB(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("a", maxContextBytes+5_000)
	mustWrite(t, filepath.Join(dir, "REVIEW-CONTEXT.md"), huge)
	c, err := Load(dir, DefaultPaths())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(c.Content) != maxContextBytes {
		t.Fatalf("expected %d bytes after truncation, got %d", maxContextBytes, len(c.Content))
	}
	if !c.Truncated {
		t.Fatalf("expected Truncated=true")
	}
}

func TestLoad_TruncatesAtRuneBoundary_NotMidRune(t *testing.T) {
	dir := t.TempDir()
	// 3-byte rune "ế" repeated past the cap. A naive byte slice could split
	// the rune at byte 8192 and yield invalid UTF-8.
	huge := strings.Repeat("ế", maxContextBytes)
	mustWrite(t, filepath.Join(dir, "REVIEW-CONTEXT.md"), huge)

	c, err := Load(dir, DefaultPaths())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !c.Truncated {
		t.Fatalf("expected Truncated=true")
	}
	if !utf8.ValidString(c.Content) {
		t.Fatalf("context content is not valid UTF-8 after truncation")
	}
	if len(c.Content) > maxContextBytes {
		t.Fatalf("content %d bytes exceeds cap %d", len(c.Content), maxContextBytes)
	}
}

func TestLoad_ZeroByteFile_ReturnsEmptyContentButRecordsPath(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "REVIEW-CONTEXT.md"), "")
	c, _ := Load(dir, DefaultPaths())
	if c.Content != "" {
		t.Fatalf("expected empty content, got %q", c.Content)
	}
	if c.Path == "" {
		t.Fatalf("expected non-empty Path for 0-byte file (file existed)")
	}
}

func TestLoad_DirectoryAtCandidatePath_Skipped(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "REVIEW-CONTEXT.md")) // create as dir
	c, err := Load(dir, DefaultPaths())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.Path != "" {
		t.Fatalf("directory at candidate path should be skipped, got Path=%q", c.Path)
	}
}

func TestLoad_CallerSuppliedPaths(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "custom.md"), "custom-context")

	c, err := Load(dir, []string{"custom.md"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.Content != "custom-context" {
		t.Fatalf("expected custom-context, got %q", c.Content)
	}
}

func TestLoad_DefaultPaths_DoesNotIncludeKizunaxDir(t *testing.T) {
	for _, p := range DefaultPaths() {
		if strings.HasPrefix(p, ".kizunax") {
			t.Fatalf("DefaultPaths() must not include .kizunax/ paths (consumer concern), got %q", p)
		}
	}
}

func TestLoad_NilPaths_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "docs"))
	mustWrite(t, filepath.Join(dir, "docs", "review-context.md"), "docs-wins")
	c, _ := Load(dir, nil)
	if c.Content != "docs-wins" {
		t.Fatalf("expected docs-wins via DefaultPaths(), got %q", c.Content)
	}
}
