package prompt

import "testing"

func TestEstimateInputTokens_EmptyInputs(t *testing.T) {
	if got := EstimateInputTokens("", ""); got != 0 {
		t.Errorf("empty inputs = %d, want 0", got)
	}
}

func TestEstimateInputTokens_RoughChars4(t *testing.T) {
	// 400 chars total → ~120 tokens (400/4 * 1.2 safety factor)
	system := makeASCII(200)
	user := makeASCII(200)
	got := EstimateInputTokens(system, user)
	if got < 110 || got > 130 {
		t.Errorf("400-char input estimate = %d, want ~120 (range 110-130)", got)
	}
}

func TestEstimateInputTokens_ConservativeOverestimate(t *testing.T) {
	prompt := makeASCII(4000)
	got := EstimateInputTokens("", prompt)
	if got < 1000 || got > 1500 {
		t.Errorf("4000-char prompt estimate = %d, want 1000-1500", got)
	}
}

func makeASCII(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
