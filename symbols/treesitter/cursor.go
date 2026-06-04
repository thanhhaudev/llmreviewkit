//go:build !lite

package treesitter

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// NodeDef is a simple (nameStart, nameEnd) byte range returned by
// WalkNamedChildren for each matching node's name-field child.
type NodeDef struct {
	NameStart uint32
	NameEnd   uint32
}

// RawNode is a node visited by WalkAllNamedNodes. TypeID is the named symbol
// id (matches Language.SymbolIDForName). NodeRaw is the 20-byte TSNode struct
// captured from TRANSFER_BUFFER while the cursor pointed at the node, suitable
// for re-marshalling into TRANSFER_BUFFER for follow-up calls like
// ChildByFieldID or NodeEndIndex.
type RawNode struct {
	TypeID    uint16
	NodeRaw   [sizeOfNode]byte
	StartByte uint32
	EndByte   uint32
}

// FieldIDForName looks up a field name's numeric ID in the language's field
// table. Returns 0 if not found (field IDs start at 1).
func (l *Language) FieldIDForName(ctx context.Context, fieldName string) uint32 {
	r := l.rt
	fieldCountFn := r.tsMod.ExportedFunction("ts_language_field_count")
	fieldNameFn := r.tsMod.ExportedFunction("ts_language_field_name_for_id")
	if fieldCountFn == nil || fieldNameFn == nil {
		return 0
	}

	res, err := fieldCountFn.Call(ctx, uint64(l.langPtr))
	if err != nil {
		return 0
	}
	count := api.DecodeU32(res[0])

	for i := uint32(1); i <= count; i++ {
		nameRes, err := fieldNameFn.Call(ctx, uint64(l.langPtr), uint64(i))
		if err != nil {
			continue
		}
		namePtr := api.DecodeU32(nameRes[0])
		if namePtr == 0 {
			continue
		}
		var bs []byte
		for off := namePtr; ; off++ {
			b, ok := r.mem.ReadByte(off)
			if !ok || b == 0 {
				break
			}
			bs = append(bs, b)
		}
		if string(bs) == fieldName {
			return i
		}
	}
	return 0
}

// SymbolIDForName looks up a node type's numeric ID in the language's symbol
// table. Returns 0 if not found. Pass named=true to search named symbols only.
func (l *Language) SymbolIDForName(ctx context.Context, name string, named bool) uint16 {
	r := l.rt
	fn := r.tsMod.ExportedFunction("ts_language_symbol_for_name")
	if fn == nil {
		return 0
	}

	bs := append([]byte(name), 0)
	mallocRes, err := r.rfns.malloc.Call(ctx, uint64(len(bs)))
	if err != nil {
		return 0
	}
	ptr := api.DecodeU32(mallocRes[0])
	defer r.rfns.free.Call(ctx, uint64(ptr)) //nolint:errcheck
	r.mem.Write(ptr, bs)

	namedInt := uint64(0)
	if named {
		namedInt = 1
	}
	res, err := fn.Call(ctx, uint64(l.langPtr), uint64(ptr), uint64(len(name)), namedInt)
	if err != nil {
		return 0
	}
	return uint16(api.DecodeU32(res[0]))
}

// sizeOfCursor is the size of the cursor handle that the web-tree-sitter
// JS binding marshals into TRANSFER_BUFFER: 4 × i32 = 16 bytes. Verified
// against web-tree-sitter@0.26.9 marshalTreeCursor/unmarshalTreeCursor:
//
//	function marshalTreeCursor(cursor, address = TRANSFER_BUFFER) {
//	  C.setValue(address + 0*SIZE_OF_INT, cursor[0], "i32");
//	  C.setValue(address + 1*SIZE_OF_INT, cursor[1], "i32");
//	  C.setValue(address + 2*SIZE_OF_INT, cursor[2], "i32");
//	  C.setValue(address + 3*SIZE_OF_INT, cursor[3], "i32");
//	}
//
// Reading only 12 bytes (the public TSTreeCursor C struct size of
// tree + id + 3 u32 context — 20 bytes — divided by something) drops
// either the cursor's "tree" pointer or one of its context words and the
// next ts_tree_cursor_* call dereferences garbage, walking the wrong part
// of the tree and reporting incorrect byte offsets ("off by 2" symptom).
const sizeOfCursor = 16

