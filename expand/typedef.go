package expand

import (
	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// TypeDefs returns candidate files defining types referenced in the diff.
// Only symbols with Kind=SymTypeRef are considered; other kinds (calls,
// defs, imports) are ignored. Files already in the diff are excluded.
//
// Multiple diff symbols pointing at the same definition file merge,
// summing SymbolMatches.
//
// Returns nil if idx is nil or unhealthy.
func TypeDefs(diffSyms []symbols.Symbol, idx *index.Index, diffFiles map[string]bool) []Candidate {
	if idx == nil || !idx.Healthy() {
		return nil
	}
	byFile := make(map[string]*Candidate)
	for _, s := range diffSyms {
		if s.Kind != symbols.SymTypeRef || s.Name == "" {
			continue
		}
		for _, def := range idx.LookupDefs(s.Name, s.Pkg) {
			if diffFiles[def.File] {
				continue
			}
			c, ok := byFile[def.File]
			if !ok {
				c = &Candidate{
					File:          def.File,
					Strategy:      "type_def",
					SymbolMatches: 0,
					LineHint:      def.Line,
				}
				byFile[def.File] = c
			}
			c.SymbolMatches++
			if def.Line > 0 && (c.LineHint == 0 || def.Line < c.LineHint) {
				c.LineHint = def.Line
			}
		}
	}
	out := make([]Candidate, 0, len(byFile))
	for _, c := range byFile {
		out = append(out, *c)
	}
	return out
}
