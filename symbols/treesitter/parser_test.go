//go:build !lite

package treesitter

import (
	"context"
	"os"
	"testing"
)

func loadPhpGrammar(t *testing.T) []byte {
	t.Helper()
	path := "../../../test-fixtures/tree-sitter-php.wasm"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("php grammar fixture not present at %s: %v (run test-fixtures/fetch.sh)", path, err)
	}
	return data
}

func TestParser_ParsePHP(t *testing.T) {
	ctx := context.Background()
	r, err := getRuntime(ctx)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}

	grammarBytes := loadPhpGrammar(t)
	lang, err := r.LoadGrammar(ctx, "php", grammarBytes)
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	defer lang.Close(ctx)

	src := []byte("<?php\nfunction login() { return true; }\n")
	tree, err := lang.Parse(ctx, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Close(ctx)

	root := tree.RootNode(ctx)
	if root.Type(ctx) == "" {
		t.Fatal("root node has empty type")
	}
	if root.ChildCount(ctx) == 0 {
		t.Fatal("root node has no children")
	}
	t.Logf("root node type: %q, child count: %d", root.Type(ctx), root.ChildCount(ctx))
}
