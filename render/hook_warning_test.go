package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thanhhaudev/llmreviewkit/schema"
)

var updateHookGolden = flag.Bool("update-hook-golden", false, "update hook-warning golden")

func TestRenderHookWarning_Golden(t *testing.T) {
	r := schema.ReviewResult{
		Findings: []schema.Finding{
			{Severity: "high", Title: "Missing error wrap on token refresh", File: "api.go", LineStart: 142},
			{Severity: "high", Title: "Race in connection pool", File: "db.go", LineStart: 88},
			{Severity: "medium", Title: "Inefficient sort", File: "list.go", LineStart: 30},
		},
	}
	got := RenderHookWarning(r)

	goldenPath := filepath.Join("testdata", "hook-warning.golden.md")
	if *updateHookGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("output mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func TestRenderHookWarning_OnlyHighAndCritical(t *testing.T) {
	r := schema.ReviewResult{
		Findings: []schema.Finding{
			{Severity: "low", Title: "low one", File: "x.go", LineStart: 1},
			{Severity: "medium", Title: "med one", File: "x.go", LineStart: 2},
		},
	}
	got := RenderHookWarning(r)
	if got != "" {
		t.Errorf("expected empty output for no high+, got:\n%s", got)
	}
}

func TestRenderHookWarning_CapsAtFive(t *testing.T) {
	var findings []schema.Finding
	for i := 0; i < 7; i++ {
		findings = append(findings, schema.Finding{
			Severity: "high", Title: "issue", File: "a.go", LineStart: i,
		})
	}
	got := RenderHookWarning(schema.ReviewResult{Findings: findings})
	if got == "" {
		t.Fatalf("expected output, got empty")
	}
	if !strings.Contains(got, "(2 more)") {
		t.Errorf("expected '(2 more)' overflow row, got:\n%s", got)
	}
	// Count finding rows: each high row mentions "issue" once.
	if n := strings.Count(got, "| issue |"); n != 5 {
		t.Errorf("expected 5 capped finding rows, got %d:\n%s", n, got)
	}
}
