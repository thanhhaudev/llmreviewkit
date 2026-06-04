//go:build !lite

package treesitter

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// sizeOfInt is the size of an i32 in wasm memory.
const sizeOfInt = 4

// sizeOfNode is the size of a TSNode struct in wasm memory: 5 × i32 = 20 bytes.
// Layout (matches web-tree-sitter binding.ts):
//
//	[0] id         i32
//	[1] startIndex i32
//	[2] startRow   i32
//	[3] startColumn i32
//	[4] other      i32  (internal context word)
const sizeOfNode = 5 * sizeOfInt

// Language wraps a loaded tree-sitter grammar (a wazero module + the
// language pointer returned by tree_sitter_<name>).
//
// Lifecycle: call Close when the Language is no longer needed to release
// the underlying grammar side module. After Close the Language must not
// be used for parsing.
type Language struct {
	rt         *Runtime
	name       string
	grammarMod api.Module
	langPtr    uint32
}

// LoadGrammar instantiates a grammar side module and obtains its language
// pointer. The grammarName is used both as the wazero module instance name
// AND as the suffix for the exported tree_sitter_<name> function
// (e.g. "php" → tree_sitter_php).
//
// ABI notes (web-tree-sitter 0.26.9):
//   - Grammar side modules use the Emscripten dynamic-linking ABI: they
//     import from "env" (memory, table, libc trampolines) and expect
//     env.__memory_base to point past the runtime's data region.
//   - Before instantiating each grammar, we close the current "env" module
//     and replace it with env_grammar.wasm (__memory_base=11712, __table_base=30)
//     so the grammar's active data segment does not overwrite the runtime's data at
//     address 0, and the grammar's table entries don't overwrite the runtime's
//     function table entries at positions 0–29. Both env variants import memory
//     and table from "mem_owner" so all modules share a single wasm memory/table.
//   - After grammar loading, "env" is restored to env.wasm (__memory_base=0).
//   - __wasm_apply_data_relocs must run before __wasm_call_ctors.
//   - tree_sitter_<name>() returns the TSLanguage* as an i32 result.
func (r *Runtime) LoadGrammar(ctx context.Context, grammarName string, wasmBytes []byte) (*Language, error) {
	// Swap "env" to the grammar variant (__memory_base=11712) so the grammar's
	// active data segment doesn't overwrite the runtime's data at address 0.
	// Both env variants share the same memory via "mem_owner".
	if err := r.envMod.CloseWithExitCode(ctx, 0); err != nil {
		return nil, fmt.Errorf("treesitter: close env for grammar %q: %w", grammarName, err)
	}
	grammarEnv, err := r.wazRt.InstantiateWithConfig(ctx, EnvGrammarWASM,
		wazero.NewModuleConfig().WithName("env"))
	if err != nil {
		// Restore original env so the runtime stays usable.
		restoredEnv, _ := r.wazRt.InstantiateWithConfig(ctx, EnvWASM,
			wazero.NewModuleConfig().WithName("env"))
		if restoredEnv != nil {
			r.envMod = restoredEnv
		}
		return nil, fmt.Errorf("treesitter: load grammar env for %q: %w", grammarName, err)
	}

	mod, err := r.wazRt.InstantiateWithConfig(ctx, wasmBytes,
		wazero.NewModuleConfig().WithName(fmt.Sprintf("grammar_%s", grammarName)))

	// Always restore "env" to the runtime variant (__memory_base=0) regardless
	// of whether grammar loading succeeded.
	_ = grammarEnv.CloseWithExitCode(ctx, 0)
	restoredEnv, restoreErr := r.wazRt.InstantiateWithConfig(ctx, EnvWASM,
		wazero.NewModuleConfig().WithName("env"))
	if restoreErr != nil {
		return nil, fmt.Errorf("treesitter: restore env after grammar %q: %w", grammarName, restoreErr)
	}
	r.envMod = restoredEnv

	if err != nil {
		return nil, fmt.Errorf("treesitter: load grammar %q: %w", grammarName, err)
	}

	// __wasm_apply_data_relocs patches function-pointer relocations in the
	// data segment; must run before __wasm_call_ctors.
	if relocs := mod.ExportedFunction("__wasm_apply_data_relocs"); relocs != nil {
		if _, err := relocs.Call(ctx); err != nil {
			return nil, fmt.Errorf("treesitter: grammar %q __wasm_apply_data_relocs: %w", grammarName, err)
		}
	}
	if ctors := mod.ExportedFunction("__wasm_call_ctors"); ctors != nil {
		if _, err := ctors.Call(ctx); err != nil {
			return nil, fmt.Errorf("treesitter: grammar %q ctors: %w", grammarName, err)
		}
	}
	exp := fmt.Sprintf("tree_sitter_%s", grammarName)
	langFn := mod.ExportedFunction(exp)
	if langFn == nil {
		return nil, fmt.Errorf("treesitter: grammar %q missing %s export", grammarName, exp)
	}
	res, err := langFn.Call(ctx)
	if err != nil {
		return nil, fmt.Errorf("treesitter: grammar %q lang call: %w", grammarName, err)
	}
	langPtr := api.DecodeU32(res[0])

	return &Language{
		rt:         r,
		name:       grammarName,
		grammarMod: mod,
		langPtr:    langPtr,
	}, nil
}

