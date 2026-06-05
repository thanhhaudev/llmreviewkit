package engine

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/thanhhaudev/llmreviewkit/bundlelog"
	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/resolver"
	"github.com/thanhhaudev/llmreviewkit/statedir"
)

// shouldEnrich is the v1.2.0 gate for the enrichment pipeline. It folds
// in the legacy "WorkspaceRoot non-empty" check from engine.go:117, the
// new SkipEnrichment kill switch, and the optional WorkspaceFileCap
// auto-degrade. Returns false (with a verbose log on auto-degrade) when
// the call should skip extraction + resolver + attachment.
func (e *Engine) shouldEnrich(bundle diff.Bundle) bool {
	if e.cfg.WorkspaceRoot == "" {
		return false
	}
	if e.cfg.SkipEnrichment {
		return false
	}
	if len(bundle.Diff) == 0 && len(bundle.Untracked) == 0 {
		return false
	}
	if e.cfg.WorkspaceFileCap > 0 {
		if count, ok := countTrackedFiles(e.cfg.WorkspaceRoot); ok && count > e.cfg.WorkspaceFileCap {
			e.logf("[warn] workspace has %d tracked files (cap %d); auto-skipping enrichment to avoid CPU spin\n",
				count, e.cfg.WorkspaceFileCap)
			return false
		}
	}
	return true
}

// countTrackedFiles returns the count of tracked files in the repo at
// cwd, via `git ls-files -z`. Returns (count, true) on success or
// (0, false) on any error — callers fail open (proceed with enrichment)
// so a broken git environment doesn't accidentally degrade behavior.
func countTrackedFiles(cwd string) (int, bool) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	return bytes.Count(out, []byte{0}), true
}

// refsToInputs converts resolver.Reference to diff.ReferenceInput,
// crossing the package boundary without an import cycle. Moved from
// internal/runner.toReferenceInputs.
func refsToInputs(refs []resolver.Reference) []diff.ReferenceInput {
	out := make([]diff.ReferenceInput, len(refs))
	for i, r := range refs {
		out[i] = diff.ReferenceInput{
			Path:    r.File,
			Excerpt: r.Excerpt,
			Symbols: []string{r.Symbol.Name},
		}
	}
	return out
}

// assembleBundleLogEntry builds the per-review bundlelog.Entry. Reason
// inference (priority):
//  1. Paths in bundle.Diff (diff headers only, NOT untracked) → "diff_file"
//  2. Paths in bundle.Untracked → "untracked_text"
//  3. attachRes.Files already carries Reason="def_match:<csv>" from attach.go
//
// Using diff.DiffOnlyPaths (not diff.Paths) is deliberate — diff.Paths
// includes untracked files for canonicalization, but we want them only
// once under "untracked_text".
//
// Workspace identifier = basename of ws.Root.
func assembleBundleLogEntry(
	bundle diff.Bundle,
	attachRes diff.AttachResult,
	stats resolver.ResolveStats,
	ws statedir.WorkspaceDir,
	indexHits, indexMisses int,
	resolverPath string,
	expansionByStrategy map[string]int,
	expansionDropped int,
) bundlelog.Entry {
	diffOnlyPaths := diff.DiffOnlyPaths(bundle)
	bundleList := make([]diff.ReferencedFileLogEntry, 0, len(diffOnlyPaths)+len(bundle.Untracked)+len(attachRes.Files))

	for _, p := range diffOnlyPaths {
		bundleList = append(bundleList, diff.ReferencedFileLogEntry{
			Path:   p,
			Reason: "diff_file",
			Bytes:  0,
		})
	}
	for _, u := range bundle.Untracked {
		bundleList = append(bundleList, diff.ReferencedFileLogEntry{
			Path:   u.Path,
			Reason: "untracked_text",
			Bytes:  u.Bytes,
		})
	}
	bundleList = append(bundleList, attachRes.Files...)

	wsLabel := ""
	if ws.Root != "" {
		wsLabel = filepath.Base(ws.Root)
	}

	return bundlelog.Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Workspace: wsLabel,
		DiffFiles: len(diffOnlyPaths),
		Bundle:    bundleList,
		Stats: bundlelog.Stats{
			Extracted:    stats.ExtractedCount,
			Filtered:     stats.FilteredCount,
			Resolved:     stats.ResolvedCount,
			Attached:     attachRes.Attached,
			Dropped:      attachRes.Dropped,
			BudgetBytes:  attachRes.BudgetBytes,
			UsedBytes:    attachRes.UsedBytes,
			IndexHits:           indexHits,
			IndexMisses:         indexMisses,
			ResolverPath:        resolverPath,
			ExpansionByStrategy: expansionByStrategy,
			ExpansionDropped:    expansionDropped,
		},
	}
}
