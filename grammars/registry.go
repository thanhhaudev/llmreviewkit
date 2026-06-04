//go:build !lite

// Package grammars holds the user-loaded grammar registry and the
// install/list/remove operations backing the kizunax grammars subcommand.
package grammars

import "fmt"

// Entry describes one supported language's grammar source.
type Entry struct {
	Lang        string // user-facing key (e.g. "php")
	GrammarName string // matches wasmGrammarNameFor() value used by symbols/treesitter
	NpmPackage  string // npm package name (e.g. "tree-sitter-php")
	Version     string // pinned version
	WasmFile    string // path inside the npm tarball
	SHA256      string // hex-encoded expected SHA256 of the .wasm
}

// CDNUrl returns the unpkg.com URL for this entry.
func (e Entry) CDNUrl() string {
	return fmt.Sprintf("https://unpkg.com/%s@%s/%s", e.NpmPackage, e.Version, e.WasmFile)
}

// Registry maps user-facing lang keys to grammar source descriptors.
// v0.12.2 ships entries for PHP, TypeScript, Python. Adding a language:
//
//  1. Add an entry here with a verified SHA256 (compute via curl + sha256sum
//     against a freshly-downloaded copy from npm).
//  2. Add the query const + queryForGrammar entry in internal/symbols/wasm.go.
//  3. Add the grammar name in symbols.wasmGrammarNameFor.
//
// SHA256 values must be regenerated whenever Version changes. Real SHA256
// values for the v0.12.2 pinned versions are populated in Task 14 by
// fetching + hashing the actual binaries.
var Registry = map[string]Entry{
	"php": {
		Lang: "php", GrammarName: "php",
		NpmPackage: "tree-sitter-php", Version: "0.23.10",
		WasmFile: "tree-sitter-php.wasm",
		SHA256:   "36830bfcd25a58a2840f18f94e442e0a5f02030a1d063349c19cf97803508ce6",
	},
	"typescript": {
		Lang: "typescript", GrammarName: "typescript",
		NpmPackage: "tree-sitter-typescript", Version: "0.23.2",
		WasmFile: "tree-sitter-typescript.wasm",
		SHA256:   "778025db5a8be0e70f8ccc3671e486dfeddd048c25d9e8a70c26de2e1bf6f97d",
	},
	"python": {
		Lang: "python", GrammarName: "python",
		NpmPackage: "tree-sitter-python", Version: "0.23.6",
		WasmFile: "tree-sitter-python.wasm",
		SHA256:   "8c93692fb368e288a5824cee55773c9b3602804f513bda48c97661e52e9c2da2",
	},
}
