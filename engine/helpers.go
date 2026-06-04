package engine

import (
	"path/filepath"
	"time"

	"github.com/thanhhaudev/llmreviewkit/bundlelog"
	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/resolver"
	"github.com/thanhhaudev/llmreviewkit/statedir"
)

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
