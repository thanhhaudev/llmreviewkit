//go:build !lite

package treesitter

import _ "embed"

// runtimeWASM is the web-tree-sitter@0.26.9 runtime binary.
// Source: https://unpkg.com/web-tree-sitter@0.26.9/web-tree-sitter.wasm.
// See embed_assets/VERSIONS.md for refresh procedure.
//
//go:embed embed_assets/web-tree-sitter.wasm
var runtimeWASM []byte
