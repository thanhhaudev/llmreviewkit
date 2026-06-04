//go:build !lite

package treesitter

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero/api"
)

// Capture is a single capture produced by a query execution.
type Capture struct {
	Name      string // capture name from the query (e.g. "name.definition.function")
	StartByte uint32
	EndByte   uint32
}

// Query is a compiled tree-sitter query.
type Query struct {
	lang         *Language
	queryPtr     uint32
	captureNames []string
}

// NewQuery compiles a tags.scm-style query against the language.
//
// IMPORTANT: NewQuery must be called BEFORE the first lang.Parse call on
// this Language instance. ts_parser_delete leaves a dlmalloc sentinel
// (0x5555aab8) in the free list that causes ts_query_new's internal malloc
// to trap if called after Parse+free. The package's test fixture enforces
// this order; callers must do the same.
//
// ABI notes (web-tree-sitter 0.26.9):
//   - ts_query_new(langPtr, srcPtr, srcLen, errOffsetPtr, errTypePtr) → queryPtr
//   - Error output pointers point into TRANSFER_BUFFER directly:
//     errOffsetPtr = TRANSFER_BUFFER, errTypePtr = TRANSFER_BUFFER + SIZE_OF_INT
//   - ts_query_capture_count(queryPtr) → count (plain return, no TRANSFER_BUFFER)
//   - ts_query_capture_name_for_id(queryPtr, idx, TRANSFER_BUFFER) → char* ptr;
//     length written to TRANSFER_BUFFER[0]
//
// Retry behaviour: some grammars compiled with older tree-sitter-cli versions
// (e.g. tree-sitter-typescript@0.23.2, compiled with cli 0.24.4) require two
// "warm-up" calls to ts_query_new that fail with an out-of-bounds wasm trap
// before the dlmalloc allocator is properly initialised. We silently retry up
// to maxQueryRetries times on OOB traps; the trap allocates the dlmalloc free
// list, making the subsequent attempt succeed.
// maxQueryRetries is the maximum number of ts_query_new attempts before giving
// up. Grammars compiled with older tree-sitter-cli versions need a number of
// OOB warm-up traps before dlmalloc is properly primed:
//   - typescript@0.23.2 (cli 0.24.4):  2 OOB traps then succeeds
//   - php@0.23.x (cli 0.24.x):         needs more — empirically up to ~12
// We use a generous bound because each retry only burns ~1 ms of wazero
// execution and the cost of falling back to regex is significantly higher
// (per-grammar quality regression vs v0.12.1).
const maxQueryRetries = 16

