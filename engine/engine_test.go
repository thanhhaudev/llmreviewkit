package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/engine"
	"github.com/thanhhaudev/llmreviewkit/prompt"
	"github.com/thanhhaudev/llmreviewkit/provider/mock"
)

// cannedReview is a minimal valid ReviewResult JSON. Field names and valid
// values match pkg/schema/schema.go:
//   - verdict must be "approve" or "needs-attention"
//   - severity must be "critical" | "high" | "medium" | "low"
//   - confidence must be in [0, 1]
//   - body is the correct field name (not "detail")
//   - line_start / line_end (not "line")
const cannedReview = `{"verdict":"approve","summary":"ok","findings":[{"severity":"medium","file":"x.go","line_start":1,"line_end":1,"title":"demo","body":"d","confidence":0.5}],"next_steps":[]}`

func TestEngine_BasicReview(t *testing.T) {
	prov := mock.New(cannedReview, 100, 200)
	cfg := engine.Config{
		Provider:      prov,
		WorkspaceRoot: t.TempDir(),

	}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	bundle := diff.Bundle{Diff: "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n+demo\n"}
	res, err := eng.Review(context.Background(), bundle, engine.ReviewOptions{Mode: prompt.ModeStandard})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(res.Review.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(res.Review.Findings))
	}
	if res.TotalTokens != 300 {
		t.Fatalf("want TotalTokens=300, got %d", res.TotalTokens)
	}
	if len(prov.Calls) != 1 {
		t.Fatalf("expected exactly 1 Chat call, got %d", len(prov.Calls))
	}
}

func TestEngine_RequiredFieldsValidated(t *testing.T) {
	// Missing Provider — must error regardless of WorkspaceRoot.
	if _, err := engine.New(engine.Config{WorkspaceRoot: "/tmp"}); err == nil {
		t.Fatal("expected error for missing Provider")
	}
	// Empty WorkspaceRoot is now allowed; enrichment is simply skipped
	// when WorkspaceRoot is not set. Engine.New must succeed.
	if _, err := engine.New(engine.Config{Provider: mock.New("", 0, 0)}); err != nil {
		t.Fatalf("expected New to succeed with empty WorkspaceRoot, got: %v", err)
	}
}

func TestEngine_EnrichmentDisabledByEmptyBundle(t *testing.T) {
	prov := mock.New(cannedReview, 10, 20)
	cfg := engine.Config{
		Provider:      prov,
		WorkspaceRoot: t.TempDir(),

	}
	eng, _ := engine.New(cfg)
	res, err := eng.Review(context.Background(), diff.Bundle{}, engine.ReviewOptions{Mode: prompt.ModeStandard})
	if err != nil {
		t.Fatalf("Review with empty bundle: %v", err)
	}
	if res.Stats.ExtractedCount != 0 {
		t.Fatalf("expected no symbols extracted from empty bundle, got %d", res.Stats.ExtractedCount)
	}
	if res.Stats.ResolverPath != "v1" {
		t.Fatalf("expected v1 default, got %s", res.Stats.ResolverPath)
	}
}