// WalkNamedChildren walks all named nodes matching one of the given symbol IDs
// using a depth-first tree cursor and calls fn for each matching node to get
// its name-field child's byte range. Only nodes with a non-empty name child
// are reported.
//
// If nameFieldID > 0, for each matching node, the "name" field child is looked
// up and its byte range is returned. If nameFieldID == 0, the node's own byte
// range is used.
//
// Returns a slice of NodeDef for each matching (name start/end byte) found.
// Avoids ts_query_new entirely — safe to call in any runtime state.
func (l *Language) WalkNamedChildren(ctx context.Context, tree *Tree, matchSymIDs []uint16, nameFieldID uint32) ([]NodeDef, error) {
	r := l.rt
	mem := r.mem

	cursorNewFn := r.tsMod.ExportedFunction("ts_tree_cursor_new_wasm")
	cursorDeleteFn := r.tsMod.ExportedFunction("ts_tree_cursor_delete_wasm")
	cursorFirstChildFn := r.tsMod.ExportedFunction("ts_tree_cursor_goto_first_child_wasm")
	cursorNextSiblingFn := r.tsMod.ExportedFunction("ts_tree_cursor_goto_next_sibling_wasm")
	cursorParentFn := r.tsMod.ExportedFunction("ts_tree_cursor_goto_parent_wasm")
	cursorCurrentNodeFn := r.tsMod.ExportedFunction("ts_tree_cursor_current_node_wasm")
	cursorTypeIDFn := r.tsMod.ExportedFunction("ts_tree_cursor_current_node_type_id_wasm")
	childByFieldFn := r.tsMod.ExportedFunction("ts_node_child_by_field_id_wasm")
	endIndexFn := r.tsMod.ExportedFunction("ts_node_end_index_wasm")

	if cursorNewFn == nil || cursorDeleteFn == nil || cursorFirstChildFn == nil ||
		cursorNextSiblingFn == nil || cursorParentFn == nil || cursorCurrentNodeFn == nil ||
		cursorTypeIDFn == nil || endIndexFn == nil {
		return nil, fmt.Errorf("treesitter: required cursor functions not exported")
	}

	// Place root node in TRANSFER_BUFFER and create cursor.
	root := tree.RootNode(ctx)
	root.marshalNodeToTransferBuf()
	if _, err := cursorNewFn.Call(ctx, uint64(tree.treePtr)); err != nil {
		return nil, fmt.Errorf("treesitter: ts_tree_cursor_new_wasm: %w", err)
	}

	// Save cursor state (16 bytes at TRANSFER_BUFFER).
	cursor := make([]byte, sizeOfCursor)
	cursorRaw, ok := mem.Read(r.transferBuf, sizeOfCursor)
	if !ok {
		// Cursor was allocated but we can't snapshot it; free it via the
		// handle still sitting in TRANSFER_BUFFER, then bail.
		cursorDeleteFn.Call(ctx) //nolint:errcheck
		return nil, fmt.Errorf("treesitter: read cursor from TRANSFER_BUFFER failed")
	}
	copy(cursor, cursorRaw)

	// Delete cursor on return.
	defer func() {
		mem.Write(r.transferBuf, cursor)
		cursorDeleteFn.Call(ctx) //nolint:errcheck
	}()

	// Build a set of matching symbol IDs for fast lookup.
	matchSet := make(map[uint16]bool, len(matchSymIDs))
	for _, id := range matchSymIDs {
		matchSet[id] = true
	}

	var results []NodeDef
	depth := 0

	for {
		// Restore cursor into TRANSFER_BUFFER.
		mem.Write(r.transferBuf, cursor)

		// Get current node type ID.
		typeRes, err := cursorTypeIDFn.Call(ctx, uint64(tree.treePtr))
		if err != nil {
			break
		}
		typeID := uint16(api.DecodeU32(typeRes[0]))

		if matchSet[typeID] {
			// Get current node raw bytes.
			mem.Write(r.transferBuf, cursor)
			if _, err := cursorCurrentNodeFn.Call(ctx, uint64(tree.treePtr)); err == nil {
				nodeRaw, ok := mem.Read(r.transferBuf, sizeOfNode)
				if ok {
					if nameFieldID > 0 && childByFieldFn != nil {
						// Navigate to the "name" field child.
						mem.Write(r.transferBuf, nodeRaw)
						if _, err := childByFieldFn.Call(ctx, uint64(tree.treePtr), uint64(nameFieldID)); err == nil {
							nameNodeRaw, ok := mem.Read(r.transferBuf, sizeOfNode)
							if ok {
								nameStart := binary.LittleEndian.Uint32(nameNodeRaw[4:8])
								mem.Write(r.transferBuf, nameNodeRaw)
								endRes, err := endIndexFn.Call(ctx, uint64(tree.treePtr))
								if err == nil {
									nameEnd := api.DecodeU32(endRes[0])
									if nameStart < nameEnd {
										results = append(results, NodeDef{NameStart: nameStart, NameEnd: nameEnd})
									}
								}
							}
						}
					} else {
						// Use node's own byte range.
						nodeStart := binary.LittleEndian.Uint32(nodeRaw[4:8])
						mem.Write(r.transferBuf, nodeRaw)
						endRes, err := endIndexFn.Call(ctx, uint64(tree.treePtr))
						if err == nil {
							nodeEnd := api.DecodeU32(endRes[0])
							if nodeStart < nodeEnd {
								results = append(results, NodeDef{NameStart: nodeStart, NameEnd: nodeEnd})
							}
						}
					}
				}
			}
		}

		// Restore cursor for navigation.
		mem.Write(r.transferBuf, cursor)

		// Try to go to first child.
		childRes, err := cursorFirstChildFn.Call(ctx, uint64(tree.treePtr))
		if err == nil && api.DecodeI32(childRes[0]) != 0 {
			cursorRaw, ok = mem.Read(r.transferBuf, sizeOfCursor)
			if !ok {
				break
			}
			copy(cursor, cursorRaw)
			depth++
			continue
		}

		// No children — try next sibling.
		for {
			mem.Write(r.transferBuf, cursor)
			sibRes, err := cursorNextSiblingFn.Call(ctx, uint64(tree.treePtr))
			if err == nil && api.DecodeI32(sibRes[0]) != 0 {
				cursorRaw, ok = mem.Read(r.transferBuf, sizeOfCursor)
				if !ok {
					goto done
				}
				copy(cursor, cursorRaw)
				break
			}
			// No next sibling — go to parent.
			if depth == 0 {
				goto done
			}
			mem.Write(r.transferBuf, cursor)
			parentRes, err := cursorParentFn.Call(ctx, uint64(tree.treePtr))
			if err != nil || api.DecodeI32(parentRes[0]) == 0 {
				goto done
			}
			cursorRaw, ok = mem.Read(r.transferBuf, sizeOfCursor)
			if !ok {
				goto done
			}
			copy(cursor, cursorRaw)
			depth--
		}
	}
done:
	return results, nil
}

