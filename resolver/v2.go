package resolver

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// ResolveStatsV2 extends ResolveStats with index-specific metrics.
// Wire-compatible via ToV1() for v0.12.4 telemetry consumers.
type ResolveStatsV2 struct {
	Refs                 []Reference
	ExtractedCount       int
	FilteredCount        int
	ResolvedCount        int
	IndexHits            int
	IndexMisses          int
	Candidates           []Reference // populated by v0.13.1 bundle expansion (empty here)
	CandidatesByStrategy map[string]int
	ResolverPath         string // "v2" | "regex_fallback"
}

// ToV1 converts to the v0.12.4 ResolveStats shape so runner can keep a
// single code path for telemetry emission.
func (s ResolveStatsV2) ToV1() ResolveStats {
	return ResolveStats{
		Refs:           s.Refs,
		ExtractedCount: s.ExtractedCount,
		FilteredCount:  s.FilteredCount,
		ResolvedCount:  s.ResolvedCount,
	}
}

// FindReferencesV2 resolves diff symbols against the workspace AST index.
// Returns ResolveStatsV2 with IndexHits/Misses telemetry. Stdlib filter
// is applied first (same as v1). On match, an excerpt is loaded from
// the def file for prompt inclusion.
func FindReferencesV2(syms []symbols.Symbol, ws string, idx *index.Index,
	diffFiles []string, maxRefsPerSymbol int) (ResolveStatsV2, error) {

	stats := ResolveStatsV2{
		ExtractedCount:       len(syms),
		ResolverPath:         "v2",
		CandidatesByStrategy: map[string]int{},
	}

	// Reuse v0.12.4 stdlib filter.
	work := make([]symbols.Symbol, 0, len(syms))
	for _, s := range syms {
		if IsStdlibSymbol(s) {
			continue
		}
		if s.Name == "" {
			continue
		}
		work = append(work, s)
	}
	stats.FilteredCount = len(work)
	if len(work) == 0 {
		return stats, nil
	}

	matches := map[string][]Reference{}

	for _, s := range work {
		defs := idx.LookupDefs(s.Name, s.Pkg)
		if len(defs) == 0 {
			stats.IndexMisses++
			continue
		}
		stats.IndexHits++

		refs := locationsToRefs(defs, s, ws, maxRefsPerSymbol)
		if len(refs) > 0 {
			matches[s.Name] = refs
		}
	}

	// Flatten
	var out []Reference
	for _, m := range matches {
		out = append(out, m...)
	}
	stats.Refs = out
	stats.ResolvedCount = len(matches)
	return stats, nil
}

// locationsToRefs reads excerpts from disk for each Location and converts
// to Reference (resolver-internal). Caps at maxRefsPerSymbol.
func locationsToRefs(locs []index.Location, parent symbols.Symbol, ws string,
	maxRefs int) []Reference {
	if maxRefs <= 0 {
		maxRefs = 5
	}
	if len(locs) > maxRefs {
		locs = locs[:maxRefs]
	}
	out := make([]Reference, 0, len(locs))
	for _, loc := range locs {
		excerpt := readExcerptAt(filepath.Join(ws, loc.File), loc.Line, 4*1024)
		out = append(out, Reference{
			Symbol:  parent,
			File:    loc.File,
			Excerpt: excerpt,
		})
	}
	return out
}

// readExcerptAt reads up to maxBytes of content centered around line.
// Returns "" on read error. Mimics resolver.buildExcerpt semantics.
func readExcerptAt(absPath string, line, maxBytes int) string {
	f, err := os.Open(absPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var (
		lineNo   int
		captured []string
		start    = line - 3
		end      = line + 50
	)
	if start < 1 {
		start = 1
	}
	for scanner.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if lineNo > end {
			break
		}
		captured = append(captured, scanner.Text())
	}
	out := strings.Join(captured, "\n")
	if len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out
}
