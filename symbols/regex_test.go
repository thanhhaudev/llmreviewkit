package symbols

import "testing"

func TestRegex_ExtractsTypeScriptFunction(t *testing.T) {
	src := []byte(`
export function authenticate(userId: number): Promise<User> {
	return tokenCache.get(userId);
}
`)
	e := &RegexExtractor{}
	syms := e.Extract("auth.ts", src)
	defs := symbolNames(syms, SymDef)
	wantContains(t, defs, "authenticate")
	calls := symbolNames(syms, SymCall)
	wantContains(t, calls, "get")
}

func TestRegex_ExtractsPythonDef(t *testing.T) {
	src := []byte(`
import os

def authenticate(user_id):
    session = SessionStore.get(user_id)
    return session.user
`)
	e := &RegexExtractor{}
	syms := e.Extract("auth.py", src)
	defs := symbolNames(syms, SymDef)
	wantContains(t, defs, "authenticate")
	imports := symbolNames(syms, SymImport)
	wantContains(t, imports, "os")
}

func TestRegex_ExtractsRustFn(t *testing.T) {
	src := []byte(`
use std::collections::HashMap;

fn build_map() -> HashMap<String, i32> {
	HashMap::new()
}
`)
	e := &RegexExtractor{}
	syms := e.Extract("main.rs", src)
	defs := symbolNames(syms, SymDef)
	wantContains(t, defs, "build_map")
}

func TestRegex_ExtractsJavaClass(t *testing.T) {
	src := []byte(`
public class UserRepository {
	private final Cache<String, User> cache;

	public User findById(String id) {
		return cache.get(id);
	}
}
`)
	e := &RegexExtractor{}
	syms := e.Extract("UserRepository.java", src)
	defs := symbolNames(syms, SymDef)
	wantContains(t, defs, "UserRepository")
}

func TestRegex_ExtractCallSites(t *testing.T) {
	src := []byte(`tokenCache.get(req.userId);`)
	e := &RegexExtractor{}
	syms := e.Extract("file.ts", src)
	for _, s := range syms {
		if s.Kind == SymCall && s.Name == "get" && s.Pkg == "tokenCache" {
			return
		}
	}
	t.Fatalf("expected tokenCache.get call, got %+v", syms)
}

func TestRegex_PositionInfoRecorded(t *testing.T) {
	src := []byte("// header\n\ndef hello():\n    pass\n")
	e := &RegexExtractor{}
	syms := e.Extract("h.py", src)
	for _, s := range syms {
		if s.Kind == SymDef && s.Name == "hello" {
			if s.Line != 3 {
				t.Fatalf("expected Line=3, got %d", s.Line)
			}
			if s.File != "h.py" {
				t.Fatalf("expected File=h.py, got %q", s.File)
			}
			return
		}
	}
	t.Fatalf("hello def not found")
}

func TestLangPatterns_HasDefaultEntry(t *testing.T) {
	ps, ok := langPatterns["default"]
	if !ok {
		t.Fatal("langPatterns[\"default\"] must exist as the fallback")
	}
	if len(ps.defs) == 0 || len(ps.imports) == 0 || len(ps.calls) == 0 {
		t.Fatalf("default patternSet must have defs/imports/calls populated: %+v", ps)
	}
}

func TestRegexExtractor_PHP(t *testing.T) {
	src := []byte(`<?php
namespace App\Auth;

use App\Db\Connection;
use App\Logger as Log;

class AuthService {
    public function login(string $username) {
        $row = $this->db->fetchRow($username);
        Connection::open();
        return $row;
    }

    private function validate($input) {
        return $input;
    }
}
`)
	got := (&RegexExtractor{lang: "php"}).Extract("auth.php", src)

	want := map[string]bool{
		"def:AuthService":      false,
		"def:login":            false,
		"def:validate":         false,
		"import:Connection":    false,
		"import:Log":           false,
		"call:db.fetchRow":     false,
		"call:Connection.open": false,
	}
	for _, s := range got {
		var key string
		switch s.Kind {
		case SymDef:
			key = "def:" + s.Name
		case SymImport:
			key = "import:" + s.Name
		case SymCall:
			key = "call:" + s.Pkg + "." + s.Name
		}
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing expected symbol: %s — got %+v", k, got)
		}
	}
}

