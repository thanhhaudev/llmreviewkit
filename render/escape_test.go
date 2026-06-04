package render

import "testing"

func TestEscapeMarkdownCell_Pipe(t *testing.T) {
	if got := escapeMarkdownCell("a|b"); got != `a\|b` {
		t.Errorf("a|b → %q, want a\\|b", got)
	}
}

func TestEscapeMarkdownCell_Newline(t *testing.T) {
	if got := escapeMarkdownCell("a\nb"); got != "a b" {
		t.Errorf("newline → %q, want 'a b'", got)
	}
	if got := escapeMarkdownCell("a\r\nb"); got != "a b" {
		t.Errorf("CRLF → %q, want 'a b'", got)
	}
}

func TestEscapeMarkdownCell_Tab(t *testing.T) {
	if got := escapeMarkdownCell("a\tb"); got != "a b" {
		t.Errorf("tab → %q, want 'a b'", got)
	}
}

func TestEscapeMarkdownCell_Idempotent(t *testing.T) {
	once := escapeMarkdownCell("a|b\nc")
	twice := escapeMarkdownCell(once)
	if once != twice {
		t.Errorf("not idempotent: once=%q twice=%q", once, twice)
	}
}

func TestEscapeMarkdownCell_PlainText(t *testing.T) {
	in := "plain finding title with spaces"
	if got := escapeMarkdownCell(in); got != in {
		t.Errorf("plain text changed: %q → %q", in, got)
	}
}
