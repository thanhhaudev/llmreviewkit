//go:build !lite

package grammars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstall_HappyPath(t *testing.T) {
	payload := []byte("fake-wasm-bytes")
	sum := sha256.Sum256(payload)
	expectedHash := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		w.Write(payload)
	}))
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	entry := Entry{
		Lang: "fake", GrammarName: "fake",
		NpmPackage: "tree-sitter-fake", Version: "1.0.0",
		WasmFile: "fake.wasm", SHA256: expectedHash,
	}

	if err := installFromURL(context.Background(), entry, srv.URL, false); err != nil {
		t.Fatalf("install: %v", err)
	}

	dest := filepath.Join(tmpHome, ".kizunax/grammars/fake.wasm")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected file at %s: %v", dest, err)
	}
	if string(data) != string(payload) {
		t.Fatalf("content mismatch")
	}
}

func TestInstall_SHAMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("wrong payload"))
	}))
	defer srv.Close()

	entry := Entry{
		Lang: "fake", GrammarName: "fake",
		NpmPackage: "tree-sitter-fake", Version: "1.0.0",
		WasmFile: "fake.wasm",
		// 64 hex chars that won't match SHA256("wrong payload")
		SHA256: "deadbeef0000000000000000000000000000000000000000000000000000beef",
	}
	t.Setenv("HOME", t.TempDir())

	if err := installFromURL(context.Background(), entry, srv.URL, false); err == nil {
		t.Fatal("expected SHA mismatch error")
	}
}

func TestInstall_Http404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	entry := Entry{
		Lang: "fake", GrammarName: "fake",
		WasmFile: "fake.wasm", SHA256: "any",
	}
	t.Setenv("HOME", t.TempDir())

	if err := installFromURL(context.Background(), entry, srv.URL, false); err == nil {
		t.Fatal("expected 404 error")
	}
}

func TestList_ReportsProjectAndGlobal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// t.Chdir requires go1.24; use os.Chdir + manual restore for go1.21 compat.
	orig, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
	os.MkdirAll(filepath.Join(tmp, ".kizunax/grammars"), 0755)
	os.WriteFile(filepath.Join(tmp, ".kizunax/grammars/php.wasm"), []byte("p"), 0644)

	items, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, it := range items {
		if it.Lang == "php" && it.Source == "project" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected php project entry in %+v", items)
	}
}

func TestRemove_Global(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	path := filepath.Join(tmp, ".kizunax/grammars/php.wasm")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("x"), 0644)
	if err := Remove("php", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("file still exists")
	}
}

func TestRemove_NotInstalled_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := Remove("php", false); err == nil {
		t.Fatal("expected error when grammar not installed")
	}
}

func TestRemove_UnknownLang_ReturnsError(t *testing.T) {
	if err := Remove("unknownlang", false); err == nil {
		t.Fatal("expected error for unknown lang")
	}
}
