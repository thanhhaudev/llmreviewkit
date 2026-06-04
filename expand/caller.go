package expand

import (
	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// Callers returns candidate files that reference any diff symbol in
// idx.Refs. Files already in the diff (per diffFiles) are excluded —
// they're already attached as primary diff bundle.
//
// Multiple diff symbols pointing at the same caller file are merged:
// SymbolMatches sums, LineHint is the lowest line seen.
//
// Returns nil if idx is nil or unhealthy. Non-fatal additive design.
func Callers(diffSyms []symbols.Symbol, idx *index.Index, diffFiles map[string]bool) []Candidate {
	if idx == nil || !idx.Healthy() {
		return nil
	}
	byFile := make(map[string]*Candidate)
	for _, s := range diffSyms {
		if s.Name == "" {
			continue
		}
		for _, ref := range idx.LookupRefs(s.Name, s.Pkg) {
			if diffFiles[ref.File] {
				continue
			}
			c, ok := byFile[ref.File]
			if !ok {
				c = &Candidate{
					File:          ref.File,
					Strategy:      "caller",
					SymbolMatches: 0,
					LineHint:      ref.Line,
				}
				byFile[ref.File] = c
			}
			c.SymbolMatches++
			if ref.Line > 0 && (c.LineHint == 0 || ref.Line < c.LineHint) {
				c.LineHint = ref.Line
			}
		}
	}
	out := make([]Candidate, 0, len(byFile))
	for _, c := range byFile {
		out = append(out, *c)
	}
	return out
}
