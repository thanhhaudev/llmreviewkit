// Package index provides a per-workspace AST index that maps symbol names
// to their definition/reference locations across the workspace. Used by
// resolver v2 (internal/resolver/v2.go) to replace regex grep with O(1)
// lookups. Persisted to ~/.kizunax/state/{ws-hash}/index/index.json.
package index

// CurrentSchemaVersion is bumped on any breaking change to the persisted
// format. When LoadJSON encounters a version != Current, the index is
// rebuilt from scratch.
const CurrentSchemaVersion = 1

// Kind classifies a symbol occurrence. Mirrors symbols.SymbolKind but kept
// local to avoid the import cycle (symbols imports diff, diff imports
// nothing that touches index).
type Kind int

const (
	SymDef Kind = iota
	SymCall
	SymImport
	SymTypeRef
)

func (k Kind) String() string {
	switch k {
	case SymDef:
		return "def"
	case SymCall:
		return "call"
	case SymImport:
		return "import"
	case SymTypeRef:
		return "type_ref"
	default:
		return "unknown"
	}
}

// Location is one occurrence of a symbol in the workspace.
type Location struct {
	Name     string `json:"name"`               // identifier
	File     string `json:"file"`               // repo-relative
	Line     int    `json:"line"`
	Kind     Kind   `json:"kind"`
	Pkg      string `json:"pkg,omitempty"`      // e.g. "app.db" for Python qualified
	Receiver string `json:"recv,omitempty"`     // e.g. "*AuthService" for Go method
}

// FileIndex aggregates everything we know about one file.
type FileIndex struct {
	Path    string     `json:"path"`
	Lang    string     `json:"lang"`            // "go" | "python" | "php" | "typescript"
	Mtime   int64      `json:"mtime"`           // unix nanos at last scan
	Hash    string     `json:"hash,omitempty"`  // optional sha256-8 for rename detection
	Defs    []Location `json:"defs"`
	Refs    []Location `json:"refs"`
	Imports []string   `json:"imports"`
}

// Index is the per-workspace global view.
type Index struct {
	Version int                   `json:"v"`
	Root    string                `json:"root"`
	Built   int64                 `json:"built"`  // unix nanos of last full build
	Files   map[string]*FileIndex `json:"files"`
	// Derived on Load; not serialized.
	bySymbol map[string][]Location `json:"-"`
}

// RebuildLookups (re)builds the bySymbol derived map. Call after LoadJSON
// or after any mutation of Files. Idempotent.
func (idx *Index) RebuildLookups() {
	idx.bySymbol = make(map[string][]Location)
	for _, fi := range idx.Files {
		for _, loc := range fi.Defs {
			idx.bySymbol[loc.Name] = append(idx.bySymbol[loc.Name], loc)
		}
		for _, loc := range fi.Refs {
			idx.bySymbol[loc.Name] = append(idx.bySymbol[loc.Name], loc)
		}
	}
}

// LookupDefs returns all definition Locations for the given symbol name.
// Pkg filtering is asymmetric on purpose: a Location with Pkg="" carries
// no package info (e.g. Go AST extractor records definitions without
// recording the containing package), so it matches ANY caller pkg. We
// only drop a candidate when BOTH the caller's pkg and the Location's
// pkg are non-empty AND disagree — this restores v1 regex-grep loose
// match behavior while still letting fully-qualified calls (caller
// pkg="store", def pkg="store") prefer same-pkg matches.
func (idx *Index) LookupDefs(name, pkg string) []Location {
	if idx.bySymbol == nil {
		return nil
	}
	var out []Location
	for _, loc := range idx.bySymbol[name] {
		if loc.Kind != SymDef {
			continue
		}
		if pkg != "" && loc.Pkg != "" && loc.Pkg != pkg {
			continue
		}
		out = append(out, loc)
	}
	return out
}

// LookupRefs returns all non-def Locations (SymCall, SymTypeRef) for the
// given symbol name. Pkg filtering uses the same asymmetric rule as
// LookupDefs — see that function's comment.
func (idx *Index) LookupRefs(name, pkg string) []Location {
	if idx.bySymbol == nil {
		return nil
	}
	var out []Location
	for _, loc := range idx.bySymbol[name] {
		if loc.Kind == SymDef || loc.Kind == SymImport {
			continue
		}
		if pkg != "" && loc.Pkg != "" && loc.Pkg != pkg {
			continue
		}
		out = append(out, loc)
	}
	return out
}

// Healthy reports whether the index is loaded and ready to query.
func (idx *Index) Healthy() bool {
	return idx != nil && idx.Version == CurrentSchemaVersion && idx.bySymbol != nil
}
