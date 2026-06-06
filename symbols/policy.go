//go:build !lite

package symbols

import (
	"context"

	"github.com/thanhhaudev/llmreviewkit/symbols/treesitter"
)

// extractionPolicyState mirrors engine.ExtractionPolicy without importing
// engine (which would cycle). engine.New() copies its policy into here via
// SetExtractionPolicy below.
//
// Strategy int mapping matches engine.ExtractionStrategy:
//
//	0 = StrategyAuto
//	1 = StrategyTreeSitter
//	2 = StrategyGoNative
//	3 = StrategyRegex
type extractionPolicyState struct {
	php int
}

var currentPolicy = extractionPolicyState{php: 0}

// SetExtractionPolicy is called by engine.New() to mirror the engine.Config
// policy into the symbols package, since symbols cannot import engine.
// The int argument encodes engine.ExtractionStrategy: 0=auto, 1=treesitter,
// 2=gonative, 3=regex.
//
// Threading model: this is set once at New() time and read by the dispatch
// switch on every Extract call. Reads are not atomic; setup happens before
// any Extract goroutines are spawned by the engine, so this is safe in
// practice.
func SetExtractionPolicy(phpStrategy int) {
	currentPolicy.php = phpStrategy
}

// DispatchPHP routes PHP extraction through the configured strategy.
// StrategyAuto (default 0) prefers phpsyms (Go-native) and falls back to
// extractPHPViaWalk (tree-sitter) only on empty result. StrategyTreeSitter
// always uses tree-sitter. StrategyGoNative always uses phpsyms.
// StrategyRegex always uses regexFallback.
//
// Tree-sitter path is preserved until v1.8.0 (a wrap-up).
func DispatchPHP(ctx context.Context, lang *treesitter.Language, content []byte, path string) []Symbol {
	switch currentPolicy.php {
	case 2: // StrategyGoNative
		return ExtractPHPViaPhpsyms(path, content)
	case 1: // StrategyTreeSitter
		return extractPHPViaWalk(ctx, lang, content, path)
	case 3: // StrategyRegex
		return regexFallback(path, content)
	default: // StrategyAuto
		syms := ExtractPHPViaPhpsyms(path, content)
		if len(syms) == 0 {
			return extractPHPViaWalk(ctx, lang, content, path)
		}
		return syms
	}
}
