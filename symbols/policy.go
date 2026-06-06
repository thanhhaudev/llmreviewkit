//go:build !lite

package symbols

import (
	"context"
	"fmt"
	"time"

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
//	2 = StrategyPhpsyms
//	3 = StrategyRegex
type extractionPolicyState struct {
	php            int
	extractTimeout time.Duration // 0 = no cap
	maxFileSize    int           // 0 = no cap
}

var currentPolicy = extractionPolicyState{php: 0}

// SetExtractionPolicy is called by engine.New() to mirror the engine.Config
// policy into the symbols package, since symbols cannot import engine.
// The phpStrategy argument encodes engine.ExtractionStrategy: 0=auto,
// 1=treesitter, 2=phpsyms, 3=regex.
// extractTimeout=0 disables the per-file timeout. maxFileSize=0 disables
// the size cap.
//
// Threading model: this is set once at New() time and read by the dispatch
// switch on every Extract call. Reads are not atomic; setup happens before
// any Extract goroutines are spawned by the engine, so this is safe in
// practice.
func SetExtractionPolicy(phpStrategy int, extractTimeout time.Duration, maxFileSize int) {
	currentPolicy.php = phpStrategy
	currentPolicy.extractTimeout = extractTimeout
	currentPolicy.maxFileSize = maxFileSize
}

// DispatchPHP routes PHP extraction through the configured strategy.
// StrategyAuto (default 0) prefers phpsyms (Go-native) and falls back to
// extractPHPViaWalk (tree-sitter) only on empty result. StrategyTreeSitter
// always uses tree-sitter. StrategyPhpsyms always uses phpsyms.
// StrategyRegex always uses regexFallback.
//
// After extraction completes, an ExtractEvent is fired via fireExtractEvent
// (no-op when no observer is registered). The event reports the strategy
// that ACTUALLY ran — for StrategyAuto this is 2 (Phpsyms) when phpsyms
// succeeds, or 1 (TreeSitter) when it falls back.
//
// Tree-sitter path is preserved until v1.8.0 (a wrap-up).
func DispatchPHP(ctx context.Context, lang *treesitter.Language, content []byte, path string) []Symbol {
	start := time.Now()
	switch currentPolicy.php {
	case 2: // StrategyPhpsyms
		syms := ExtractPHPViaPhpsyms(path, content)
		fireExtractEvent(path, 2, time.Since(start))
		return syms
	case 1: // StrategyTreeSitter
		syms := extractPHPViaWalk(ctx, lang, content, path)
		fireExtractEvent(path, 1, time.Since(start))
		return syms
	case 3: // StrategyRegex
		syms := regexFallback(path, content)
		fireExtractEvent(path, 3, time.Since(start))
		return syms
	default: // StrategyAuto
		syms := ExtractPHPViaPhpsyms(path, content)
		if len(syms) == 0 {
			syms = extractPHPViaWalk(ctx, lang, content, path)
			fireExtractEvent(path, 1, time.Since(start)) // fell back to TreeSitter
			return syms
		}
		fireExtractEvent(path, 2, time.Since(start)) // Phpsyms succeeded
		return syms
	}
}

// PolicyWarning is a soft warning surfaced when the extraction policy bypasses
// or aborts extraction for a file (size cap exceeded, timeout fired).
// Callers are expected to log or aggregate these.
type PolicyWarning struct {
	File   string
	Reason string
}

// ExtractWithPolicy applies the per-file MaxFileSize + ExtractionTimeout
// gates from the current policy, then dispatches to the language-specific
// extractor. Currently only PHP is policy-routed (via DispatchPHP); other
// languages go through the default Extractor flow (ForFile + Extract) which
// is what callers use today.
//
// This function is opt-in. Callers that don't need timeout/size gates can
// keep using ForFile + Extract directly.
func ExtractWithPolicy(ctx context.Context, lang *treesitter.Language, path string, content []byte) ([]Symbol, *PolicyWarning) {
	if currentPolicy.maxFileSize > 0 && len(content) > currentPolicy.maxFileSize {
		return nil, &PolicyWarning{
			File:   path,
			Reason: fmt.Sprintf("file %d bytes exceeds MaxFileSize %d", len(content), currentPolicy.maxFileSize),
		}
	}

	timeout := currentPolicy.extractTimeout
	if timeout <= 0 {
		return DispatchPHP(ctx, lang, content, path), nil
	}

	type result struct{ syms []Symbol }
	ch := make(chan result, 1)
	go func() {
		ch <- result{syms: DispatchPHP(ctx, lang, content, path)}
	}()
	select {
	case r := <-ch:
		return r.syms, nil
	case <-time.After(timeout):
		return nil, &PolicyWarning{
			File:   path,
			Reason: fmt.Sprintf("extraction exceeded %s", timeout),
		}
	}
}
