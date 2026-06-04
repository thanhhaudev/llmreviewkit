//go:build !lite

package treesitter

import _ "embed"

// MemOwnerWASM is the precompiled mem_owner module that owns the shared
// memory and function table. Both env.wasm and env_grammar.wasm import
// from this module so that all wasm modules (runtime + grammars) share
// the same memory instance.
// Source: internal/symbols/treesitter/wat/mem_owner.wat.
// Regenerate via scripts/rebuild-env-wasm.sh after editing the WAT.
//
//go:embed embed_assets/mem_owner.wasm
var MemOwnerWASM []byte

// EnvGrammarWASM is the precompiled grammar env module — same as env.wasm
// but with __memory_base = 11712 (past the runtime's data region). Grammar
// side modules import from this module so their data segments don't
// overwrite the runtime's data at address 0.
// Source: internal/symbols/treesitter/wat/env_grammar.wat.
// Regenerate via scripts/rebuild-env-wasm.sh after editing the WAT.
//
//go:embed embed_assets/env_grammar.wasm
var EnvGrammarWASM []byte