// WalkAllNamedNodes walks the tree depth-first and returns one RawNode per
// node whose symbol id appears in matchSymIDs. The 20-byte TSNode struct is
// snapshot from TRANSFER_BUFFER at visit time so callers can later marshal
// it back for follow-up exports (child-by-field, end-index, parent, etc.)
// without re-walking the tree.
//
// Avoids ts_query_new entirely — safe to call in any runtime state.
//
// Performance: O(N) over the parsed tree. For typical source files the
// hot inner loop runs ~10k iterations; wazero call overhead dominates and
// the whole walk completes in single-digit milliseconds.
func (l *Language) WalkAllNamedNodes(ctx context.Context, tree *Tree, matchSymIDs []uint16) ([]RawNode, error) {
	r := l.rt
	mem := r.mem

	cursorNewFn := r.tsMod.ExportedFunction("ts_tree_cursor_new_wasm")
	cursorDeleteFn := r.tsMod.ExportedFunction("ts_tree_cursor_delete_wasm")
	cursorFirstChildFn := r.tsMod.ExportedFunction("ts_tree_cursor_goto_first_child_wasm")
	cursorNextSiblingFn := r.tsMod.ExportedFunction("ts_tree_cursor_goto_next_sibling_wasm")
	cursorParentFn := r.tsMod.ExportedFunction("ts_tree_cursor_goto_parent_wasm")
	cursorCurrentNodeFn := r.tsMod.ExportedFunction("ts_tree_cursor_current_node_wasm")
	cursorTypeIDFn := r.tsMod.ExportedFunction("ts_tree_cursor_current_node_type_id_wasm")
	endIndexFn := r.tsMod.ExportedFunction("ts_node_end_index_wasm")

	if cursorNewFn == nil || cursorDeleteFn == nil || cursorFirstChildFn == nil ||
		cursorNextSiblingFn == nil || cursorParentFn == nil || cursorCurrentNodeFn == nil ||
		cursorTypeIDFn == nil || endIndexFn == nil {
		return nil, fmt.Errorf("treesitter: required cursor functions not exported")
	}

	root := tree.RootNode(ctx)
	root.marshalNodeToTransferBuf()
	if _, err := cursorNewFn.Call(ctx, uint64(tree.treePtr)); err != nil {
		return nil, fmt.Errorf("treesitter: ts_tree_cursor_new_wasm: %w", err)
	}

	cursor := make([]byte, sizeOfCursor)
	cursorRaw, ok := mem.Read(r.transferBuf, sizeOfCursor)
	if !ok {
		cursorDeleteFn.Call(ctx) //nolint:errcheck
		return nil, fmt.Errorf("treesitter: read cursor from TRANSFER_BUFFER failed")
	}
	copy(cursor, cursorRaw)

	defer func() {
		mem.Write(r.transferBuf, cursor)
		cursorDeleteFn.Call(ctx) //nolint:errcheck
	}()

	matchSet := make(map[uint16]bool, len(matchSymIDs))
	for _, id := range matchSymIDs {
		matchSet[id] = true
	}

	var results []RawNode
	depth := 0

	restoreCursor := func() {
		mem.Write(r.transferBuf, cursor)
	}

	for {
		restoreCursor()
		typeRes, err := cursorTypeIDFn.Call(ctx, uint64(tree.treePtr))
		if err != nil {
			break
		}
		typeID := uint16(api.DecodeU32(typeRes[0]))

		if matchSet[typeID] {
			restoreCursor()
			if _, err := cursorCurrentNodeFn.Call(ctx, uint64(tree.treePtr)); err == nil {
				nodeRaw, ok := mem.Read(r.transferBuf, sizeOfNode)
				if ok {
					nodeStart := binary.LittleEndian.Uint32(nodeRaw[4:8])
					mem.Write(r.transferBuf, nodeRaw)
					endRes, err := endIndexFn.Call(ctx, uint64(tree.treePtr))
					if err == nil {
						nodeEnd := api.DecodeU32(endRes[0])
						var rn RawNode
						rn.TypeID = typeID
						rn.StartByte = nodeStart
						rn.EndByte = nodeEnd
						copy(rn.NodeRaw[:], nodeRaw)
						results = append(results, rn)
					}
				}
			}
		}

		// Try first child.
		restoreCursor()
		childRes, err := cursorFirstChildFn.Call(ctx, uint64(tree.treePtr))
		if err == nil && api.DecodeI32(childRes[0]) != 0 {
			cursorRaw, ok = mem.Read(r.transferBuf, sizeOfCursor)
			if !ok {
				break
			}
			copy(cursor, cursorRaw)
			depth++
			continue
		}

		// No children — next sibling / unwind.
		for {
			restoreCursor()
			sibRes, err := cursorNextSiblingFn.Call(ctx, uint64(tree.treePtr))
			if err == nil && api.DecodeI32(sibRes[0]) != 0 {
				cursorRaw, ok = mem.Read(r.transferBuf, sizeOfCursor)
				if !ok {
					goto done
				}
				copy(cursor, cursorRaw)
				break
			}
			if depth == 0 {
				goto done
			}
			restoreCursor()
			parentRes, err := cursorParentFn.Call(ctx, uint64(tree.treePtr))
			if err != nil || api.DecodeI32(parentRes[0]) == 0 {
				goto done
			}
			cursorRaw, ok = mem.Read(r.transferBuf, sizeOfCursor)
			if !ok {
				goto done
			}
			copy(cursor, cursorRaw)
			depth--
		}
	}
done:
	return results, nil
}