func TestRegexExtractor_TS(t *testing.T) {
	src := []byte(`
import { Foo, Bar as Baz } from './lib';
import type { Qux } from './types';
import Default from './d';
import * as ns from './ns';

export function classic(x: number) { return x; }
export const arrow = (n: number) => n + 1;
const inferred = async () => 42;

export class Service {
    handle(): void {}
}

interface Iface {}
type Alias = string;
enum Mode { A, B }

obj.method();
maybe?.method();
`)
	got := (&RegexExtractor{lang: "ts"}).Extract("svc.ts", src)

	want := map[string]bool{
		"def:classic":       false,
		"def:arrow":         false,
		"def:inferred":      false,
		"def:Service":       false,
		"def:Iface":         false,
		"def:Alias":         false,
		"def:Mode":          false,
		"import:Foo":        false,
		"import:Baz":        false, // aliased from Bar
		"import:Qux":        false,
		"import:Default":    false,
		"import:ns":         false,
		"call:obj.method":   false,
		"call:maybe.method": false,
	}
	for _, s := range got {
		var key string
		switch s.Kind {
		case SymDef:
			key = "def:" + s.Name
		case SymImport:
			key = "import:" + s.Name
		case SymCall:
			key = "call:" + s.Pkg + "." + s.Name
		}
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing expected symbol: %s — got %+v", k, got)
		}
	}
}

func TestRegexExtractor_Python(t *testing.T) {
	src := []byte(`
from app.db import Connection
import app.logger as log
import os

@app.route("/login")
def login(username):
    row = db.fetch_row(username)
    return row

async def refresh():
    pass

class AuthService:
    def authenticate(self, user):
        return self.validate(user)
`)
	got := (&RegexExtractor{lang: "python"}).Extract("auth.py", src)

	want := map[string]bool{
		"def:login":          false,
		"def:refresh":        false,
		"def:authenticate":   false,
		"def:AuthService":    false,
		"import:Connection":  false,
		"import:log":         false, // alias of app.logger
		"import:os":          false,
		"call:db.fetch_row":  false,
		"call:self.validate": false,
	}
	for _, s := range got {
		var key string
		switch s.Kind {
		case SymDef:
			key = "def:" + s.Name
		case SymImport:
			key = "import:" + s.Name
		case SymCall:
			key = "call:" + s.Pkg + "." + s.Name
		}
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing expected symbol: %s — got %+v", k, got)
		}
	}
}

func TestRegexExtractor_DefaultLang_PreservesV012Behavior(t *testing.T) {
	src := []byte("func Foo() {}\nimport \"bar/baz\"\npkg.Method()\n")
	got := (&RegexExtractor{lang: "default"}).Extract("x.unknown", src)

	var hasDef, hasImp, hasCall bool
	for _, s := range got {
		switch s.Kind {
		case SymDef:
			if s.Name == "Foo" {
				hasDef = true
			}
		case SymImport:
			if s.Name == "bar/baz" {
				hasImp = true
			}
		case SymCall:
			if s.Pkg == "pkg" && s.Name == "Method" {
				hasCall = true
			}
		}
	}
	if !hasDef || !hasImp || !hasCall {
		t.Fatalf("default lang must yield def+import+call: %+v", got)
	}

	// Empty lang must behave identically to "default".
	gotEmpty := (&RegexExtractor{}).Extract("x.unknown", src)
	if len(gotEmpty) != len(got) {
		t.Fatalf("empty lang must equal default lang: got %d vs %d", len(gotEmpty), len(got))
	}
}
