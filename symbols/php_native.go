package symbols

import (
	"github.com/thanhhaudev/phpsyms"
)

// ExtractPHPViaPhpsyms runs the Go-native phpsyms extractor and adapts its
// Symbol shape into llmreviewkit's Symbol contract. This is the alternative
// to extractPHPViaWalk (tree-sitter) — opt-in via Config.ExtractionPolicy in
// later work. v1.5.0 adds this path; v1.8.0 will remove the tree-sitter
// fallback.
//
// Mapping (phpsyms.SymbolKind → symbols.SymbolKind):
//   - KindClass, KindInterface, KindTrait, KindMethod, KindFunction → SymDef
//   - KindUseImport                                                → SymImport
//   - KindStaticCall, KindMethodCall, KindFunctionCall             → SymCall
//   - KindTypeRef                                                  → SymTypeRef
//
// Notes:
//   - phpsyms doesn't panic on malformed input; the err return is always nil
//     in practice. Soft errors degrade to partial results.
//   - Anonymous classes have empty Name; we skip emitting them (they wouldn't
//     resolve to anything in the workspace index anyway).
//   - For SymImport, phpsyms emits the qualified name in s.Qualified, not s.Name.
//     We map Qualified → Symbol.Name (the qualified form is what callers index).
func ExtractPHPViaPhpsyms(path string, content []byte) []Symbol {
	psyms, _ := phpsyms.Extract(path, content)
	if len(psyms) == 0 {
		return nil
	}
	out := make([]Symbol, 0, len(psyms))
	for _, ps := range psyms {
		kind, ok := mapPhpsymsKind(ps.Kind)
		if !ok {
			continue
		}
		name := ps.Name
		if ps.Kind == phpsyms.KindUseImport {
			// phpsyms stores the qualified name in Qualified, not Name.
			name = ps.Qualified
		}
		if name == "" {
			// Anonymous class or unresolvable — skip.
			continue
		}
		out = append(out, Symbol{
			Name: name,
			Kind: kind,
			File: path,
			Line: ps.Range.StartLine,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapPhpsymsKind(k phpsyms.SymbolKind) (SymbolKind, bool) {
	switch k {
	case phpsyms.KindClass, phpsyms.KindInterface, phpsyms.KindTrait,
		phpsyms.KindMethod, phpsyms.KindFunction:
		return SymDef, true
	case phpsyms.KindUseImport:
		return SymImport, true
	case phpsyms.KindStaticCall, phpsyms.KindMethodCall, phpsyms.KindFunctionCall:
		return SymCall, true
	case phpsyms.KindTypeRef:
		return SymTypeRef, true
	}
	return 0, false
}
