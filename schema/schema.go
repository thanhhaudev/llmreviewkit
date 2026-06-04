package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type ReviewResult struct {
	Verdict   string    `json:"verdict"`
	Summary   string    `json:"summary"`
	Findings  []Finding `json:"findings"`
	NextSteps []string  `json:"next_steps"`

	// TLDR is populated by the runner (helper call), NOT the main LLM JSON.
	// json:"-" ensures the LLM cannot inject it.
	TLDR string `json:"-"`
}

type Finding struct {
	Severity       string  `json:"severity"`
	Title          string  `json:"title"`
	Body           string  `json:"body"`
	File           string  `json:"file"`
	LineStart      int     `json:"line_start"`
	LineEnd        int     `json:"line_end"`
	Confidence     float64 `json:"confidence"`
	Recommendation string  `json:"recommendation"`
}

type ParseError struct {
	Raw   string
	Cause error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("review JSON parse failed: %v", e.Cause)
}

var (
	validVerdict    = map[string]bool{"approve": true, "needs-attention": true}
	validSeverities = map[string]bool{"critical": true, "high": true, "medium": true, "low": true}
	fenceStartRE    = regexp.MustCompile("(?s)```(?:json)?\\s*")
	fenceEndRE      = regexp.MustCompile("```\\s*$")
)

func Parse(raw string) (ReviewResult, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = fenceStartRE.ReplaceAllString(cleaned, "")
	cleaned = fenceEndRE.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)

	if first := strings.Index(cleaned, "{"); first > 0 {
		cleaned = cleaned[first:]
	}
	if last := strings.LastIndex(cleaned, "}"); last >= 0 && last < len(cleaned)-1 {
		cleaned = cleaned[:last+1]
	}

	var r ReviewResult
	if err := json.Unmarshal([]byte(cleaned), &r); err != nil {
		return ReviewResult{}, &ParseError{Raw: raw, Cause: err}
	}

	if !validVerdict[r.Verdict] {
		return r, &ParseError{Raw: raw, Cause: fmt.Errorf("invalid verdict %q", r.Verdict)}
	}
	for i, f := range r.Findings {
		if !validSeverities[f.Severity] {
			return r, &ParseError{Raw: raw, Cause: fmt.Errorf("finding[%d]: invalid severity %q", i, f.Severity)}
		}
		if f.Confidence < 0 || f.Confidence > 1 {
			return r, &ParseError{Raw: raw, Cause: fmt.Errorf("finding[%d]: confidence %.2f out of [0,1]", i, f.Confidence)}
		}
	}

	return r, nil
}

// LoadSchemaJSON returns the review-output JSON schema as raw text.
// Lookup order:
//  1. <pluginRoot>/schemas/review-output.schema.json (disk override).
//  2. Embedded default (library consumers without a plugin layout, or
//     consumers who set PromptRoot to override templates but don't
//     ship their own schema).
//
// Disk read failures (missing file, permission denied) fall through to
// embedded. Only an unreachable embedded blob returns an error — which
// in practice can't happen because //go:embed is compile-time.
func LoadSchemaJSON(pluginRoot string) (string, error) {
	if pluginRoot != "" {
		path := filepath.Join(pluginRoot, "schemas", "review-output.schema.json")
		if data, err := os.ReadFile(path); err == nil {
			return string(data), nil
		}
	}
	if got, ok := embeddedSchemaJSON(); ok {
		return got, nil
	}
	return "", fmt.Errorf("embedded schema unavailable")
}