// WalkAllNamedNodesNoCursor performs the same walk as WalkAllNamedNodes
// but using ts_node_named_child_wasm + ts_node_named_child_count_wasm
// instead of a tree cursor. It is markedly slower (each child lookup costs
// one wasm call instead of an iterator step) but is robust to whatever
// cursor-state weirdness causes WalkAllNamedNodes to drop entire subtrees
// for some grammars (PHP class_declaration in a multi-method file is the
// motivating case).
//
// Strategy: a Go-side recursion that, for each node:
//  1. Records the node if its type id is in matchSymIDs.
//  2. Iterates its named children via ts_node_named_child_wasm and recurses.
//
// We never need a cursor handle; each node is uniquely identified by its
// 20-byte TSNode struct which we keep on the Go stack across recursion
// frames. The wazero limit on Go stack growth is generous enough that the
// natural AST depth (~30 for non-pathological source) is fine.
func (l *Language) WalkAllNamedNodesNoCursor(ctx context.Context, tree *Tree, matchSymIDs []uint16) ([]RawNode, error) {
	r := l.rt
	mem := r.mem

	namedChildFn := r.tsMod.ExportedFunction("ts_node_named_child_wasm")
	namedChildCountFn := r.tsMod.ExportedFunction("ts_node_named_child_count_wasm")
	endIndexFn := r.tsMod.ExportedFunction("ts_node_end_index_wasm")
	symFn := r.tsMod.ExportedFunction("ts_node_symbol_wasm")
	if namedChildFn == nil || namedChildCountFn == nil || endIndexFn == nil || symFn == nil {
		return nil, fmt.Errorf("treesitter: required node functions not exported")
	}

	matchSet := make(map[uint16]bool, len(matchSymIDs))
	for _, id := range matchSymIDs {
		matchSet[id] = true
	}

	root := tree.RootNode(ctx)
	var rootRaw [sizeOfNode]byte
	copy(rootRaw[:], root.nodeRaw[:])

	var results []RawNode

	// Iterative DFS using an explicit stack of raw TSNode structs — avoids
	// Go-recursion overhead and stack-overflow risk on pathological inputs.
	stack := make([][sizeOfNode]byte, 0, 64)
	stack = append(stack, rootRaw)

	for len(stack) > 0 {
		n := len(stack) - 1
		nodeRaw := stack[n]
		stack = stack[:n]

		// Compute typeID by symbol_wasm.
		mem.Write(r.transferBuf, nodeRaw[:])
		symRes, err := symFn.Call(ctx, uint64(tree.treePtr))
		if err != nil {
			continue
		}
		typeID := uint16(api.DecodeU32(symRes[0]))

		if matchSet[typeID] {
			// Get end index for this node.
			mem.Write(r.transferBuf, nodeRaw[:])
			endRes, err := endIndexFn.Call(ctx, uint64(tree.treePtr))
			if err == nil {
				nodeEnd := api.DecodeU32(endRes[0])
				nodeStart := binary.LittleEndian.Uint32(nodeRaw[4:8])
				if nodeStart < nodeEnd {
					var rn RawNode
					rn.TypeID = typeID
					rn.StartByte = nodeStart
					rn.EndByte = nodeEnd
					copy(rn.NodeRaw[:], nodeRaw[:])
					results = append(results, rn)
				}
			}
		}

		// Iterate named children in REVERSE order so that, after pushing
		// onto the LIFO stack, they pop in source order (left-to-right).
		mem.Write(r.transferBuf, nodeRaw[:])
		countRes, err := namedChildCountFn.Call(ctx, uint64(tree.treePtr))
		if err != nil {
			continue
		}
		count := api.DecodeU32(countRes[0])
		// Push children in reverse so leftmost ends up on top.
		// Collect first, then push reversed.
		children := make([][sizeOfNode]byte, 0, count)
		for i := uint32(0); i < count; i++ {
			mem.Write(r.transferBuf, nodeRaw[:])
			if _, err := namedChildFn.Call(ctx, uint64(tree.treePtr), uint64(i)); err != nil {
				continue
			}
			childRaw, ok := mem.Read(r.transferBuf, sizeOfNode)
			if !ok {
				continue
			}
			id := binary.LittleEndian.Uint32(childRaw[0:4])
			if id == 0 {
				continue
			}
			var c [sizeOfNode]byte
			copy(c[:], childRaw)
			children = append(children, c)
		}
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, children[i])
		}
	}
	return results, nil
}

