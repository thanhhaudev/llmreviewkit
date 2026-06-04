package symbols

import (
	"sort"
	"testing"
)

func TestGoAST_ExtractCallExpressions(t *testing.T) {
	src := []byte(`
package main

import (
	"path"
	"strings"
)

func main() {
	base := path.Base("foo")
	upper := strings.ToUpper(base)
	_ = upper
}
`)
	e := &GoASTExtractor{}
	syms := e.Extract("main.go", src)

	names := symbolNames(syms, SymCall)
	wantContains(t, names, "Base", "ToUpper")
}

func TestGoAST_ExtractTypeDefinitions(t *testing.T) {
	src := []byte(`
package model

type User struct {
	ID   int
	Name string
}

type Repo interface {
	Get(id int) (*User, error)
}
`)
	e := &GoASTExtractor{}
	syms := e.Extract("model.go", src)

	names := symbolNames(syms, SymDef)
	wantContains(t, names, "User", "Repo")
}

func TestGoAST_ExtractImports(t *testing.T) {
	src := []byte(`
package main

import (
	"fmt"
	"os"
)

func main() { fmt.Println(os.Args) }
`)
	e := &GoASTExtractor{}
	syms := e.Extract("main.go", src)

	names := symbolNames(syms, SymImport)
	wantContains(t, names, "fmt", "os")
}

func TestGoAST_PkgPrefixRecorded(t *testing.T) {
	src := []byte(`
package x

import "path"

func f() { _ = path.Base("/a/b") }
`)
	e := &GoASTExtractor{}
	syms := e.Extract("x.go", src)

	for _, s := range syms {
		if s.Kind == SymCall && s.Name == "Base" {
			if s.Pkg != "path" {
				t.Fatalf("expected Pkg=path, got %q", s.Pkg)
			}
			return
		}
	}
	t.Fatalf("did not find Base call symbol; got %+v", syms)
}

func TestGoAST_PositionInfoRecorded(t *testing.T) {
	src := []byte(`package x
func A() {}
func B() {}
`)
	e := &GoASTExtractor{}
	syms := e.Extract("x.go", src)
	for _, s := range syms {
		if s.Kind != SymDef {
			continue
		}
		if s.File != "x.go" {
			t.Fatalf("expected File=x.go, got %q", s.File)
		}
		if s.Line == 0 {
			t.Fatalf("expected Line > 0 for %s", s.Name)
		}
	}
}

func TestGoAST_ParseFailureReturnsEmpty(t *testing.T) {
	src := []byte(`not a valid go file`)
	e := &GoASTExtractor{}
	syms := e.Extract("x.go", src)
	if len(syms) != 0 {
		t.Fatalf("expected empty on parse failure, got %+v", syms)
	}
}

func TestGoAST_Generics(t *testing.T) {
	src := []byte(`package x

type Map[K comparable, V any] struct{}

func MakeMap[K comparable, V any]() Map[K, V] { return Map[K, V]{} }
`)
	e := &GoASTExtractor{}
	syms := e.Extract("x.go", src)
	names := symbolNames(syms, SymDef)
	wantContains(t, names, "Map", "MakeMap")
}

func symbolNames(syms []Symbol, kind SymbolKind) []string {
	var out []string
	for _, s := range syms {
		if s.Kind == kind {
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}

func wantContains(t *testing.T, got []string, want ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Fatalf("missing %q in %v", w, got)
		}
	}
}

func TestGoAST_ThreeLevelSelector(t *testing.T) {
	src := []byte(`package main

func main() {
	m.auth.Authenticate("x")
	pkg.Sub.Func()
}
`)
	got := (&GoASTExtractor{}).Extract("main.go", src)

	want := map[string]bool{
		"call:auth.Authenticate": false,
		"call:Sub.Func":          false,
	}
	for _, s := range got {
		if s.Kind == SymCall {
			key := "call:" + s.Pkg + "." + s.Name
			if _, ok := want[key]; ok {
				want[key] = true
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing expected call symbol: %s — got %+v", k, got)
		}
	}
}

func TestGoAST_CallResultChain_BailsToTwoLevel(t *testing.T) {
	// f().method — Fun.X is *ast.CallExpr (not Ident, not SelectorExpr).
	// Must NOT emit a bogus pkg name; emitting Pkg="" is acceptable.
	src := []byte(`package main
func main() { f().method() }
`)
	got := (&GoASTExtractor{}).Extract("main.go", src)
	for _, s := range got {
		if s.Kind == SymCall && s.Name == "method" && s.Pkg != "" {
			t.Errorf("expected no pkg for f().method, got Pkg=%q", s.Pkg)
		}
	}
}
