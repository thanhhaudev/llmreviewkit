// Package expand provides bundle expansion strategies for the
// llmreviewkit review pipeline. Strategies pull additional workspace
// files into the prompt context beyond direct symbol-definition matches.
//
// Strategies (gated by engine.Config flags):
//   - Callers   (Strategy A) — files that reference any diff symbol via the index
//   - TypeDefs  (Strategy B) — files defining types referenced in the diff
//   - Tests     (Strategy D) — files matching test-naming patterns adjacent to diff files
//
// Ranker scores candidates and packs them into a byte budget. Failures
// (missing index, file read errors, budget exhaustion) are non-fatal
// and additive — the core review pipeline always proceeds.
package expand

// Candidate is one file the expansion pipeline considers attaching to
// the review prompt. Strategies emit Candidates; the ranker scores and
// orders them; pack reads excerpts and fits them into a budget.
type Candidate struct {
	// File is repo-relative.
	File string

	// Strategy is one of "def_match" (direct resolver hit), "caller",
	// "type_def", "test". Drives the ranker's strategy-weight bonus.
	Strategy string

	// SymbolMatches is the count of diff symbols pointing at this file
	// (callers strategy) or the count of type references resolved to
	// this file (type_def). Higher = stronger signal.
	SymbolMatches int

	// DistanceDirs is the directory-tree hop count from the nearest diff
	// file. 0 = same dir, 1 = parent or sibling, etc. Used in ranker as
	// 1 / (DistanceDirs + 1) to reward proximity.
	DistanceDirs int

	// Recency is the file's mtime in unix nanoseconds. Reward recently
	// modified files (likely related to current work).
	Recency int64

	// LineHint is an optional excerpt center (line number). 0 means
	// "no hint — start from line 1". For callers, set to the ref line.
	LineHint int

	// Excerpt is populated lazily by Pack when the candidate is selected.
	// Empty until then.
	Excerpt string

	// Bytes is len(Excerpt) once populated. Used in budget accounting.
	Bytes int

	// score is the ranker's computed score; unexported, set by Rank.
	score float64
}

// AttachResult is the output of Pack — the subset of ranked candidates
// that fit within the budget, plus diagnostic counters.
type AttachResult struct {
	// Attached is the candidates selected, sorted by score desc.
	Attached []Candidate

	// Dropped is the remaining ranked candidates that did not fit.
	// Preserved for telemetry; not attached to the prompt.
	Dropped []Candidate

	// BudgetBytes is the cap that Pack honored.
	BudgetBytes int

	// UsedBytes is sum of Attached.Bytes + per-file overhead.
	UsedBytes int

	// ByStrategy maps strategy name to count among Attached.
	// Useful for tuning weights + reporting per-call telemetry.
	ByStrategy map[string]int
}
