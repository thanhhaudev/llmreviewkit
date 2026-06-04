package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// LangForPath returns the language tag for a file path based on extension.
// Returns empty string for files outside the 4 supported languages.
// v0.13.0 scope: go, python, php, typescript only.
func LangForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".php":
		return "php"
	case ".ts", ".tsx":
		return "typescript"
	}
	return ""
}

// ScanFile scans one file and returns its FileIndex. Returns (nil, nil)
// for files outside the 4 supported languages. Returns (nil, err) on
// stat/read error. relPath is repo-relative; absolute path is
// filepath.Join(ws, relPath).
func ScanFile(ws, relPath string) (*FileIndex, error) {
	lang := LangForPath(relPath)
	if lang == "" {
		return nil, nil
	}

	abs := filepath.Join(ws, relPath)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", relPath, err)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}

	// symbols.ForFile returns the appropriate per-language extractor
	// (Go AST / WASM walker / regex fallback). May return nil if the
	// extension is in the index's whitelist but unknown to symbols —
	// unlikely with our 4-lang scope but handle defensively.
	ex := symbols.ForFile(relPath)
	if ex == nil {
		return nil, nil
	}
	syms := ex.Extract(relPath, content)

	fi := &FileIndex{
		Path:  relPath,
		Lang:  lang,
		Mtime: info.ModTime().UnixNano(),
	}
	for _, s := range syms {
		loc := Location{
			Name: s.Name,
			File: relPath,
			Line: s.Line,
			Pkg:  s.Pkg,
			Kind: mapSymbolKind(s.Kind),
		}
		switch loc.Kind {
		case SymDef:
			fi.Defs = append(fi.Defs, loc)
		case SymImport:
			fi.Imports = append(fi.Imports, s.Name)
		default:
			fi.Refs = append(fi.Refs, loc)
		}
	}
	return fi, nil
}

// mapSymbolKind converts symbols.SymbolKind to index.Kind. The enums
// have different numeric values (symbols is iota+1, index is iota+0),
// so we map by semantic name. Unknown kinds default to SymCall.
func mapSymbolKind(k symbols.SymbolKind) Kind {
	switch k {
	case symbols.SymDef:
		return SymDef
	case symbols.SymCall:
		return SymCall
	case symbols.SymImport:
		return SymImport
	case symbols.SymTypeRef:
		return SymTypeRef
	}
	return SymCall
}

// skipDirs are directories that are never scanned. Mirrors resolver's
// shouldSkipDir + adds common per-language vendor dirs.
var skipDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".svn":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".idea":        true,
	".vscode":      true,
}

// WalkWorkspace returns repo-relative paths of all files in ws that match
// a supported language (LangForPath != ""). Walks once, skipping
// well-known vendor/build directories. Lexical (filepath.WalkDir) order.
func WalkWorkspace(ws string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(ws, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // best-effort, skip unreadable
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != ws {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(ws, path)
		if err != nil {
			return nil
		}
		if LangForPath(rel) == "" {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
