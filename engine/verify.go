package engine

import (
	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/schema"
)

// FindingVerification classifies one finding by whether its cited
// file:line range actually intersects a +/- hunk in the diff bundle.
// Use as a post-process to surface speculative or hallucinated findings.
type FindingVerification struct {
	// InHunk is true when the finding's [LineStart, LineEnd] range
	// overlaps at least one hunk for the cited file.
	InHunk bool

	// FileInDiff is true when the cited file appears in the diff at
	// all (even if the line range is unchanged context).
	FileInDiff bool

	// Reason is a human-readable explanation when verification fails.
	// Empty string when InHunk == true (verification passes).
	Reason string
}

// Verified is a convenience: true iff the cited file is in the diff
// AND the cited line range overlaps a hunk. Equivalent to InHunk.
func (v FindingVerification) Verified() bool { return v.InHunk }

// VerifyFindings classifies each finding by whether its claimed file:line
// range falls within an actual diff hunk for the cited file.
//
// Three outcomes:
//  1. file in diff AND line range overlaps a hunk → InHunk=true,
//     FileInDiff=true, Reason="" (verified)
//  2. file in diff, line range outside all hunks → InHunk=false,
//     FileInDiff=true, Reason explains context-line drift
//  3. file NOT in diff → InHunk=false, FileInDiff=false, Reason
//     flags as likely hallucination / wrong file path
//
// Callers decide policy: drop unverified findings, downgrade their
// severity, annotate with a warning, etc. Llmreviewkit itself does
// not modify the findings — it only reports the classification.
//
// Note: LLMs commonly drift ±5-10 lines on long diffs, so case 2 is
// not always wrong; the finding may describe a real bug at an
// adjacent line. Case 3 is much more suspicious and usually warrants
// dropping.
//
// Returns a slice the same length as findings, in the same order.
func VerifyFindings(bundle diff.Bundle, findings []schema.Finding) []FindingVerification {
	hunks := diff.HunksByFile(bundle.Diff)
	out := make([]FindingVerification, len(findings))
	for i, f := range findings {
		if _, fileOK := hunks[f.File]; !fileOK {
			out[i] = FindingVerification{
				FileInDiff: false,
				InHunk:     false,
				Reason:     "file not present in diff (likely hallucination or wrong file path)",
			}
			continue
		}
		if diff.FindingInHunk(hunks, f.File, f.LineStart, f.LineEnd) {
			out[i] = FindingVerification{FileInDiff: true, InHunk: true}
		} else {
			out[i] = FindingVerification{
				FileInDiff: true,
				InHunk:     false,
				Reason:     "file is in diff but cited line range falls in unchanged context (LLM line-number drift common; bug may be nearby)",
			}
		}
	}
	return out
}
