package render

import "strings"

// escapeMarkdownCell returns s safe to embed in a single markdown table cell.
// Replaces table-breaking characters with safe equivalents:
//   - `|`     → `\|` (escaped pipe; existing `\|` left untouched)
//   - `\r\n`  → space
//   - `\n`    → space
//   - `\t`    → space
//
// Idempotent: escapeMarkdownCell(escapeMarkdownCell(s)) == escapeMarkdownCell(s).
func escapeMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Escape `|` only when not already preceded by `\` so calling twice is a no-op.
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '|' && (i == 0 || s[i-1] != '\\') {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}
