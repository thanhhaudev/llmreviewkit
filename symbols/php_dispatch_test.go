//go:build !lite

package symbols

import (
	"strings"
	"testing"
	"time"
)

func TestDispatchPHP_DefaultAuto_UsesPhpsyms(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0, 0, 0) }) // reset
	SetExtractionPolicy(0, 0, 0)                       // StrategyAuto
	src := []byte("<?php class Foo { public function bar() {} }")
	syms := DispatchPHP(nil, nil, src, "Foo.php")
	if len(syms) == 0 {
		t.Fatal("auto strategy returned no symbols")
	}
	// Auto path with non-empty phpsyms result must include 'Foo' SymDef.
	var sawFoo bool
	for _, s := range syms {
		if s.Name == "Foo" && s.Kind == SymDef {
			sawFoo = true
		}
	}
	if !sawFoo {
		t.Errorf("Foo SymDef not in result: %+v", syms)
	}
}

func TestDispatchPHP_GoNative_UsesPhpsyms(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0, 0, 0) })
	SetExtractionPolicy(2, 0, 0) // StrategyGoNative
	src := []byte("<?php class Foo {}")
	syms := DispatchPHP(nil, nil, src, "Foo.php")
	if len(syms) == 0 {
		t.Fatal("gonative strategy returned no symbols")
	}
}

func TestDispatchPHP_Regex_UsesRegex(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0, 0, 0) })
	SetExtractionPolicy(3, 0, 0) // StrategyRegex
	src := []byte("<?php class Foo {}")
	syms := DispatchPHP(nil, nil, src, "Foo.php")
	// regexFallback is the baseline — must emit something for a simple class.
	if len(syms) == 0 {
		t.Fatal("regex strategy returned no symbols")
	}
}

func TestExtractWithPolicy_SkipsLargeFiles(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0, 0, 0) })
	SetExtractionPolicy(0, 0, 100)                                // maxFileSize=100 bytes
	big := []byte("<?php " + strings.Repeat("class Foo {} ", 50)) // >100 bytes
	if len(big) <= 100 {
		t.Fatalf("test input too small: %d", len(big))
	}
	syms, warn := ExtractWithPolicy(nil, nil, "Big.php", big)
	if len(syms) != 0 {
		t.Errorf("expected skip; got %d syms", len(syms))
	}
	if warn == nil {
		t.Fatal("expected PolicyWarning")
	}
	if warn.File != "Big.php" {
		t.Errorf("warn.File: got %q want %q", warn.File, "Big.php")
	}
	if !strings.Contains(warn.Reason, "MaxFileSize") {
		t.Errorf("warn.Reason missing 'MaxFileSize': %q", warn.Reason)
	}
}

func TestExtractWithPolicy_RunsWhenWithinSizeBudget(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0, 0, 0) })
	SetExtractionPolicy(0, 0, 1024) // 1 KiB cap
	syms, warn := ExtractWithPolicy(nil, nil, "Small.php", []byte("<?php class Foo {}"))
	if warn != nil {
		t.Fatalf("unexpected warning: %+v", warn)
	}
	if len(syms) == 0 {
		t.Fatal("expected symbols; got none")
	}
}

func TestExtractWithPolicy_TimeoutFiresOrNot(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0, 0, 0) })
	// Set a ridiculously small timeout so the select most likely picks the
	// timeout branch. If the scheduler beats us and the goroutine wins,
	// that's also valid behavior (best-effort timeout). Either result is OK
	// as long as no panic.
	SetExtractionPolicy(0, 1*time.Nanosecond, 0)
	_, _ = ExtractWithPolicy(nil, nil, "T.php", []byte("<?php class Foo {}"))
	// Just verifying no panic/deadlock under heavy time pressure.
}

func TestExtractWithPolicy_DefaultsAllowExtraction(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0, 0, 0) })
	// No timeout, no size cap — should always extract.
	SetExtractionPolicy(0, 0, 0)
	syms, warn := ExtractWithPolicy(nil, nil, "D.php", []byte("<?php class Foo {}"))
	if warn != nil {
		t.Fatalf("unexpected warning: %+v", warn)
	}
	if len(syms) == 0 {
		t.Fatal("expected symbols")
	}
}