func (l *Language) NewQuery(ctx context.Context, source string) (*Query, error) {
	r := l.rt
	mem := r.mem

	// Allocate wasm memory for the source string (null-terminated).
	srcBytes := []byte(source)
	srcLen := uint64(len(srcBytes))

	mallocFn := r.rfns.malloc
	if mallocFn == nil {
		return nil, fmt.Errorf("treesitter: malloc not available")
	}
	res, err := mallocFn.Call(ctx, srcLen+1) // +1 for null terminator
	if err != nil {
		return nil, fmt.Errorf("treesitter: malloc for query source: %w", err)
	}
	srcPtr := api.DecodeU32(res[0])
	if srcPtr == 0 {
		return nil, fmt.Errorf("treesitter: malloc returned NULL for query source")
	}
	// Write source bytes + null terminator.
	mem.Write(srcPtr, append(srcBytes, 0))
	defer r.rfns.free.Call(ctx, uint64(srcPtr)) //nolint:errcheck

	// ts_query_new writes error offset/type to TRANSFER_BUFFER[0] and
	// TRANSFER_BUFFER[4] respectively. Pass those addresses directly.
	queryNewFn := r.tsMod.ExportedFunction("ts_query_new")
	if queryNewFn == nil {
		return nil, fmt.Errorf("treesitter: ts_query_new not exported")
	}

	// Retry loop: grammars compiled with older tree-sitter-cli (e.g. 0.24.x)
	// may trigger an out-of-bounds wasm trap on the first 1–2 ts_query_new calls.
	// Each trap performs enough allocations to prime the dlmalloc free list so
	// that the subsequent attempt succeeds. We retry on OOB traps only.
	var qr []uint64
	for attempt := 1; ; attempt++ {
		var callErr error
		qr, callErr = queryNewFn.Call(ctx,
			uint64(l.langPtr),
			uint64(srcPtr),
			srcLen,
			uint64(r.transferBuf),           // errOffsetPtr → TRANSFER_BUFFER
			uint64(r.transferBuf+sizeOfInt), // errTypePtr   → TRANSFER_BUFFER + 4
		)
		if callErr == nil {
			break // success
		}
		if attempt >= maxQueryRetries || !strings.Contains(callErr.Error(), "out of bounds memory access") {
			return nil, fmt.Errorf("treesitter: ts_query_new: %w", callErr)
		}
		// OOB trap primed the allocator; retry.
	}

	queryPtr := api.DecodeU32(qr[0])
	if queryPtr == 0 {
		// Read error info from TRANSFER_BUFFER.
		errOffset, _ := mem.ReadUint32Le(r.transferBuf)
		errType, _ := mem.ReadUint32Le(r.transferBuf + sizeOfInt)
		return nil, fmt.Errorf("treesitter: query parse error at byte %d (errorType %d): %s",
			errOffset, errType, source)
	}

	// Read capture names.
	captureCountFn := r.tsMod.ExportedFunction("ts_query_capture_count")
	captureNameFn := r.tsMod.ExportedFunction("ts_query_capture_name_for_id")
	if captureCountFn == nil || captureNameFn == nil {
		_, _ = r.tsMod.ExportedFunction("ts_query_delete").Call(ctx, uint64(queryPtr))
		return nil, fmt.Errorf("treesitter: ts_query_capture_count or ts_query_capture_name_for_id not exported")
	}

	cnRes, err := captureCountFn.Call(ctx, uint64(queryPtr))
	if err != nil {
		_, _ = r.tsMod.ExportedFunction("ts_query_delete").Call(ctx, uint64(queryPtr))
		return nil, fmt.Errorf("treesitter: ts_query_capture_count: %w", err)
	}
	count := api.DecodeU32(cnRes[0])
	captureNames := make([]string, count)
	for i := uint32(0); i < count; i++ {
		// ts_query_capture_name_for_id(queryPtr, idx, TRANSFER_BUFFER) → char*
		// Writes name length to TRANSFER_BUFFER[0].
		nameRes, err := captureNameFn.Call(ctx, uint64(queryPtr), uint64(i), uint64(r.transferBuf))
		if err != nil {
			continue
		}
		namePtr := api.DecodeU32(nameRes[0])
		nameLen, _ := mem.ReadUint32Le(r.transferBuf)
		if namePtr == 0 || nameLen == 0 {
			captureNames[i] = ""
			continue
		}
		buf, ok := mem.Read(namePtr, nameLen)
		if !ok {
			captureNames[i] = ""
			continue
		}
		captureNames[i] = string(buf)
	}

	return &Query{lang: l, queryPtr: queryPtr, captureNames: captureNames}, nil
}

// Close frees the query's runtime allocation.
func (q *Query) Close(ctx context.Context) error {
	if q == nil || q.queryPtr == 0 {
		return nil
	}
	fn := q.lang.rt.tsMod.ExportedFunction("ts_query_delete")
	if fn == nil {
		return nil
	}
	_, err := fn.Call(ctx, uint64(q.queryPtr))
	q.queryPtr = 0
	return err
}

