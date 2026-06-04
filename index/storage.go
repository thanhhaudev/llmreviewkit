package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrSchemaVersionMismatch is returned by LoadJSON when the persisted
// index's Version != CurrentSchemaVersion. Callers should rebuild.
var ErrSchemaVersionMismatch = errors.New("index schema version mismatch")

// IsSchemaVersionMismatch reports whether err is a wrapped
// ErrSchemaVersionMismatch.
func IsSchemaVersionMismatch(err error) bool {
	return errors.Is(err, ErrSchemaVersionMismatch)
}

// WriteJSON serializes idx to path via atomic temp+rename. The temp file
// is created in the same directory as path so the rename is on the same
// filesystem. Mode 0600. Parent directory is created if missing.
func WriteJSON(path string, idx *Index) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".index-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// Ensure cleanup on early return.
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "")
	if err := enc.Encode(idx); err != nil {
		tmp.Close()
		return fmt.Errorf("encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	tmpPath = "" // skip cleanup, rename succeeded
	return nil
}

// LoadJSON reads and parses an index from disk. Returns
// ErrSchemaVersionMismatch if the persisted Version != CurrentSchemaVersion.
// On success, the returned Index has bySymbol populated.
func LoadJSON(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("unmarshal index: %w", err)
	}
	if idx.Version != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: file=%d current=%d", ErrSchemaVersionMismatch, idx.Version, CurrentSchemaVersion)
	}
	idx.RebuildLookups()
	return &idx, nil
}

// Lock represents a held file lock; release via Release().
type Lock struct {
	path string
	file *os.File
}

// AcquireLock tries to acquire an exclusive lock on path. Creates the file
// with O_EXCL — if the file exists, retry until timeout. Times out after
// timeout duration, returning a timeout error.
//
// Note: this is best-effort cross-process exclusion via lockfile-creation
// pattern. POSIX flock would be more robust but requires syscall.Flock
// which works on Unix only. Lockfile is cross-platform.
func AcquireLock(path string, timeout time.Duration) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return &Lock{path: path, file: f}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("lock timeout (held by another process? path=%s)", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Release closes the lock file and removes it. Idempotent — safe to call
// twice or via defer even on AcquireLock failure (nil receiver).
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	if l.path != "" {
		_ = os.Remove(l.path)
		l.path = ""
	}
	return nil
}
