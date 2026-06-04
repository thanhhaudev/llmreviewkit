//go:build !lite

// Package queries holds embedded tags.scm-style query strings per
// language plus a universal walker that translates Captures to
// []symbols.Symbol.
package queries

import (
	"strings"

	"github.com/thanhhaudev/llmreviewkit/symbols"
	"github.com/thanhhaudev/llmreviewkit/symbols/treesitter"
)

// CaptureToSymbol translates a single Capture to a Symbol based on the
// capture name. Returns (zero, false) if the capture name doesn't match
// a known pattern OR if the byte range is invalid.
//
// Mapping:
//   - name.definition.*        → SymDef
//   - name.reference.call      → SymCall
//   - name.reference.import    → SymImport
//   - module                   → SymImport
//   - name.reference.type      → SymTypeRef
//   - anything else            → drop
func CaptureToSymbol(c treesitter.Capture, src []byte, path string, line int) (symbols.Symbol, bool) {
	if c.StartByte >= c.EndByte || int(c.EndByte) > len(src) {
		return symbols.Symbol{}, false
	}
	name := string(src[c.StartByte:c.EndByte])

	var kind symbols.SymbolKind
	switch {
	case strings.HasPrefix(c.Name, "name.definition."):
		kind = symbols.SymDef
	case c.Name == "name.reference.call":
		kind = symbols.SymCall
	case c.Name == "name.reference.import" || c.Name == "module":
		kind = symbols.SymImport
	case c.Name == "name.reference.type":
		kind = symbols.SymTypeRef
	default:
		return symbols.Symbol{}, false
	}

	return symbols.Symbol{
		Name: name,
		Kind: kind,
		File: path,
		Line: line,
	}, true
}

// ScanCaptures translates a slice of captures to symbols. Captures
// that don't map to known kinds are silently dropped. Line numbers
// are computed from src by counting newlines up to StartByte.
func ScanCaptures(captures []treesitter.Capture, src []byte, path string) []symbols.Symbol {
	out := make([]symbols.Symbol, 0, len(captures))
	for _, c := range captures {
		line := lineNumberAt(src, c.StartByte)
		if sym, ok := CaptureToSymbol(c, src, path, line); ok {
			out = append(out, sym)
		}
	}
	return out
}

func lineNumberAt(src []byte, byteOff uint32) int {
	if int(byteOff) > len(src) {
		byteOff = uint32(len(src))
	}
	line := 1
	for i := uint32(0); i < byteOff; i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}
