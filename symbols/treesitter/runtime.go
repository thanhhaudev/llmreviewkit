//go:build !lite

// Package treesitter wraps the web-tree-sitter runtime via wazero to
// extract structured symbols from non-Go source files. The runtime is
// a process-wide singleton instantiated lazily.
package treesitter

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	api "github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Runtime holds the wazero runtime + the instantiated tree-sitter
// module. Use getRuntime to obtain the process-wide singleton.
//
// Tasks 3–6 complete. Task 7 adds grammar loading (Language type).
// Task 8 will add the query API (Query type).
type Runtime struct {
	wazRt       wazero.Runtime
	rfns        *runtimeFns
	tsMod       api.Module
	envMod      api.Module // current "env" module (may be replaced during grammar loads)
	mem         api.Memory // shared memory from mem_owner; stable across env module swaps
	transferBuf uint32     // address returned by ts_init(); shared TRANSFER_BUFFER for all _wasm calls
}

// parseSrc holds the source bytes for the current in-progress parse.
// Protected by parseSrcMu; safe because the runtime is a process-wide
// singleton and we serialize parse calls.
var (
	parseSrcMu  sync.Mutex
	parseSrcBuf []byte
)

var (
	runtimeOnce sync.Once
	runtimeInst *Runtime
	runtimeErr  error
)

// getRuntime returns the process-wide tree-sitter runtime singleton.
// First call lazily constructs the runtime; subsequent calls return
// the cached instance (or the cached error if first call failed).
//
// NOTE: Singleton is preserved for test code and any caller that
// explicitly opts in. Production extraction in internal/symbols uses
// NewRuntime to obtain a fresh, isolated runtime per grammar — this
// avoids dlmalloc cross-contamination between grammars (see Fix 1 in
// v0.12.2 for the PHP-after-TS ts_query_new OOB regression).
func getRuntime(ctx context.Context) (*Runtime, error) {
	runtimeOnce.Do(func() {
		runtimeInst, runtimeErr = newRuntime(ctx)
	})
	return runtimeInst, runtimeErr
}

// NewRuntime constructs a fresh, isolated wazero runtime with all the
// tree-sitter modules instantiated. Each call returns a brand-new
// runtime — no sharing with other callers. Cost: ~90 ms cold start.
//
// Why: web-tree-sitter 0.26.9's dlmalloc allocator state inside a single
// wasm runtime is corrupted by ts_query_new OOB warm-up traps in a way
// that prevents a subsequent (different) grammar from initialising. By
// giving each grammar its own runtime we eliminate the shared dlmalloc
// state entirely. The caller is responsible for closing the runtime
// (Close) when finished, or for letting it leak until process exit (the
// review process is short-lived enough that the leak is acceptable).
func NewRuntime(ctx context.Context) (*Runtime, error) {
	return newRuntime(ctx)
}

// Close releases the underlying wazero runtime and all its instantiated
// modules. After Close the Runtime must not be used. Safe to call on a
// nil receiver.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil || r.wazRt == nil {
		return nil
	}
	err := r.wazRt.Close(ctx)
	r.wazRt = nil
	return err
}

func newRuntime(ctx context.Context) (*Runtime, error) {
	rt := wazero.NewRuntime(ctx)

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: wasi instantiate: %w", err)
	}

	rfns := &runtimeFns{}
	if err := instantiateHostModule(ctx, rt, rfns); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: host instantiate: %w", err)
	}

	// mem_owner.wasm must be instantiated FIRST. It owns the shared memory
	// and function table that all other modules (env, grammar env, runtime,
	// grammars) import. This ensures a single shared memory instance.
	if _, err := rt.InstantiateWithConfig(ctx, MemOwnerWASM,
		wazero.NewModuleConfig().WithName("mem_owner")); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: mem_owner instantiate: %w", err)
	}

	// env.wasm — imports memory/table from mem_owner, provides __memory_base=0
	// and function re-exports for the runtime module.
	envMod, err := rt.InstantiateWithConfig(ctx, EnvWASM,
		wazero.NewModuleConfig().WithName("env"))
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: env instantiate: %w", err)
	}
	if _, err := rt.InstantiateWithConfig(ctx, GOTMemWASM,
		wazero.NewModuleConfig().WithName("GOT.mem")); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: GOT.mem instantiate: %w", err)
	}

	tsMod, err := rt.InstantiateWithConfig(ctx, runtimeWASM,
		wazero.NewModuleConfig().WithName("tree-sitter"))
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: runtime instantiate: %w", err)
	}

	if err := bindRuntimeFns(rfns, tsMod); err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}

	// __wasm_apply_data_relocs must run before __wasm_call_ctors and ts_init.
	// It patches function-pointer entries in the data segment that Emscripten
	// dynamic-linking stores as integer offsets from __table_base rather than
	// direct funcref table elements.
	applyRelocs := tsMod.ExportedFunction("__wasm_apply_data_relocs")
	if applyRelocs != nil {
		if _, err := applyRelocs.Call(ctx); err != nil {
			_ = rt.Close(ctx)
			return nil, fmt.Errorf("treesitter: __wasm_apply_data_relocs: %w", err)
		}
	}

	// __wasm_call_ctors runs C++ constructors and any runtime-level
	// initialization that couldn't happen in the start section.
	ctors := tsMod.ExportedFunction("__wasm_call_ctors")
	if ctors != nil {
		if _, err := ctors.Call(ctx); err != nil {
			_ = rt.Close(ctx)
			return nil, fmt.Errorf("treesitter: __wasm_call_ctors: %w", err)
		}
	}

	tsInit := tsMod.ExportedFunction("ts_init")
	if tsInit == nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: ts_init not exported")
	}
	initRes, err := tsInit.Call(ctx)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: ts_init call: %w", err)
	}
	// ts_init() returns the TRANSFER_BUFFER address — the shared memory
	// region used by all _wasm suffix functions to pass results between
	// Go and the wasm runtime.
	transferBuf := api.DecodeU32(initRes[0])
	if transferBuf == 0 {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: ts_init returned NULL transfer buffer")
	}

	// Obtain the shared memory from mem_owner. This reference is stable even
	// if the "env" module is closed and replaced during grammar loading.
	memOwnerMod := rt.Module("mem_owner")
	if memOwnerMod == nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: mem_owner module not found after instantiation")
	}
	sharedMem := memOwnerMod.ExportedMemory("memory")
	if sharedMem == nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("treesitter: mem_owner does not export memory")
	}

	return &Runtime{wazRt: rt, rfns: rfns, tsMod: tsMod, envMod: envMod, mem: sharedMem, transferBuf: transferBuf}, nil
}

