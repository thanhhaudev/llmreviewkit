// Package statedir provides per-workspace state directory helpers used by
// llmreviewkit consumers to persist index files, telemetry logs, and other
// out-of-tree artifacts that should live alongside a project but not in it.
//
// Layout convention (overridable by callers):
//
//	<baseDir>/<workspace-hash>/...
//
// where <workspace-hash> = basename(abs(workspaceRoot)) + sha256-prefix(abs(workspaceRoot)).
// Two workspaces with the same basename at different paths get isolated state.
package statedir

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// WorkspaceDir identifies the on-disk state directory for one workspace.
type WorkspaceDir struct {
	Root string // absolute path; created by Resolve
}

// Resolve derives a deterministic state directory rooted at baseDir for the
// given workspaceRoot. The directory is created with mode 0o700 if missing.
//
// Example:
//
//	ws, err := statedir.Resolve("/home/u/.llmreviewkit", "/repo/myproj")
//	// ws.Root == "/home/u/.llmreviewkit/myproj-<hash>"
func Resolve(baseDir, workspaceRoot string) (WorkspaceDir, error) {
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return WorkspaceDir{}, fmt.Errorf("abs: %w", err)
	}
	base := filepath.Base(abs)
	h := sha256.Sum256([]byte(abs))
	name := fmt.Sprintf("%s-%s", base, hex.EncodeToString(h[:8]))
	root := filepath.Join(baseDir, name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return WorkspaceDir{}, fmt.Errorf("mkdir: %w", err)
	}
	return WorkspaceDir{Root: root}, nil
}

// WriteAtomic writes data to path via temp+rename so concurrent readers
// never see a half-written file. Parent dir is created with mode 0o700 if
// missing. Mode applied to the final file.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
