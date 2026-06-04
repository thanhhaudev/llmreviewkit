// Package bundlelog persists per-review bundle composition records to a jsonl
// file for offline pivot during measurement windows. Env coupling is the
// consumer's responsibility — this library never reads env vars.
package bundlelog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/thanhhaudev/llmreviewkit/diff"
)

const (
	LogName      = "bundle-log.jsonl"
	BackupName   = "bundle-log.1.jsonl"
	SizeCapBytes = 10 * 1024 * 1024 // 10 MiB
)

// Entry is one jsonl record (one line per review).
type Entry struct {
	Timestamp string                        `json:"ts"`
	Workspace string                        `json:"ws"`
	DiffFiles int                           `json:"diff_files"`
	Bundle    []diff.ReferencedFileLogEntry `json:"bundle"`
	Stats     Stats                         `json:"stats"`
}

// Stats is the aggregate counts block. Field names match resolver.ResolveStats(V2)
// + diff.AttachResult for direct jq pivot.
type Stats struct {
	Extracted   int `json:"extracted"`
	Filtered    int `json:"filtered"`
	Resolved    int `json:"resolved"`
	Attached    int `json:"attached"`
	Dropped     int `json:"dropped"`
	BudgetBytes int `json:"budget_bytes"`
	UsedBytes   int `json:"used_bytes"`
	// v0.13.0 additions (omitempty so v0.12.4 entries don't break parsing)
	IndexHits    int    `json:"index_hits,omitempty"`
	IndexMisses  int    `json:"index_misses,omitempty"`
	ResolverPath string `json:"resolver_path,omitempty"`
}

// AppendTo writes one jsonl record to sink. Returns the marshal/write error so
// the caller decides whether to log it (the library never touches stderr).
// nil sink is a no-op so callers don't need to wrap in an `if`.
func AppendTo(sink io.Writer, entry Entry) error {
	if sink == nil {
		return nil
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	line = append(line, '\n')
	if _, err := sink.Write(line); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// AppendWithRotation is a convenience helper for consumers that want
// disk-backed logging with the 10 MiB rotation convention from the
// kizunax v0.12.4 baseline. It opens path for append, rotates to
// backupPath if path is already ≥capBytes, writes the entry, closes.
// Returns the first non-nil error encountered.
//
// path = "" is a no-op (treated like nil sink).
func AppendWithRotation(path, backupPath string, capBytes int64, entry Entry) error {
	if path == "" {
		return nil
	}
	if capBytes > 0 && backupPath != "" {
		if info, err := os.Stat(path); err == nil && info.Size() >= capBytes {
			_ = os.Rename(path, backupPath)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return AppendTo(f, entry)
}
