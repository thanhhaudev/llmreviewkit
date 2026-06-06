//go:build lite

package symbols

import "time"

// useWASM always returns false in lite builds — WASM grammar files
// are not embedded, so the factory falls back to RegexExtractor.
func useWASM(ext string) bool { return false }

// newWASMExtractor is unreachable in lite builds (useWASM gates the call).
// Defined here so factory.go compiles under the lite tag.
func newWASMExtractor(ext string) Extractor {
	return &RegexExtractor{lang: extToLang(ext)}
}

// SetWorkspaceRoot is a no-op in lite builds — the tree-sitter runtime is
// not compiled in, so workspace-based grammar resolution is skipped.
func SetWorkspaceRoot(_ string) {}

// SetExtractionPolicy is a no-op in lite builds — DispatchPHP and the
// phpsyms-backed path live in policy.go (!lite). Engine.New() calls this
// unconditionally; the lite stub keeps that call site building.
func SetExtractionPolicy(_ int, _ time.Duration, _ int) {}

// ExtractEvent stub mirrors the !lite type so engine code can compile.
// In lite builds DispatchPHP is absent, so this is never fired.
type ExtractEvent struct {
	File     string
	Strategy int
	Duration time.Duration
}

// ExtractStrategyName mirrors the !lite implementation.
func ExtractStrategyName(strategy int) string {
	switch strategy {
	case 0:
		return "auto"
	case 1:
		return "treesitter"
	case 2:
		return "phpsyms"
	case 3:
		return "regex"
	}
	return "unknown"
}

// SetExtractObserver is a no-op in lite builds — DispatchPHP is absent so
// no events would fire. Stub preserves the call site in engine/kizunax.
func SetExtractObserver(_ func(ExtractEvent)) {}