// Exec runs the query over the subtree rooted at root, returning all captures
// in the order they appear.
//
// ABI notes (web-tree-sitter 0.26.9):
//   - Marshal root node to TRANSFER_BUFFER (as marshalNode does: write nodeRaw
//     bytes at TRANSFER_BUFFER + 0).
//   - ts_query_captures_wasm(queryPtr, treePtr, 0,0, 0,0, 0,0, 0,0, 0,0, 0,0,
//     matchLimit, maxStartDepth) → void; writes to TRANSFER_BUFFER:
//     [0] count i32, [4] startAddress i32 (heap ptr), [8] didExceedMatchLimit i32
//   - At startAddress: sequence of capture records, each:
//     [captureIdx i32][TSNode 5×i32 = 20 bytes]
//   - Node startByte is TSNode field[1] (bytes 4-7 within the 20-byte struct).
//   - Node endByte retrieved via ts_node_end_index_wasm(treePtr) after marshaling
//     the node's raw bytes back to TRANSFER_BUFFER.
//   - Caller owns startAddress; must call free after reading.
func (q *Query) Exec(ctx context.Context, root Node) ([]Capture, error) {
	r := q.lang.rt
	mem := r.mem

	capturesFn := r.tsMod.ExportedFunction("ts_query_captures_wasm")
	if capturesFn == nil {
		return nil, fmt.Errorf("treesitter: ts_query_captures_wasm not exported")
	}
	endIndexFn := r.tsMod.ExportedFunction("ts_node_end_index_wasm")
	if endIndexFn == nil {
		return nil, fmt.Errorf("treesitter: ts_node_end_index_wasm not exported")
	}

	// Marshal root node into TRANSFER_BUFFER (same convention as marshalNode in JS:
	// write the 20-byte TSNode struct at TRANSFER_BUFFER + 0*SIZE_OF_NODE).
	root.marshalNodeToTransferBuf()

	// Call ts_query_captures_wasm with all optional range/limit args zeroed
	// except matchLimit=4294967295 and maxStartDepth=4294967295 (no limits).
	const noLimit = uint64(0xFFFF_FFFF)
	_, err := capturesFn.Call(ctx,
		uint64(q.queryPtr),
		uint64(root.tree.treePtr),
		0, 0, // startPosition row, col
		0, 0, // endPosition row, col
		0, 0, // startIndex, endIndex
		0, 0, // startContainingPosition row, col
		0, 0, // endContainingPosition row, col
		0, 0, // startContainingIndex, endContainingIndex
		noLimit, // matchLimit
		noLimit, // maxStartDepth
	)
	if err != nil {
		return nil, fmt.Errorf("treesitter: ts_query_captures_wasm: %w", err)
	}

	// Read results from TRANSFER_BUFFER.
	count, ok := mem.ReadUint32Le(r.transferBuf)
	if !ok {
		return nil, fmt.Errorf("treesitter: read capture count from TRANSFER_BUFFER failed")
	}
	startAddr, ok := mem.ReadUint32Le(r.transferBuf + sizeOfInt)
	if !ok {
		return nil, fmt.Errorf("treesitter: read capture startAddress from TRANSFER_BUFFER failed")
	}
	// Free the heap-allocated captures buffer when done.
	if startAddr != 0 {
		defer r.rfns.free.Call(ctx, uint64(startAddr)) //nolint:errcheck
	}
	if count == 0 || startAddr == 0 {
		return nil, nil
	}

	// Each capture record in the captures mode:
	//   [patternIndex  i32]  ← skipped (we don't expose pattern index)
	//   [captureCount  i32]  ← how many captures in this match
	//   [captureIndex  i32]  ← which capture within the match is the "current" one
	//   then captureCount × [captureIdx i32][TSNode 5×i32]
	//
	// 'count' is the number of match+capture groups returned (same as JS rawCount loop).
	// We iterate exactly as the JS captures() method does.
	var captures []Capture
	addr := startAddr

	for i := uint32(0); i < count; i++ {
		patternIdx, ok := mem.ReadUint32Le(addr)
		_ = patternIdx
		if !ok {
			break
		}
		addr += sizeOfInt

		capCount, ok := mem.ReadUint32Le(addr)
		if !ok {
			break
		}
		addr += sizeOfInt

		// captureIndex: which capture within the match is highlighted
		captureIndex, ok := mem.ReadUint32Le(addr)
		if !ok {
			break
		}
		addr += sizeOfInt

		// Read all captureCount capture slots, but only emit the one at captureIndex.
		for j := uint32(0); j < capCount; j++ {
			captureIdx, ok := mem.ReadUint32Le(addr)
			if !ok {
				addr += sizeOfInt + sizeOfNode
				continue
			}
			addr += sizeOfInt

			// TSNode is sizeOfNode (20) bytes.
			nodeRaw, ok := mem.Read(addr, sizeOfNode)
			addr += sizeOfNode
			if !ok {
				continue
			}

			if j != captureIndex {
				// Not the highlighted capture for this iteration; skip.
				continue
			}

			// startByte is field[1] of TSNode (bytes 4-7).
			startByte := binary.LittleEndian.Uint32(nodeRaw[4:8])

			// endByte: marshal this node to TRANSFER_BUFFER, call ts_node_end_index_wasm.
			mem.Write(r.transferBuf, nodeRaw)
			endRes, err := endIndexFn.Call(ctx, uint64(root.tree.treePtr))
			if err != nil {
				continue
			}
			endByte := api.DecodeU32(endRes[0])

			name := ""
			if int(captureIdx) < len(q.captureNames) {
				name = q.captureNames[captureIdx]
			}

			captures = append(captures, Capture{
				Name:      name,
				StartByte: startByte,
				EndByte:   endByte,
			})
		}
	}
	return captures, nil
}
