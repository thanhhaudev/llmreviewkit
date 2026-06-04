package symbols

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrammarResolver_PrefersProjectOverGlobal(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "ws")
	globalDir := filepath.Join(tmp, "home")
	if err := os.MkdirAll(filepath.Join(projectDir, ".kizunax/grammars"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(globalDir, ".kizunax/grammars"), 0755); err != nil {
		t.Fatal(err)
	}
	projWasm := filepath.Join(projectDir, ".kizunax/grammars/php.wasm")
	globalWasm := filepath.Join(globalDir, ".kizunax/grammars/php.wasm")
	if err := os.WriteFile(projWasm, []byte("project"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalWasm, []byte("global"), 0644); err != nil {
		t.Fatal(err)
	}

	r := &GrammarResolver{WorkspaceRoot: projectDir, HomeDir: globalDir}
	got := r.Find("php")
	if got != projWasm {
		t.Errorf("expected project path %q, got %q", projWasm, got)
	}
}

func TestGrammarResolver_FallsBackToGlobal(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "ws")
	globalDir := filepath.Join(tmp, "home")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(globalDir, ".kizunax/grammars"), 0755); err != nil {
		t.Fatal(err)
	}
	globalWasm := filepath.Join(globalDir, ".kizunax/grammars/php.wasm")
	if err := os.WriteFile(globalWasm, []byte("global"), 0644); err != nil {
		t.Fatal(err)
	}

	r := &GrammarResolver{WorkspaceRoot: projectDir, HomeDir: globalDir}
	got := r.Find("php")
	if got != globalWasm {
		t.Errorf("expected global path %q, got %q", globalWasm, got)
	}
}

func TestGrammarResolver_NoneFound_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	r := &GrammarResolver{WorkspaceRoot: tmp, HomeDir: tmp}
	if got := r.Find("php"); got != "" {
		t.Errorf("expected empty path, got %q", got)
	}
}

func TestGrammarResolver_EmptyGrammarName_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	r := &GrammarResolver{WorkspaceRoot: tmp, HomeDir: tmp}
	if got := r.Find(""); got != "" {
		t.Errorf("expected empty path for empty name, got %q", got)
	}
}

func TestGrammarResolver_EmptyDirsAreSkipped(t *testing.T) {
	tmp := t.TempDir()
	// Only HomeDir set; WorkspaceRoot empty.
	globalDir := filepath.Join(tmp, "home")
	if err := os.MkdirAll(filepath.Join(globalDir, ".kizunax/grammars"), 0755); err != nil {
		t.Fatal(err)
	}
	globalWasm := filepath.Join(globalDir, ".kizunax/grammars/php.wasm")
	if err := os.WriteFile(globalWasm, []byte("global"), 0644); err != nil {
		t.Fatal(err)
	}

	r := &GrammarResolver{WorkspaceRoot: "", HomeDir: globalDir}
	got := r.Find("php")
	if got != globalWasm {
		t.Errorf("empty WorkspaceRoot should fall through to global, got %q", got)
	}
}