// NodeChildByFieldID returns the byte range of the named-field child of the
// given parent node (raw 20-byte TSNode struct) for the given field id.
// Returns (0,0,false) if the field has no child or the lookup fails. Uses
// ts_node_child_by_field_id_wasm + ts_node_end_index_wasm.
//
// Use with RawNode.NodeRaw captured via WalkAllNamedNodes:
//
//	start, end, ok := lang.NodeChildByFieldID(ctx, tree, rn.NodeRaw[:], nameFieldID)
func (l *Language) NodeChildByFieldID(ctx context.Context, tree *Tree, nodeRaw []byte, fieldID uint32) (uint32, uint32, bool) {
	if fieldID == 0 || len(nodeRaw) < sizeOfNode {
		return 0, 0, false
	}
	r := l.rt
	mem := r.mem
	childByFieldFn := r.tsMod.ExportedFunction("ts_node_child_by_field_id_wasm")
	endIndexFn := r.tsMod.ExportedFunction("ts_node_end_index_wasm")
	if childByFieldFn == nil || endIndexFn == nil {
		return 0, 0, false
	}

	mem.Write(r.transferBuf, nodeRaw)
	if _, err := childByFieldFn.Call(ctx, uint64(tree.treePtr), uint64(fieldID)); err != nil {
		return 0, 0, false
	}
	childRaw, ok := mem.Read(r.transferBuf, sizeOfNode)
	if !ok {
		return 0, 0, false
	}
	// A missing-field child has id=0 in field[0] of TSNode.
	id := binary.LittleEndian.Uint32(childRaw[0:4])
	if id == 0 {
		return 0, 0, false
	}
	start := binary.LittleEndian.Uint32(childRaw[4:8])
	mem.Write(r.transferBuf, childRaw)
	endRes, err := endIndexFn.Call(ctx, uint64(tree.treePtr))
	if err != nil {
		return 0, 0, false
	}
	end := api.DecodeU32(endRes[0])
	if start >= end {
		return 0, 0, false
	}
	return start, end, true
}

