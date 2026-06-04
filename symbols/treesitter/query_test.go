//go:build !lite

package treesitter

import (
	"context"
	"testing"
)

func TestQuery_PHPFunctionDef(t *testing.T) {
	ctx := context.Background()
	r, err := newRuntime(ctx)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer r.Close(ctx)
	lang, err := r.LoadGrammar(ctx, "php", loadPhpGrammar(t))
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	// PHP 0.24.2 traps OOB in ts_query_new even with a fresh isolated runtime,
	// so we skip rather than fail when that path errors out. Production uses
	// cursor-walk (extractPHPViaWalk) instead — see internal/symbols/wasm.go.
	q, err := lang.NewQuery(ctx, "(function_definition name: (name) @name.definition.function)")
	if err != nil {
		t.Skipf("PHP NewQuery unsupported on this grammar build (production uses cursor walk): %v", err)
	}
	defer q.Close(ctx)

	src := []byte("<?php\nfunction login() {}\nfunction logout() {}\n")
	tree, err := lang.Parse(ctx, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Close(ctx)

	caps, err := q.Exec(ctx, tree.RootNode(ctx))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	want := map[string]bool{"login": false, "logout": false}
	for _, c := range caps {
		text := string(src[c.StartByte:c.EndByte])
		if _, ok := want[text]; ok {
			want[text] = true
		}
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("missing capture for %q in %+v", name, caps)
		}
	}
}
