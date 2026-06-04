//go:build lite

package symbols

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
