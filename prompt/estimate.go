package prompt

// EstimateInputTokens returns a rough token count for the assembled prompt.
// Uses the chars/4 heuristic with a 1.2x safety factor — code is denser than
// natural language because of identifiers and symbols, so we over-estimate
// rather than under-estimate. The caller treats the result as a conservative
// upper bound: if EstimateInputTokens(...) > ModelMaxInputTokens(...), the
// real input definitely exceeds the budget; if it's under, the real input
// usually fits (occasionally with a few percent of margin to spare).
func EstimateInputTokens(systemPrompt, userPrompt string) int {
	chars := len(systemPrompt) + len(userPrompt)
	if chars == 0 {
		return 0
	}
	return int(float64(chars) / 4.0 * 1.2)
}