// instantiateHostModule wires up the "host" module that env.wasm imports
// from. Provides:
//   - 6 tree-sitter / emscripten callback stubs (no-ops; runtime won't
//     normally invoke them).
//   - 10 libc trampolines (via addTrampolines) that forward to runtime
//     exports via the runtimeFns late-bound struct.
//
// The trampolines fail-loud (panic) if invoked before bindRuntimeFns
// has populated rfns — but this never happens in practice because no
// grammar can run before the runtime module is up.
// GetRuntime exposes the runtime singleton to callers in other packages.
// Used by the symbols package extractors to obtain the shared wazero
// runtime that hosts loaded grammars.
func GetRuntime(ctx context.Context) (*Runtime, error) {
	return getRuntime(ctx)
}

func instantiateHostModule(ctx context.Context, rt wazero.Runtime, rfns *runtimeFns) error {
	b := rt.NewHostModuleBuilder("host").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, requestedPages int32) int32 { return 0 }).
		Export("emscripten_resize_heap").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context) {
			panic("treesitter: _abort_js invoked by runtime")
		}).
		Export("_abort_js").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, payload int32) int32 { return 0 }).
		Export("tree_sitter_query_progress_callback").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, payload, isError int32) int32 { return 0 }).
		Export("tree_sitter_progress_callback").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, inputBuf, charIndex, row, column, lenAddr int32) {
			// Serve source text to the wasm parser as UTF-16 LE.
			//
			// charIndex is a BYTE offset into the source (the wasm shim
			// func[263] converts the parser's internal UTF-16 code-unit
			// position to bytes before calling this trampoline).
			//
			// We emit ONE UTF-16 code unit per source BYTE: each byte X
			// becomes the code unit 0x00XX (Latin-1 padding). This keeps a
			// 1:1 source-byte ↔ code-unit mapping so the start/end indices
			// the parser reports back can be used directly to slice src as
			// Go bytes — no char↔byte conversion table needed.
			//
			// Bytes >= 0x80 are remapped to the benign ASCII letter 'a' so
			// the PHP / Python lexer treats them as identifier filler. The
			// 1:1 byte/code-unit position mapping is preserved — the slice
			// we take back uses the ORIGINAL bytes from parseSrcBuf, not
			// what the grammar saw, so symbol names remain bit-perfect.
			parseSrcMu.Lock()
			src := parseSrcBuf
			parseSrcMu.Unlock()
			if charIndex < 0 || int(charIndex) >= len(src) {
				mod.Memory().WriteUint32Le(uint32(lenAddr), 0)
				return
			}
			// Cap by input buffer size: 10240 bytes / 2 = 5120 code units.
			const maxUTF16Units = 5120
			remaining := src[charIndex:]
			n := len(remaining)
			if n > maxUTF16Units {
				n = maxUTF16Units
			}
			utf16Bytes := make([]byte, n*2)
			for i := 0; i < n; i++ {
				b := remaining[i]
				if b >= 0x80 {
					b = 'a'
				}
				utf16Bytes[i*2] = b
				utf16Bytes[i*2+1] = 0
			}
			mod.Memory().Write(uint32(inputBuf), utf16Bytes)
			mod.Memory().WriteUint32Le(uint32(lenAddr), uint32(n))
		}).
		Export("tree_sitter_parse_callback").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, logType, message int32) {}).
		Export("tree_sitter_log_callback")

	b = addTrampolines(b, rfns)
	_, err := b.Instantiate(ctx)
	return err
}
