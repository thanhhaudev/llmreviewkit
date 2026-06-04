package expand

import (
	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// buildIndex builds a tiny in-memory index from synthetic file specs.
// Used by Callers / TypeDefs tests to avoid disk I/O.
func buildIndex(files map[string]struct {
	Defs []index.Location
	Refs []index.Location
}) *index.Index {
	idx := &index.Index{
		Version: index.CurrentSchemaVersion,
		Files:   make(map[string]*index.FileIndex),
	}
	for path, fi := range files {
		idx.Files[path] = &index.FileIndex{
			Path: path,
			Lang: "go",
			Defs: fi.Defs,
			Refs: fi.Refs,
		}
	}
	idx.RebuildLookups()
	return idx
}

// mkSym is a Symbol literal builder used in test fixtures.
func mkSym(name, pkg string, kind symbols.SymbolKind) symbols.Symbol {
	return symbols.Symbol{Name: name, Pkg: pkg, Kind: kind}
}