func TestEngine_SyncIndex(t *testing.T) {
	ws := t.TempDir()
	// Drop a tiny Go file so the index has at least one def.
	if err := os.WriteFile(filepath.Join(ws, "a.go"), []byte("package x\nfunc Demo() {}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	prov := mock.New(cannedReview, 10, 20)
	stateDir := filepath.Join(t.TempDir(), "state")
	cfg := engine.Config{
		Provider:      prov,
		WorkspaceRoot: ws,
		StateDir:      stateDir,

		UseIndex:      true,
	}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := eng.SyncIndex(); err != nil {
		t.Fatalf("SyncIndex: %v", err)
	}
	idxJSON := filepath.Join(eng.StateWorkspaceRoot(), "index", "index.json")
	if _, err := os.Stat(idxJSON); err != nil {
		t.Fatalf("index.json should exist after SyncIndex: %v", err)
	}
}

func TestEngine_BundleLogSink(t *testing.T) {
	var buf bytes.Buffer
	prov := mock.New(cannedReview, 10, 20)
	ws := t.TempDir()
	// Need a real workspace file so the enrichment branch runs and emits a log entry.
	if err := os.WriteFile(filepath.Join(ws, "a.go"), []byte("package x\nfunc Demo() {}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	bundle := diff.Bundle{Diff: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n+func Other() {}\n"}

	cfg := engine.Config{
		Provider:      prov,
		WorkspaceRoot: ws,

		BundleLogSink: &buf,
	}
	eng, _ := engine.New(cfg)
	if _, err := eng.Review(context.Background(), bundle, engine.ReviewOptions{Mode: prompt.ModeStandard}); err != nil {
		t.Fatalf("Review: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline in jsonl, got %q", out)
	}
	// Should parse as JSON object — take the LAST line to ignore any
	// [verbose] preamble written by logf.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	var entry map[string]any
	if err := json.Unmarshal([]byte(last), &entry); err != nil {
		t.Fatalf("bundle log line should be valid JSON: %v\nraw last line: %s", err, last)
	}
	if _, ok := entry["ws"]; !ok {
		t.Fatalf("expected ws field in bundle log entry; got %v", entry)
	}
}

func TestEngine_BudgetAutoBumpWhenExpansionEnabled(t *testing.T) {
	prov := mock.New(cannedReview, 10, 20)
	eng, err := engine.New(engine.Config{
		Provider:      prov,
		WorkspaceRoot: t.TempDir(),
		ExpandCallers: true,
		// EnrichBudget left at zero
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := eng.EnrichBudget(); got != 96*1024 {
		t.Fatalf("auto-bump: want 96KB when ExpandCallers=true and budget unset, got %d", got)
	}
}

func TestEngine_BudgetNotBumpedWhenNoExpansion(t *testing.T) {
	prov := mock.New(cannedReview, 10, 20)
	eng, _ := engine.New(engine.Config{
		Provider:      prov,
		WorkspaceRoot: t.TempDir(),
	})
	if got := eng.EnrichBudget(); got != 32*1024 {
		t.Fatalf("default budget should be 32KB when no expansion, got %d", got)
	}
}

func TestEngine_BudgetExplicitOverridesAutoBump(t *testing.T) {
	prov := mock.New(cannedReview, 10, 20)
	eng, _ := engine.New(engine.Config{
		Provider:      prov,
		WorkspaceRoot: t.TempDir(),
		ExpandCallers: true,
		EnrichBudget:  16 * 1024, // explicit override
	})
	if got := eng.EnrichBudget(); got != 16*1024 {
		t.Fatalf("explicit budget should win over auto-bump, got %d", got)
	}
}

func TestEngine_CustomPromptRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o755); err != nil {
		t.Fatalf("mkdir schemas: %v", err)
	}

	// Custom prompt template — includes all interpolation tokens so
	// prompt.Build succeeds. Tokens: TARGET_LABEL, SCHEMA_INLINE,
	// REVIEW_INPUT, REFERENCED_FILES, USER_FOCUS (optional but safe to include).
	custom := "CUSTOM_HEADER\n\nReview target: {{TARGET_LABEL}}\nSchema:\n{{SCHEMA_INLINE}}\n\nDiff:\n{{REVIEW_INPUT}}\n{{REFERENCED_FILES}}\n{{USER_FOCUS}}\n"
	if err := os.WriteFile(filepath.Join(dir, "prompts", "review.md"), []byte(custom), 0o644); err != nil {
		t.Fatalf("seed review.md: %v", err)
	}

	// Note: no need to seed schemas/ — LoadSchemaJSON falls back to the
	// embedded schema when the disk lookup fails, so callers can override
	// just the prompts without shipping their own schema.

	prov := mock.New(cannedReview, 10, 20)
	cfg := engine.Config{
		Provider:      prov,
		WorkspaceRoot: t.TempDir(),
		PromptRoot:    dir,
	}
	eng, _ := engine.New(cfg)
	if _, err := eng.Review(context.Background(), diff.Bundle{Diff: "marker-diff"}, engine.ReviewOptions{Mode: prompt.ModeStandard}); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(prov.Calls) == 0 {
		t.Fatal("expected at least one Chat call")
	}
	userMsg := prov.Calls[0].Messages[0].Content
	if !strings.Contains(userMsg, "CUSTOM_HEADER") {
		preview := userMsg
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Fatalf("expected custom prompt to be used; got user message starting with %q", preview)
	}
}