// Close releases the grammar side module. After Close the Language
// must not be used. Idempotent (returns nil if already closed).
func (l *Language) Close(ctx context.Context) error {
	if l == nil || l.grammarMod == nil {
		return nil
	}
	err := l.grammarMod.Close(ctx)
	l.grammarMod = nil
	return err
}

// Tree is a parsed tree-sitter tree. Always Close after use to free
// wasm-side memory.
type Tree struct {
	lang    *Language
	treePtr uint32
}

// Parse parses src using the grammar's language. Returns an error if the
// runtime call fails or the parser returns a NULL tree.
//
// ABI notes (web-tree-sitter 0.26.9):
//   - ts_parser_new_wasm() returns void and writes two i32 values to
//     TRANSFER_BUFFER: [0]=parserPtr, [4]=parseCallbackPtr (a function
//     table index used for the text-feed trampoline).
//   - ts_parser_parse_wasm(parserPtr, parseCallbackPtr, oldTreePtr,
//     rangeAddr, rangeCount) returns the tree i32 directly.
//   - Source bytes are served by the tree_sitter_parse_callback host
//     function registered in the "host" module, which reads from the
//     parseSrcBuf package global.
//   - Parse operations are serialized via parseSrcMu (the runtime is a
//     process-wide singleton).
func (l *Language) Parse(ctx context.Context, src []byte) (*Tree, error) {
	r := l.rt
	mem := r.mem
	if mem == nil {
		return nil, fmt.Errorf("treesitter: runtime memory not available")
	}

	// Required exports
	parserNewFn := r.tsMod.ExportedFunction("ts_parser_new_wasm")
	setLangFn := r.tsMod.ExportedFunction("ts_parser_set_language")
	parseFn := r.tsMod.ExportedFunction("ts_parser_parse_wasm")
	deleteFn := r.tsMod.ExportedFunction("ts_parser_delete")
	if parserNewFn == nil || setLangFn == nil || parseFn == nil || deleteFn == nil {
		return nil, fmt.Errorf("treesitter: missing required parser exports (new=%v, setLang=%v, parse=%v, delete=%v)",
			parserNewFn != nil, setLangFn != nil, parseFn != nil, deleteFn != nil)
	}

	// Guard parseSrcBuf only; concurrent Parse() calls are not safe
	// because they share Runtime.transferBuf and the wazero module state.
	// Caller must serialize Parse for the same Runtime.
	parseSrcMu.Lock()
	parseSrcBuf = src
	parseSrcMu.Unlock()
	defer func() {
		parseSrcMu.Lock()
		parseSrcBuf = nil
		parseSrcMu.Unlock()
	}()

	// ts_parser_new_wasm() → void; writes to TRANSFER_BUFFER:
	//   [0] = parserPtr (i32)
	//   [4] = parseCallbackPtr (i32) — function-table index for the text trampoline
	if _, err := parserNewFn.Call(ctx); err != nil {
		return nil, fmt.Errorf("treesitter: ts_parser_new_wasm: %w", err)
	}
	parserPtr, ok := mem.ReadUint32Le(r.transferBuf)
	if !ok {
		return nil, fmt.Errorf("treesitter: read parserPtr from TRANSFER_BUFFER failed")
	}
	if parserPtr == 0 {
		return nil, fmt.Errorf("treesitter: ts_parser_new_wasm returned NULL parser")
	}
	parseCallbackPtr, ok := mem.ReadUint32Le(r.transferBuf + sizeOfInt)
	if !ok {
		return nil, fmt.Errorf("treesitter: read parseCallbackPtr from TRANSFER_BUFFER failed")
	}

	// ts_parser_set_language(parserPtr, langPtr) → i32 (bool; non-zero = success)
	slRes, err := setLangFn.Call(ctx, uint64(parserPtr), uint64(l.langPtr))
	if err != nil {
		_, _ = deleteFn.Call(ctx, uint64(parserPtr))
		return nil, fmt.Errorf("treesitter: ts_parser_set_language: %w", err)
	}
	if api.DecodeI32(slRes[0]) == 0 {
		_, _ = deleteFn.Call(ctx, uint64(parserPtr))
		return nil, fmt.Errorf("treesitter: ts_parser_set_language returned false (version mismatch?)")
	}

	// ts_parser_parse_wasm(parserPtr, parseCallbackPtr, oldTreePtr=0, rangeAddr=0, rangeCount=0)
	// → i32 (treePtr; 0 = parse failed)
	parseRes, err := parseFn.Call(ctx,
		uint64(parserPtr),
		uint64(parseCallbackPtr),
		0, // oldTree
		0, // rangeAddr
		0, // rangeCount
	)
	if err != nil {
		_, _ = deleteFn.Call(ctx, uint64(parserPtr))
		return nil, fmt.Errorf("treesitter: ts_parser_parse_wasm: %w", err)
	}
	treePtr := api.DecodeU32(parseRes[0])
	if treePtr == 0 {
		_, _ = deleteFn.Call(ctx, uint64(parserPtr))
		return nil, fmt.Errorf("treesitter: ts_parser_parse_wasm returned NULL tree")
	}

	// Parser is a transient resource; free it now (the tree owns the result).
	_, _ = deleteFn.Call(ctx, uint64(parserPtr))

	return &Tree{lang: l, treePtr: treePtr}, nil
}

