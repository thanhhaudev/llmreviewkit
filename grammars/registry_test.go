//go:build !lite

package grammars

import "testing"

func TestRegistry_HasCoreLanguages(t *testing.T) {
	for _, lang := range []string{"php", "typescript", "python"} {
		e, ok := Registry[lang]
		if !ok {
			t.Errorf("Registry missing %q", lang)
			continue
		}
		if e.NpmPackage == "" || e.Version == "" || e.SHA256 == "" || e.WasmFile == "" {
			t.Errorf("Registry[%q] has empty field: %+v", lang, e)
		}
		if e.GrammarName == "" {
			t.Errorf("Registry[%q] empty GrammarName", lang)
		}
		if e.Lang != lang {
			t.Errorf("Registry[%q].Lang = %q, want %q", lang, e.Lang, lang)
		}
	}
}

func TestEntry_CDNUrl(t *testing.T) {
	e := Entry{NpmPackage: "tree-sitter-php", Version: "0.23.10", WasmFile: "tree-sitter-php.wasm"}
	got := e.CDNUrl()
	want := "https://unpkg.com/tree-sitter-php@0.23.10/tree-sitter-php.wasm"
	if got != want {
		t.Errorf("CDNUrl: got %q, want %q", got, want)
	}
}
