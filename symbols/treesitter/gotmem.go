//go:build !lite

package treesitter

import _ "embed"

// GOTMemWASM is the precompiled Global Offset Table module providing
// three mutable address globals (__stack_low, __stack_high, __heap_base)
// that the runtime expects. Source: internal/symbols/treesitter/wat/got_mem.wat.
// Regenerate via scripts/rebuild-env-wasm.sh after editing the WAT.
//
//go:embed embed_assets/got_mem.wasm
var GOTMemWASM []byte
