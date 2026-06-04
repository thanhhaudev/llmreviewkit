package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/prompt"
	"github.com/thanhhaudev/llmreviewkit/schema"
)

func RenderReview(r schema.ReviewResult, bundle diff.Bundle, totalTokens int, mode prompt.Mode) string {
	var sb strings.Builder

	header := "Review"
	if mode == prompt.ModeAdversarial {
		header = "Adversarial review"
	}

	if r.Verdict == "approve" {
		sb.WriteString(fmt.Sprintf("## ✓ %s verdict: approve\n\n", header))
	} else {
		sb.WriteString(fmt.Sprintf("## ⚠ %s verdict: needs-attention\n\n", header))
	}

	sb.WriteString(fmt.Sprintf("_Target: %s_\n\n", bundle.TargetLabel))

	switch {
	case r.TLDR != "":
		sb.WriteString(r.TLDR)
		sb.WriteString("\n\n")
	case r.Summary != "":
		sb.WriteString(r.Summary)
		sb.WriteString("\n\n")
	}

	if len(bundle.Warnings) > 0 {
		sb.WriteString("**Warnings**:\n")
		for _, w := range bundle.Warnings {
			sb.WriteString("- " + w + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.Findings) > 0 {
		findings := append([]schema.Finding{}, r.Findings...)
		sortFindings(findings)

		sb.WriteString(fmt.Sprintf("### Findings (%d)\n\n", len(findings)))
		sb.WriteString("| # | Severity | Location | Title |\n")
		sb.WriteString("|---|---|---|---|\n")
		for i, f := range findings {
			sb.WriteString(fmt.Sprintf("| %d | %s | `%s:%d-%d` | %s |\n",
				i+1,
				severityIcon(f.Severity),
				escapeMarkdownCell(f.File),
				f.LineStart,
				f.LineEnd,
				escapeMarkdownCell(f.Title)))
		}
		sb.WriteString("\n")

		for i, f := range findings {
			sb.WriteString(fmt.Sprintf("#### %d. %s `[%s, confidence %.2f]`\n\n", i+1, f.Title, f.Severity, f.Confidence))
			sb.WriteString(fmt.Sprintf("**File**: `%s:%d-%d`\n\n", f.File, f.LineStart, f.LineEnd))
			sb.WriteString(f.Body)
			sb.WriteString("\n\n")
			if f.Recommendation != "" {
				sb.WriteString("**Recommendation**: ")
				sb.WriteString(f.Recommendation)
				sb.WriteString("\n\n")
			}
		}
	} else {
		sb.WriteString("_No findings._\n\n")
	}

	if len(r.NextSteps) > 0 {
		sb.WriteString("### Next steps\n\n")
		for i, s := range r.NextSteps {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
		sb.WriteString("\n")
	}

	if totalTokens > 0 {
		sb.WriteString(fmt.Sprintf("_Tokens used: %d_\n", totalTokens))
	}

	return sb.String()
}

func sortFindings(f []schema.Finding) {
	order := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	sort.SliceStable(f, func(i, j int) bool {
		if order[f[i].Severity] != order[f[j].Severity] {
			return order[f[i].Severity] < order[f[j].Severity]
		}
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		return f[i].LineStart < f[j].LineStart
	})
}

func severityIcon(s string) string {
	switch s {
	case "critical":
		return "🔴 critical"
	case "high":
		return "🟠 high"
	case "medium":
		return "🟡 medium"
	case "low":
		return "🔵 low"
	}
	return s
}