// NamedChild returns the byte range of the i-th named child of the given node
// (raw 20-byte TSNode struct). Returns (0,0,false) if there is no such child
// or the lookup fails. Uses ts_node_named_child_wasm.
//
// Useful when you need a child whose grammar position is fixed but it's not
// declared as a named field (e.g. PHP's namespace_use_clause holds its
// qualified_name / name as an anonymous first named child).
func (l *Language) NamedChild(ctx context.Context, tree *Tree, nodeRaw []byte, index uint32) (uint32, uint32, uint16, bool) {
	if len(nodeRaw) < sizeOfNode {
		return 0, 0, 0, false
	}
	r := l.rt
	mem := r.mem
	namedChildFn := r.tsMod.ExportedFunction("ts_node_named_child_wasm")
	endIndexFn := r.tsMod.ExportedFunction("ts_node_end_index_wasm")
	if namedChildFn == nil || endIndexFn == nil {
		return 0, 0, 0, false
	}
	mem.Write(r.transferBuf, nodeRaw)
	if _, err := namedChildFn.Call(ctx, uint64(tree.treePtr), uint64(index)); err != nil {
		return 0, 0, 0, false
	}
	childRaw, ok := mem.Read(r.transferBuf, sizeOfNode)
	if !ok {
		return 0, 0, 0, false
	}
	id := binary.LittleEndian.Uint32(childRaw[0:4])
	if id == 0 {
		return 0, 0, 0, false
	}
	start := binary.LittleEndian.Uint32(childRaw[4:8])
	mem.Write(r.transferBuf, childRaw)
	endRes, err := endIndexFn.Call(ctx, uint64(tree.treePtr))
	if err != nil {
		return 0, 0, 0, false
	}
	end := api.DecodeU32(endRes[0])
	// Get symbol id for the child via ts_node_symbol_wasm.
	symFn := r.tsMod.ExportedFunction("ts_node_symbol_wasm")
	var childType uint16
	if symFn != nil {
		mem.Write(r.transferBuf, childRaw)
		if symRes, err := symFn.Call(ctx, uint64(tree.treePtr)); err == nil {
			childType = uint16(api.DecodeU32(symRes[0]))
		}
	}
	if start >= end {
		return 0, 0, 0, false
	}
	return start, end, childType, true
}

// NamedChildCount returns the number of named children of the given node.
// Uses ts_node_named_child_count_wasm.
func (l *Language) NamedChildCount(ctx context.Context, tree *Tree, nodeRaw []byte) uint32 {
	if len(nodeRaw) < sizeOfNode {
		return 0
	}
	r := l.rt
	mem := r.mem
	fn := r.tsMod.ExportedFunction("ts_node_named_child_count_wasm")
	if fn == nil {
		return 0
	}
	mem.Write(r.transferBuf, nodeRaw)
	res, err := fn.Call(ctx, uint64(tree.treePtr))
	if err != nil {
		return 0
	}
	return api.DecodeU32(res[0])
}
