package diff

import (
	"fmt"
	"sort"
	"strings"
)

// ReferenceInput is the resolver-output shape consumed by AttachReferenced.
// We keep this separate from resolver.Reference to avoid an import cycle
// (resolver imports diff for Bundle definitions).
type ReferenceInput struct {
	Path    string
	Excerpt string
	Symbols []string
}

// ReferencedFileLogEntry is the per-file record emitted to bundle-log.jsonl
// when KIZUNAX_BUNDLE_LOG is set. It is also returned by AttachReferenced
// so the runner can splice in diff_file / untracked_text entries before
// passing to bundlelog.Append.
type ReferencedFileLogEntry struct {
	Path    string   `json:"path"`
	Reason  string   `json:"reason"` // "diff_file" | "untracked_text" | "def_match:<csv>"
	Bytes   int      `json:"bytes"`
	Symbols []string `json:"symbols,omitempty"`
}

// AttachResult summarizes the outcome of AttachReferenced for the runner's
// verbose stderr line and bundlelog entry.
type AttachResult struct {
	Attached int
	Dropped  int
	// UsedBytes is the budget-unit accounting (Σ of cost = len(Excerpt)+80 per
	// kept file, where 80 bytes is the rendered path+fence+symbol-list
	// overhead). It WILL NOT equal Σ Files[i].Bytes, which counts raw excerpt
	// bytes only. Use UsedBytes for budget-vs-cap comparisons, Files[i].Bytes
	// for per-file content size.
	UsedBytes   int
	BudgetBytes int
	Files       []ReferencedFileLogEntry // kept entries only, each with Reason="def_match:..."
}

// AttachReferenced merges enrichment references into the bundle,
// respecting the budget cap. Mutates b in place AND returns an
// AttachResult for verbose logging + bundle-log emission.
// Warnings about dropped/oversized references are appended to b.Warnings.
func AttachReferenced(b *Bundle, refs []ReferenceInput, budgetBytes int) AttachResult {
	res := AttachResult{BudgetBytes: budgetBytes}
	if len(refs) == 0 || budgetBytes <= 0 {
		return res
	}

	// Dedup by Path: if same file appears twice (different symbols),
	// merge the symbol lists.
	merged := map[string]*ReferenceInput{}
	order := []string{} // preserve first-seen order
	for i := range refs {
		r := refs[i]
		if existing, ok := merged[r.Path]; ok {
			existing.Symbols = appendUnique(existing.Symbols, r.Symbols...)
			continue
		}
		cp := r
		merged[r.Path] = &cp
		order = append(order, r.Path)
	}

	// Priority sort: (-len(symbols), len(excerpt), path) — deterministic.
	sortable := make([]*ReferenceInput, 0, len(order))
	for _, p := range order {
		sortable = append(sortable, merged[p])
	}
	sort.SliceStable(sortable, func(i, j int) bool {
		if len(sortable[i].Symbols) != len(sortable[j].Symbols) {
			return len(sortable[i].Symbols) > len(sortable[j].Symbols)
		}
		if len(sortable[i].Excerpt) != len(sortable[j].Excerpt) {
			return len(sortable[i].Excerpt) < len(sortable[j].Excerpt)
		}
		return sortable[i].Path < sortable[j].Path
	})

	// Greedy fit.
	var kept []ReferencedFile
	used := 0
	dropped := 0
	for _, r := range sortable {
		// Account for path/header overhead in the rendered template.
		// Rough estimate: path + 2 fence markers + symbol list = ~80 bytes.
		cost := len(r.Excerpt) + 80
		if used+cost > budgetBytes {
			dropped++
			continue
		}
		used += cost
		kept = append(kept, ReferencedFile{
			Path:    r.Path,
			Excerpt: r.Excerpt,
			Symbols: r.Symbols,
		})
		// Build log entry with deterministic Reason.
		sortedSyms := append([]string(nil), r.Symbols...)
		sort.Strings(sortedSyms)
		res.Files = append(res.Files, ReferencedFileLogEntry{
			Path:    r.Path,
			Reason:  "def_match:" + strings.Join(sortedSyms, ","),
			Bytes:   len(r.Excerpt),
			Symbols: sortedSyms,
		})
	}

	b.ReferencedFiles = kept
	res.Attached = len(kept)
	res.Dropped = dropped
	res.UsedBytes = used
	if dropped > 0 {
		b.Warnings = append(b.Warnings,
			fmt.Sprintf("referenced files dropped: %d of %d (kept %d) due to %d-byte cap",
				dropped, len(sortable), len(kept), budgetBytes))
	}
	return res
}

func appendUnique(have []string, add ...string) []string {
	seen := map[string]bool{}
	for _, s := range have {
		seen[s] = true
	}
	for _, s := range add {
		if !seen[s] {
			have = append(have, s)
			seen[s] = true
		}
	}
	return have
}
