//go:build !lite

package symbols

import "time"

// ExtractEvent reports a single PHP extraction call: which file, which
// strategy ran, and how long it took. Emitted by DispatchPHP after the
// underlying extractor returns.
//
// Strategy is the int encoding from policy.go (0=Auto 1=TreeSitter
// 2=Phpsyms 3=Regex). Use ExtractStrategyName to render it.
type ExtractEvent struct {
	File     string
	Strategy int
	Duration time.Duration
}

// ExtractStrategyName returns a short human-readable label for the
// strategy int. Useful for telemetry counters in ResolveStats.ExtractorPath.
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

// extractObserver is the singleton sink for ExtractEvent. nil means no
// observer registered; DispatchPHP skips the fire fast-path in that case.
//
// Note: not atomic. Callers are expected to call SetExtractObserver at
// engine setup time, before any Extract goroutines spawn. Subsequent reads
// happen during normal extraction. This mirrors the SetExtractionPolicy
// pattern.
var extractObserver func(ExtractEvent)

// SetExtractObserver installs the observer. Pass nil to clear.
func SetExtractObserver(f func(ExtractEvent)) {
	extractObserver = f
}

// fireExtractEvent emits an event if an observer is registered. Internal —
// used by DispatchPHP. Kept package-private so external callers don't
// fabricate events.
func fireExtractEvent(file string, strategy int, duration time.Duration) {
	if extractObserver != nil {
		extractObserver(ExtractEvent{File: file, Strategy: strategy, Duration: duration})
	}
}