// Close releases the tree's wasm-side memory.
func (t *Tree) Close(ctx context.Context) error {
	deleteFn := t.lang.rt.tsMod.ExportedFunction("ts_tree_delete")
	if deleteFn == nil {
		return fmt.Errorf("treesitter: ts_tree_delete not exported")
	}
	if _, err := deleteFn.Call(ctx, uint64(t.treePtr)); err != nil {
		return fmt.Errorf("treesitter: ts_tree_delete: %w", err)
	}
	return nil
}

// Node is a transient handle to a tree-sitter node. The node's 5-word
// TSNode struct is stored inline (not a wasm memory pointer).
//
// TSNode layout in wasm TRANSFER_BUFFER (5 × i32 = 20 bytes):
//
//	[0]  id          — non-zero for valid node
//	[4]  startIndex
//	[8]  startRow
//	[12] startColumn
//	[16] other       — internal context word
type Node struct {
	tree    *Tree
	nodeRaw [sizeOfNode]byte // raw 20-byte TSNode struct read from TRANSFER_BUFFER
}

// RootNode returns the tree's root node.
//
// ABI: ts_tree_root_node_wasm(treePtr) → void; writes 5-word TSNode to
// TRANSFER_BUFFER. We copy those 20 bytes into Node.nodeRaw so that
// subsequent node operations can marshal back to TRANSFER_BUFFER.
func (t *Tree) RootNode(ctx context.Context) Node {
	r := t.lang.rt
	rootFn := r.tsMod.ExportedFunction("ts_tree_root_node_wasm")
	if rootFn == nil {
		panic("treesitter: ts_tree_root_node_wasm not exported")
	}
	if _, err := rootFn.Call(ctx, uint64(t.treePtr)); err != nil {
		panic(fmt.Errorf("treesitter: ts_tree_root_node_wasm: %w", err))
	}
	n := Node{tree: t}
	mem := r.mem
	raw, ok := mem.Read(r.transferBuf, sizeOfNode)
	if ok {
		copy(n.nodeRaw[:], raw)
	}
	return n
}

// marshalNodeToTransferBuf writes the node's raw TSNode struct back into
// TRANSFER_BUFFER so that the _wasm node query functions can read it.
func (n Node) marshalNodeToTransferBuf() {
	r := n.tree.lang.rt
	r.mem.Write(r.transferBuf, n.nodeRaw[:])
}

// Type returns the node's type as a string (e.g. "program", "function_declaration").
//
// ABI: marshal node to TRANSFER_BUFFER, call
// ts_node_symbol_wasm(treePtr) → i32 (symbol id), then call
// ts_language_symbol_name(langPtr, symbolId) → i32 (char* in wasm memory).
// Read the null-terminated string from wasm memory.
func (n Node) Type(ctx context.Context) string {
	r := n.tree.lang.rt
	symbolFn := r.tsMod.ExportedFunction("ts_node_symbol_wasm")
	symNameFn := r.tsMod.ExportedFunction("ts_language_symbol_name")
	if symbolFn == nil || symNameFn == nil {
		return ""
	}

	n.marshalNodeToTransferBuf()
	symRes, err := symbolFn.Call(ctx, uint64(n.tree.treePtr))
	if err != nil {
		return ""
	}
	symbolID := api.DecodeI32(symRes[0])

	nameRes, err := symNameFn.Call(ctx, uint64(n.tree.lang.langPtr), uint64(uint32(symbolID)))
	if err != nil {
		return ""
	}
	strPtr := api.DecodeU32(nameRes[0])
	if strPtr == 0 {
		return ""
	}

	// Read null-terminated C string from wasm memory.
	mem := r.mem
	var bs []byte
	for off := strPtr; ; off++ {
		b, ok := mem.ReadByte(off)
		if !ok || b == 0 {
			break
		}
		bs = append(bs, b)
	}
	return string(bs)
}

// ChildCount returns the number of children of n.
//
// ABI: marshal node to TRANSFER_BUFFER, call
// ts_node_child_count_wasm(treePtr) → i32.
func (n Node) ChildCount(ctx context.Context) uint32 {
	r := n.tree.lang.rt
	fn := r.tsMod.ExportedFunction("ts_node_child_count_wasm")
	if fn == nil {
		return 0
	}
	n.marshalNodeToTransferBuf()
	res, err := fn.Call(ctx, uint64(n.tree.treePtr))
	if err != nil {
		return 0
	}
	return api.DecodeU32(res[0])
}
