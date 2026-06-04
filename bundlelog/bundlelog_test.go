package bundlelog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendTo_WritesJSONLine(t *testing.T) {
	var buf bytes.Buffer
	entry := Entry{
		Timestamp: "2026-06-04T10:00:00Z",
		Workspace: "test-ws",
		DiffFiles: 3,
		Stats: Stats{Extracted: 10, Filtered: 8, Resolved: 5, Attached: 3, Dropped: 1, BudgetBytes: 32768, UsedBytes: 1024},
	}
	if err := AppendTo(&buf, entry); err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline, got %q", out)
	}
	if !strings.Contains(out, `"ws":"test-ws"`) {
		t.Fatalf("expected workspace in JSON, got %q", out)
	}
	// Verify well-formed JSON.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimRight(out, "\n")), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestAppendTo_NilSinkIsNoop(t *testing.T) {
	if err := AppendTo(nil, Entry{}); err != nil {
		t.Fatalf("nil sink should be no-op, got err: %v", err)
	}
}

func TestAppendWithRotation_WritesAndRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	backup := filepath.Join(dir, "log.1.jsonl")

	// First write — file doesn't exist.
	if err := AppendWithRotation(path, backup, 1024, Entry{Workspace: "first"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("path should exist: %v", err)
	}

	// Fill the file past the cap to trigger rotation on next write.
	big := make([]byte, 2048)
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Next write should rotate.
	if err := AppendWithRotation(path, backup, 1024, Entry{Workspace: "afterrotate"}); err != nil {
		t.Fatalf("rotate write: %v", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup should exist after rotation: %v", err)
	}
	// New path file should only contain the fresh entry.
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "afterrotate") {
		t.Fatalf("expected fresh entry in rotated file, got %q", data)
	}
	if strings.Contains(string(data), strings.Repeat("\x00", 100)) {
		t.Fatalf("rotated file should not still contain the big seed")
	}
}

func TestAppendWithRotation_EmptyPathIsNoop(t *testing.T) {
	if err := AppendWithRotation("", "", 0, Entry{}); err != nil {
		t.Fatalf("empty path should be no-op: %v", err)
	}
}

func TestStats_NewV0_13Fields(t *testing.T) {
	s := Stats{
		Extracted: 10, Filtered: 8, Resolved: 5,
		Attached: 3, Dropped: 1,
		BudgetBytes: 32768, UsedBytes: 1024,
		// v0.13 additions:
		IndexHits:    5,
		IndexMisses:  3,
		ResolverPath: "v2",
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	for _, want := range []string{`"index_hits":5`, `"index_misses":3`, `"resolver_path":"v2"`} {
		if !strings.Contains(got, want) {
			t.Errorf("Stats JSON missing %s; got %s", want, got)
		}
	}
}
