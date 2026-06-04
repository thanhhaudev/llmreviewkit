//go:build !lite

package treesitter

import _ "embed"

// EnvWASM is the precompiled env module providing memory, table,
// globals, and function re-exports for both the runtime and the
// grammar side modules. Source: internal/symbols/treesitter/wat/env.wat.
// Regenerate via scripts/rebuild-env-wasm.sh after editing the WAT.
//
//go:embed embed_assets/env.wasm
var EnvWASM []byte
