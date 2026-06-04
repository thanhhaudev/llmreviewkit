package symbols

import "testing"

func TestSymbolKind_String(t *testing.T) {
	cases := []struct {
		kind SymbolKind
		want string
	}{
		{SymCall, "call"},
		{SymTypeRef, "typeref"},
		{SymImport, "import"},
		{SymDef, "def"},
		{SymbolKind(0), "unknown"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Fatalf("kind=%d: got %q want %q", c.kind, got, c.want)
		}
	}
}

func TestSymbol_ZeroValue(t *testing.T) {
	var s Symbol
	if s.Name != "" || s.Pkg != "" || s.Kind != 0 || s.File != "" || s.Line != 0 {
		t.Fatalf("expected zero-value, got %+v", s)
	}
}
