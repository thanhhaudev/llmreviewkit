package render

import (
	"fmt"
	"strings"

	"github.com/thanhhaudev/llmreviewkit/schema"
)

const maxHookFindings = 5

// RenderHookWarning emits a short markdown block summarizing high+/critical
// findings for CC to inject into the Stop response. Returns an empty
// string when no finding qualifies. Caps the table at maxHookFindings rows
// and appends a "(N more)" line when the cap is exceeded.
func RenderHookWarning(r schema.ReviewResult) string {
	var rows []schema.Finding
	for _, f := range r.Findings {
		if f.Severity == "high" || f.Severity == "critical" {
			rows = append(rows, f)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	capped := rows
	if len(capped) > maxHookFindings {
		capped = capped[:maxHookFindings]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "> ⚠️ **Kizunax stop-gate**: %d high-severity finding(s) detected in working tree.\n>\n", len(rows))
	b.WriteString("> | Sev | File:Line | Finding |\n")
	b.WriteString("> |-----|-----------|---------|\n")
	for _, f := range capped {
		fmt.Fprintf(&b, "> | %s | %s:%d | %s |\n", f.Severity, f.File, f.LineStart, f.Title)
	}
	if len(rows) > len(capped) {
		fmt.Fprintf(&b, "> | … | (%d more) | |\n", len(rows)-len(capped))
	}
	b.WriteString(">\n")
	b.WriteString("> _Run `/kizunax:review` for full output. Disable: `/kizunax:setup --disable-stop-gate`._\n")
	return b.String()
}
