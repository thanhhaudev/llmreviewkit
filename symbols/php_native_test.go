package symbols

import (
	"strings"
	"testing"
)

func TestExtractPHPViaPhpsyms_EmitsAllKinds(t *testing.T) {
	src := []byte(`<?php
namespace App\Http\Controllers;

use App\Models\User;

class UserController extends Controller implements Authenticatable
{
	public function show(?User $user): JsonResponse
	{
		Cache::get('key');
		$this->log('msg');
		helper(1, 2);
		return null;
	}
}`)
	syms := ExtractPHPViaPhpsyms("UserController.php", src)
	if len(syms) == 0 {
		t.Fatal("no symbols emitted")
	}

	// Expected SymDef: UserController class, show method
	// Expected SymImport: App\Models\User
	// Expected SymTypeRef: User (param), JsonResponse (return)
	// Expected SymCall: Cache::get, $this->log, helper

	want := map[string]SymbolKind{
		"UserController": SymDef,
		"show":           SymDef,
		"get":            SymCall,
		"log":            SymCall,
		"helper":         SymCall,
		"User":           SymTypeRef,
		"JsonResponse":   SymTypeRef,
	}
	for sym, wantKind := range want {
		var found bool
		for _, s := range syms {
			if s.Name == sym && s.Kind == wantKind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s (Kind=%v); syms=%+v", sym, wantKind, syms)
		}
	}

	// Verify import has the qualified name in either Name OR Pkg.
	var sawUserImport bool
	for _, s := range syms {
		if s.Kind == SymImport && (strings.Contains(s.Name, "User") || strings.Contains(s.Pkg, "User")) {
			sawUserImport = true
			break
		}
	}
	if !sawUserImport {
		t.Errorf("App\\Models\\User import not emitted; syms=%+v", syms)
	}

	// All emitted symbols must have File set to the input path.
	for _, s := range syms {
		if s.File != "UserController.php" {
			t.Errorf("File mismatch on %+v", s)
		}
	}

	// Line numbers must be 1-based, > 0.
	for _, s := range syms {
		if s.Line < 1 {
			t.Errorf("Line < 1 on %+v", s)
		}
	}
}

func TestExtractPHPViaPhpsyms_NoPanic_OnEmptyInput(t *testing.T) {
	syms := ExtractPHPViaPhpsyms("empty.php", []byte(""))
	// Empty input → either empty slice or nil. Both acceptable. Must not panic.
	if len(syms) > 0 {
		t.Errorf("empty input produced symbols: %+v", syms)
	}
}

func TestExtractPHPViaPhpsyms_NoPanic_OnGarbage(t *testing.T) {
	syms := ExtractPHPViaPhpsyms("garbage.php", []byte("\xff\xfe<?php /* unterminated"))
	// Must not panic. Return value may be empty or contain best-effort symbols.
	_ = syms
}
