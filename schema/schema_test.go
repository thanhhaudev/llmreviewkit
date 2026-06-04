package schema

import (
	"strings"
	"testing"
)

func TestParse_PlainJSON(t *testing.T) {
	raw := `{"verdict":"approve","summary":"clean","findings":[],"next_steps":[]}`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Verdict != "approve" {
		t.Errorf("verdict = %q, want approve", r.Verdict)
	}
}

func TestParse_StripCodeFence(t *testing.T) {
	raw := "```json\n" +
		`{"verdict":"needs-attention","summary":"x","findings":[],"next_steps":[]}` +
		"\n```"
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Verdict != "needs-attention" {
		t.Errorf("verdict = %q", r.Verdict)
	}
}

func TestParse_PoseAroundJSON(t *testing.T) {
	raw := `Here is the review:
{"verdict":"approve","summary":"ok","findings":[],"next_steps":[]}
Hope this helps!`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Verdict != "approve" {
		t.Errorf("verdict = %q", r.Verdict)
	}
}

func TestParse_InvalidVerdict(t *testing.T) {
	raw := `{"verdict":"maybe","summary":"x","findings":[],"next_steps":[]}`
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected ParseError for invalid verdict")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T", err)
	}
	if !strings.Contains(pe.Cause.Error(), "verdict") {
		t.Errorf("error should mention verdict: %v", pe.Cause)
	}
}

func TestParse_InvalidSeverity(t *testing.T) {
	raw := `{"verdict":"approve","summary":"x","findings":[
		{"severity":"weird","title":"t","body":"b","file":"f","line_start":1,"line_end":2,"confidence":0.5,"recommendation":""}
	],"next_steps":[]}`
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected ParseError for invalid severity")
	}
}

func TestParse_ConfidenceOutOfRange(t *testing.T) {
	raw := `{"verdict":"approve","summary":"x","findings":[
		{"severity":"low","title":"t","body":"b","file":"f","line_start":1,"line_end":2,"confidence":1.5,"recommendation":""}
	],"next_steps":[]}`
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected ParseError for confidence>1")
	}
}

func TestParse_MalformedJSON(t *testing.T) {
	raw := `{"verdict":"approve",` // unterminated
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if _, ok := err.(*ParseError); !ok {
		t.Fatalf("expected *ParseError, got %T", err)
	}
}

func TestParse_FullExample(t *testing.T) {
	raw := `{
		"verdict": "needs-attention",
		"summary": "Two bugs found.",
		"findings": [
			{
				"severity": "critical",
				"title": "Race condition",
				"body": "Mutex was removed.",
				"file": "auth.go",
				"line_start": 35,
				"line_end": 37,
				"confidence": 0.95,
				"recommendation": "Restore lock."
			},
			{
				"severity": "high",
				"title": "SQL injection",
				"body": "String concat in WHERE clause.",
				"file": "auth.go",
				"line_start": 50,
				"line_end": 52,
				"confidence": 1.0,
				"recommendation": "Use parameterized query."
			}
		],
		"next_steps": ["Restore lock", "Fix SQL"]
	}`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Findings) != 2 {
		t.Fatalf("findings count = %d, want 2", len(r.Findings))
	}
	if r.Findings[0].Severity != "critical" {
		t.Errorf("finding[0].severity = %q", r.Findings[0].Severity)
	}
	if r.Findings[1].Confidence != 1.0 {
		t.Errorf("finding[1].confidence = %f", r.Findings[1].Confidence)
	}
	if len(r.NextSteps) != 2 {
		t.Errorf("next_steps count = %d", len(r.NextSteps))
	}
}

func TestParse_TLDR_NotPopulatedByLLM(t *testing.T) {
	raw := `{
		"verdict": "approve",
		"summary": "model summary",
		"findings": [],
		"next_steps": []
	}`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.TLDR != "" {
		t.Fatalf("TLDR must default to empty (runner-populated, not LLM), got %q", r.TLDR)
	}
}

func TestParse_TLDR_IgnoresLLMField(t *testing.T) {
	// Even if a malicious/confused LLM stuffs a tldr field, it must NOT be
	// deserialized — the json:"-" tag is the guarantee.
	raw := `{
		"verdict": "approve",
		"summary": "s",
		"tldr": "should be ignored",
		"findings": [],
		"next_steps": []
	}`
	r, _ := Parse(raw)
	if r.TLDR != "" {
		t.Fatalf("LLM must not populate TLDR; got %q", r.TLDR)
	}
}

func TestLoadSchemaJSON_EmbeddedFallback(t *testing.T) {
	s, err := LoadSchemaJSON("")
	if err != nil {
		t.Fatalf("LoadSchemaJSON(\"\") should use embedded: %v", err)
	}
	if s == "" {
		t.Fatal("expected non-empty schema string")
	}
	if !strings.Contains(s, `"verdict"`) {
		t.Fatalf("schema doesn't look like our review schema: %q", s[:min(200, len(s))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
