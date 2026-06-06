//go:build !lite

package symbols

import "testing"

func TestDispatchPHP_DefaultAuto_UsesPhpsyms(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0) }) // reset
	SetExtractionPolicy(0)                       // StrategyAuto
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
	t.Cleanup(func() { SetExtractionPolicy(0) })
	SetExtractionPolicy(2) // StrategyGoNative
	src := []byte("<?php class Foo {}")
	syms := DispatchPHP(nil, nil, src, "Foo.php")
	if len(syms) == 0 {
		t.Fatal("gonative strategy returned no symbols")
	}
}

func TestDispatchPHP_Regex_UsesRegex(t *testing.T) {
	t.Cleanup(func() { SetExtractionPolicy(0) })
	SetExtractionPolicy(3) // StrategyRegex
	src := []byte("<?php class Foo {}")
	syms := DispatchPHP(nil, nil, src, "Foo.php")
	// regexFallback is the baseline — must emit something for a simple class.
	if len(syms) == 0 {
		t.Fatal("regex strategy returned no symbols")
	}
}
