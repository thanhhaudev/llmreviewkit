package symbols

import (
	"os"
	"path/filepath"
)

// GrammarResolver finds a tree-sitter grammar .wasm file by name,
// walking project-local then user-global paths. Returns "" if not
// found — the caller falls back to regex extraction.
//
// The resolver is plain Go — it doesn't depend on wazero/treesitter
// packages, so it compiles under both default and lite build tags.
type GrammarResolver struct {
	// WorkspaceRoot is the project root containing .kizunax/grammars/.
	// Empty disables project-local lookup.
	WorkspaceRoot string
	// HomeDir is typically os.UserHomeDir() — the user's $HOME containing
	// .kizunax/grammars/. Empty disables global lookup.
	HomeDir string
}

// Find returns the path to <grammarName>.wasm or "" if not found in
// either the project-local or user-global lookup path.
func (r *GrammarResolver) Find(grammarName string) string {
	if grammarName == "" {
		return ""
	}
	candidates := []string{}
	if r.WorkspaceRoot != "" {
		candidates = append(candidates, filepath.Join(r.WorkspaceRoot, ".kizunax/grammars", grammarName+".wasm"))
	}
	if r.HomeDir != "" {
		candidates = append(candidates, filepath.Join(r.HomeDir, ".kizunax/grammars", grammarName+".wasm"))
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// DefaultResolver constructs a resolver using the given workspace root +
// os.UserHomeDir(). Errors getting home dir are non-fatal — HomeDir is
// left empty so global lookup is disabled.
func DefaultResolver(workspaceRoot string) *GrammarResolver {
	home, _ := os.UserHomeDir()
	return &GrammarResolver{
		WorkspaceRoot: workspaceRoot,
		HomeDir:       home,
	}
}
