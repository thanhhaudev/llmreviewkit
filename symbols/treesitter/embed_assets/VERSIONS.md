# Embedded WASM provenance

This directory contains binary artifacts. Sources of truth:

| File | Provenance | Version | Size |
| --- | --- | --- | --- |
| web-tree-sitter.wasm | Downloaded from https://unpkg.com/web-tree-sitter@0.26.9/web-tree-sitter.wasm | 0.26.9 | ~200 KB |
| env.wasm | Compiled from `../wat/env.wat` via `scripts/rebuild-env-wasm.sh` | tracked-in-repo | ~420-800 B |
| got_mem.wasm | Compiled from `../wat/got_mem.wat` via `scripts/rebuild-env-wasm.sh` | tracked-in-repo | ~60-80 B |
| mem_owner.wasm | Compiled from `../wat/mem_owner.wat` via `scripts/rebuild-env-wasm.sh` | tracked-in-repo | 64 B |
| env_grammar.wasm | Compiled from `../wat/env_grammar.wat` via `scripts/rebuild-env-wasm.sh` | tracked-in-repo | 871 B |

To refresh `web-tree-sitter.wasm` (e.g. on a tree-sitter security release):

    curl -sSL -o web-tree-sitter.wasm "https://unpkg.com/web-tree-sitter@<NEW>/web-tree-sitter.wasm"

Update this file's version row and run the smoke tests in `runtime_test.go`.

To refresh `env.wasm` / `got_mem.wasm` / `mem_owner.wasm` / `env_grammar.wasm` after editing WAT sources:

    ./scripts/rebuild-env-wasm.sh
