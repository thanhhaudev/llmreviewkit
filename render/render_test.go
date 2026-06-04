package render

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/prompt"
	"github.com/thanhhaudev/llmreviewkit/schema"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func loadInput(t *testing.T, name string) schema.ReviewResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	var r schema.ReviewResult
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return r
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nrun with -update to create", path, err)
	}
	// Normalize line endings — git on Windows checks out golden files with
	// CRLF by default, but the renderer always emits LF. Strip CR so the
	// comparison is platform-agnostic.
	wantStr := strings.ReplaceAll(string(want), "\r\n", "\n")
	if wantStr != got {
		t.Errorf("output != %s\n--- want ---\n%s\n--- got ---\n%s", path, wantStr, got)
	}
}

func TestRenderReview_NeedsAttention_Standard(t *testing.T) {
	r := loadInput(t, "review-needs.json")
	bundle := diff.Bundle{TargetLabel: "commit abc1234"}
	got := RenderReview(r, bundle, 4008, prompt.ModeStandard)
	checkGolden(t, "review-needs.standard.golden.md", got)
}

func TestRenderReview_Approve_Adversarial(t *testing.T) {
	r := loadInput(t, "review-approve.json")
	bundle := diff.Bundle{TargetLabel: "working tree"}
	got := RenderReview(r, bundle, 250, prompt.ModeAdversarial)
	checkGolden(t, "review-approve.adversarial.golden.md", got)
}

func TestRenderReview_WithWarnings(t *testing.T) {
	r := loadInput(t, "review-approve.json")
	bundle := diff.Bundle{
		TargetLabel: "working tree",
		Warnings:    []string{"truncated 2 files >64KB", "skipped binary file: image.png"},
	}
	got := RenderReview(r, bundle, 100, prompt.ModeStandard)
	if !contains(got, "Warnings") {
		t.Error("expected Warnings section in output")
	}
	if !contains(got, "truncated 2 files") {
		t.Error("expected warning text in output")
	}
}

func TestRenderReview_FindingTitleWithPipeDoesNotBreakTable(t *testing.T) {
	result := schema.ReviewResult{
		Verdict: "needs-attention",
		Summary: "one finding with a pipe",
		Findings: []schema.Finding{
			{
				Severity:       "high",
				Title:          "uses unsafe pipe | operator",
				File:           "internal/foo|bar.go",
				LineStart:      10,
				LineEnd:        12,
				Body:           "Body with\nembedded\nnewlines and a | pipe",
				Recommendation: "Avoid raw pipes in identifiers",
			},
		},
		NextSteps: []string{"refactor"},
	}
	bundle := diff.Bundle{TargetLabel: "working tree"}
	out := RenderReview(result, bundle, 0, prompt.ModeStandard)
	// The unescaped title must NOT appear as a raw fragment that would break
	// the table by inserting a phantom column.
	if strings.Contains(out, "uses unsafe pipe | operator |") {
		t.Errorf("unescaped pipe in title appears in raw form (would break table):\n%s", out)
	}
	// Escaped form must appear in the table row.
	if !strings.Contains(out, `uses unsafe pipe \| operator`) {
		t.Errorf("expected escaped title in table row; got:\n%s", out)
	}
	// File path with pipe must be escaped inside the location cell.
	if !strings.Contains(out, `internal/foo\|bar.go:10-12`) {
		t.Errorf("expected escaped file path in location cell; got:\n%s", out)
	}
	// Locate the table-row line for the finding and assert it has exactly
	// 5 unescaped pipes (table borders: leading | + 4 separators + trailing |).
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "| 1 ") {
			continue
		}
		rawPipes := 0
		for i := 0; i < len(line); i++ {
			if line[i] == '|' && (i == 0 || line[i-1] != '\\') {
				rawPipes++
			}
		}
		if rawPipes != 5 {
			t.Errorf("table row has %d raw pipes, want 5 (would break table): %q", rawPipes, line)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRenderReview_TLDRReplacesSummary(t *testing.T) {
	result := schema.ReviewResult{
		Verdict: "needs-attention",
		Summary: "Model's own verbose self-summary that should NOT appear.",
		TLDR:    "Helper TL;DR: 3 issues, 1 critical race condition. Address before merge.",
		Findings: []schema.Finding{
			{Severity: "critical", Title: "Race", File: "a.go", LineStart: 1, LineEnd: 1, Confidence: 0.9},
		},
	}
	bundle := diff.Bundle{TargetLabel: "test"}
	out := RenderReview(result, bundle, 0, prompt.ModeStandard)

	if !strings.Contains(out, "Helper TL;DR") {
		t.Fatalf("expected TLDR text in output: %s", out)
	}
	if strings.Contains(out, "verbose self-summary") {
		t.Fatalf("Summary must NOT render when TLDR is set: %s", out)
	}
}

func TestRenderReview_SummaryRenders_WhenTLDREmpty(t *testing.T) {
	result := schema.ReviewResult{
		Verdict: "approve",
		Summary: "Original summary text.",
	}
	bundle := diff.Bundle{TargetLabel: "test"}
	out := RenderReview(result, bundle, 0, prompt.ModeStandard)
	if !strings.Contains(out, "Original summary text.") {
		t.Fatalf("expected Summary to render when TLDR empty: %s", out)
	}
}
