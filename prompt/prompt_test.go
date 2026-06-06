package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/diff"
)

func TestBuild_GlossaryPrepended_WhenNonEmpty(t *testing.T) {
	root := setupFakePluginRoot(t)
	bundle := diff.Bundle{TargetLabel: "test"}
	gloss := "Account = customer account, not bank account"

	p, err := Build(root, ModeStandard, bundle, "{}", "", gloss, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(p.System, "Project glossary") {
		t.Fatalf("expected glossary header in system: %q", p.System)
	}
	if !strings.Contains(p.System, gloss) {
		t.Fatalf("expected glossary content in system: %q", p.System)
	}
	// Existing default system body MUST remain present after glossary.
	if !strings.Contains(p.System, "senior code reviewer") {
		t.Fatalf("default system body lost: %q", p.System)
	}
}

func TestBuild_GlossaryOmitted_WhenEmpty(t *testing.T) {
	root := setupFakePluginRoot(t)
	bundle := diff.Bundle{TargetLabel: "test"}

	p, err := Build(root, ModeStandard, bundle, "{}", "", "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(p.System, "Project glossary") {
		t.Fatalf("glossary header should be absent when empty")
	}
}

func TestBuild_GlossaryAppliesToAdversarial(t *testing.T) {
	root := setupFakePluginRoot(t)
	bundle := diff.Bundle{TargetLabel: "test"}

	p, err := Build(root, ModeAdversarial, bundle, "{}", "", "GLOSSARY", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(p.System, "GLOSSARY") {
		t.Fatalf("expected glossary in adversarial mode too")
	}
}

func setupFakePluginRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "prompts"))
	mustWrite(t, filepath.Join(root, "prompts", "review.md"), "REVIEW: {{TARGET_LABEL}}\n{{REFERENCED_FILES}}")
	mustWrite(t, filepath.Join(root, "prompts", "adversarial-review.md"), "ADV: {{TARGET_LABEL}}\n{{REFERENCED_FILES}}")
	return root
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, c string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuild_ReferencedFilesRendered(t *testing.T) {
	root := setupFakePluginRoot(t)
	bundle := diff.Bundle{
		TargetLabel: "test",
		ReferencedFiles: []diff.ReferencedFile{
			{
				Path:    "internal/path/path.go",
				Excerpt: "func Base(p string) string { return p }",
				Symbols: []string{"Base"},
			},
		},
	}
	p, err := Build(root, ModeStandard, bundle, "{}", "", "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(p.User, "Referenced files for context") {
		t.Fatalf("expected 'Referenced files for context' header in user prompt:\n%s", p.User)
	}
	if !strings.Contains(p.User, "internal/path/path.go") {
		t.Fatalf("expected referenced file path in prompt")
	}
	if !strings.Contains(p.User, "func Base") {
		t.Fatalf("expected excerpt content in prompt")
	}
	if !strings.Contains(p.User, "DO NOT flag findings") {
		t.Fatalf("expected read-only instruction in prompt")
	}
}

func TestBuild_ReferencedFilesOmittedWhenEmpty(t *testing.T) {
	root := setupFakePluginRoot(t)
	bundle := diff.Bundle{TargetLabel: "test"} // no ReferencedFiles
	p, _ := Build(root, ModeStandard, bundle, "{}", "", "", "")
	if strings.Contains(p.User, "Referenced files for context") {
		t.Fatalf("section must be absent when no referenced files")
	}
}

func TestBuild_EmbeddedFallbackOnEmptyPluginRoot(t *testing.T) {
	bundle := diff.Bundle{Diff: "diff --git a/x.go b/x.go\n@@\n+x\n"}
	p, err := Build("", ModeStandard, bundle, `{"type":"object"}`, "", "", "")
	if err != nil {
		t.Fatalf("Build with empty pluginRoot should use embedded: %v", err)
	}
	if p.User == "" {
		t.Fatalf("User prompt should not be empty")
	}
	if !strings.Contains(p.User, "diff --git") {
		t.Fatalf("expected diff to be interpolated into the prompt")
	}
}

func TestBuild_CustomPluginRootOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a recognisable custom template that still has the required
	// interpolation tokens so the function doesn't error.
	custom := "CUSTOM_HEADER\n\nReview target: {{TARGET_LABEL}}\nSchema: {{SCHEMA_INLINE}}\nDiff:\n{{REVIEW_INPUT}}\n{{REFERENCED_FILES}}\n"
	if err := os.WriteFile(filepath.Join(dir, "prompts", "review.md"), []byte(custom), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	p, err := Build(dir, ModeStandard, diff.Bundle{Diff: "marker-diff"}, "{}", "", "", "")
	if err != nil {
		t.Fatalf("Build with custom pluginRoot: %v", err)
	}
	if !strings.Contains(p.User, "CUSTOM_HEADER") {
		maxLen := len(p.User)
		if maxLen > 200 {
			maxLen = 200
		}
		t.Fatalf("expected custom template content; got %q", p.User[:maxLen])
	}
	if !strings.Contains(p.User, "marker-diff") {
		t.Fatalf("expected diff interpolation in custom template")
	}
}

func TestBuild_ReviewContextPrepended_WhenNonEmpty(t *testing.T) {
	root := setupFakePluginRoot(t)
	bundle := diff.Bundle{TargetLabel: "test"}
	ctx := "Treat belongsTo with array IDs as intentional."

	p, err := Build(root, ModeStandard, bundle, "{}", "", "", ctx)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(p.System, "Prior review context") {
		t.Fatalf("expected context header in system: %q", p.System)
	}
	if !strings.Contains(p.System, ctx) {
		t.Fatalf("expected context content in system: %q", p.System)
	}
	if !strings.Contains(p.System, "senior code reviewer") {
		t.Fatalf("default system body lost: %q", p.System)
	}
}
