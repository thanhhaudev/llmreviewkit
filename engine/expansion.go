package engine

import (
	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/expand"
	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/resolver"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// runExpansion orchestrates strategies A/B/D, ranks the union, packs
// within budget, and returns the AttachResult plus per-strategy
// telemetry. Returns zero AttachResult + nil error when no strategy
// flag is set (callers should skip the wire-up entirely in that case
// to save the ranker allocation).
func (e *Engine) runExpansion(
	diffSyms []symbols.Symbol,
	diffFiles []string,
	directRefs []resolver.Reference,
	idx *index.Index,
) expand.AttachResult {
	// Convert directRefs to def_match candidates so the ranker sees the
	// full picture (def + expansion).
	diffFileSet := make(map[string]bool, len(diffFiles))
	for _, f := range diffFiles {
		diffFileSet[f] = true
	}

	var all []expand.Candidate
	for _, r := range directRefs {
		if diffFileSet[r.File] {
			continue
		}
		all = append(all, expand.Candidate{
			File:          r.File,
			Strategy:      "def_match",
			SymbolMatches: 1,
		})
	}

	if e.cfg.ExpandCallers && idx != nil {
		all = append(all, expand.Callers(diffSyms, idx, diffFileSet)...)
	}
	if e.cfg.ExpandTypeDefs && idx != nil {
		all = append(all, expand.TypeDefs(diffSyms, idx, diffFileSet)...)
	}
	if e.cfg.ExpandTests {
		all = append(all, expand.Tests(diffFiles, e.cfg.WorkspaceRoot)...)
	}

	// Dedup by File: sum SymbolMatches, keep strongest Strategy.
	all = dedupByFile(all)

	weights := expand.DefaultRankerWeights()
	if e.cfg.RankerWeights != nil {
		weights = *e.cfg.RankerWeights
	}
	ranked := expand.Rank(all, weights)
	return expand.Pack(ranked, e.cfg.EnrichBudget, e.cfg.WorkspaceRoot)
}

// dedupByFile collapses candidates pointing at the same file. The
// strongest strategy (in the priority def_match > caller > type_def >
// test) wins; SymbolMatches sums; LineHint is the lowest non-zero seen.
func dedupByFile(in []expand.Candidate) []expand.Candidate {
	priority := map[string]int{"def_match": 4, "caller": 3, "type_def": 2, "test": 1}
	byFile := make(map[string]*expand.Candidate)
	for i := range in {
		c := in[i]
		existing, ok := byFile[c.File]
		if !ok {
			cp := c
			byFile[c.File] = &cp
			continue
		}
		existing.SymbolMatches += c.SymbolMatches
		if priority[c.Strategy] > priority[existing.Strategy] {
			existing.Strategy = c.Strategy
		}
		if c.LineHint > 0 && (existing.LineHint == 0 || c.LineHint < existing.LineHint) {
			existing.LineHint = c.LineHint
		}
	}
	out := make([]expand.Candidate, 0, len(byFile))
	for _, c := range byFile {
		out = append(out, *c)
	}
	return out
}

// expansionEnabled is a tiny helper to tighten the call site in Review.
func (e *Engine) expansionEnabled() bool {
	return e.cfg.ExpandCallers || e.cfg.ExpandTypeDefs || e.cfg.ExpandTests
}

// attachResultToInputs converts expand.AttachResult to the slice of
// diff.ReferenceInput the existing diff.AttachReferenced consumer expects.
// Kept in this file because it's part of the expansion wiring.
func attachResultToInputs(ar expand.AttachResult) []diff.ReferenceInput {
	out := make([]diff.ReferenceInput, 0, len(ar.Attached))
	for _, c := range ar.Attached {
		out = append(out, diff.ReferenceInput{
			Path:    c.File,
			Excerpt: c.Excerpt,
		})
	}
	return out
}
