package expand

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
)

// RankerWeights tunes the bundle expansion ranker. Higher = stronger pull.
// Defaults (from DefaultRankerWeights) match spec §7: SymbolMatch
// dominates (per-match ×10), then strategy bonus, then distance/recency.
//
// Library consumers can copy DefaultRankerWeights() and tweak per use
// case — for example, zeroing out Recency to make the ranker
// time-stable, or boosting StrategyTest if test files are the
// highest-signal context for the consumer's prompt.
type RankerWeights struct {
	SymbolMatch     float64 // per-match weight; default 10.0
	Distance        float64 // 1/(d+1) coefficient; default 1.0
	Recency         float64 // mtime / 1e18 coefficient; default 1.0
	StrategyDef     float64 // def_match bonus; default 8.0
	StrategyCaller  float64 // caller bonus; default 5.0
	StrategyTypeDef float64 // type_def bonus; default 4.0
	StrategyTest    float64 // test bonus; default 2.0
}

// DefaultRankerWeights returns the spec §7 defaults.
func DefaultRankerWeights() RankerWeights {
	return RankerWeights{
		SymbolMatch:     10.0,
		Distance:        1.0,
		Recency:         1.0,
		StrategyDef:     8.0,
		StrategyCaller:  5.0,
		StrategyTypeDef: 4.0,
		StrategyTest:    2.0,
	}
}

// Rank computes a score per candidate and returns them sorted by score
// descending. Ties break by File (alphabetical) for determinism.
// Input slice is not modified; a new slice is returned.
func Rank(in []Candidate, w RankerWeights) []Candidate {
	out := make([]Candidate, len(in))
	copy(out, in)
	for i := range out {
		out[i].score = computeScore(out[i], w)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].File < out[j].File
	})
	return out
}

func computeScore(c Candidate, w RankerWeights) float64 {
	s := w.SymbolMatch * float64(c.SymbolMatches)
	s += w.Distance / float64(c.DistanceDirs+1)
	s += w.Recency * (float64(c.Recency) / 1e18)
	switch c.Strategy {
	case "def_match":
		s += w.StrategyDef
	case "caller":
		s += w.StrategyCaller
	case "type_def":
		s += w.StrategyTypeDef
	case "test":
		s += w.StrategyTest
	}
	return s
}

// Per-strategy excerpt cap (bytes). Tests can be denser so they get a
// larger window. Other strategies share the 4 KB default — matches the
// existing diff.AttachReferenced behavior.
const (
	excerptCapDefault = 4 * 1024
	excerptCapTest    = 8 * 1024
	perFileOverhead   = 80 // path + fence markdown
)

// Pack greedily fits ranked candidates into budgetBytes. Excerpts are
// read lazily from workspaceRoot/<candidate.File>. Files that can't be
// read are silently dropped (non-fatal additive). Candidates that don't
// fit land in Dropped[] for telemetry.
//
// Returns AttachResult with Attached sorted same as input rank order,
// BudgetBytes echoed, UsedBytes accumulated, ByStrategy counts among
// Attached only.
func Pack(ranked []Candidate, budgetBytes int, workspaceRoot string) AttachResult {
	res := AttachResult{
		BudgetBytes: budgetBytes,
		ByStrategy:  make(map[string]int),
	}
	for _, c := range ranked {
		cap := excerptCapDefault
		if c.Strategy == "test" {
			cap = excerptCapTest
		}
		excerpt, err := readExcerpt(filepath.Join(workspaceRoot, c.File), c.LineHint, cap)
		if err != nil {
			continue // file unreadable, skip silently
		}
		c.Excerpt = excerpt
		c.Bytes = len(excerpt)
		cost := c.Bytes + perFileOverhead
		if res.UsedBytes+cost > budgetBytes {
			res.Dropped = append(res.Dropped, c)
			continue
		}
		res.Attached = append(res.Attached, c)
		res.UsedBytes += cost
		res.ByStrategy[c.Strategy]++
	}
	return res
}

// readExcerpt reads up to capBytes from path, optionally centered on
// lineHint (≥1). Returns the snippet as a string. Same approach as
// resolver/v2.go::readExcerptAt — preserved for consistency.
func readExcerpt(absPath string, lineHint, capBytes int) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if lineHint <= 0 {
		// Read from start, hard-cap.
		buf := make([]byte, capBytes)
		n, _ := f.Read(buf)
		return string(buf[:n]), nil
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var (
		lineNo   int
		captured []string
		start    = lineHint - 3
		end      = lineHint + 50
	)
	if start < 1 {
		start = 1
	}
	totalBytes := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if lineNo > end {
			break
		}
		line := scanner.Text()
		if totalBytes+len(line)+1 > capBytes {
			break
		}
		captured = append(captured, line)
		totalBytes += len(line) + 1
	}
	out := ""
	for i, l := range captured {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out, nil
}
