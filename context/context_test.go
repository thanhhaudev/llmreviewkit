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
	c, err := Load(dir)
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

// Imports below used by tests added in Task 2-3.
var (
	_ = strings.Contains
	_ = time.Now
	_ = utf8.ValidString
	_ = filepath.Join
)
