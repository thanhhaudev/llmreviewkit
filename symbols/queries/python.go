//go:build !lite

package queries

// PythonTags is the tags.scm query for Python. Curated subset of
// upstream tree-sitter-python@0.23.6 queries/tags.scm.
//
// NOTE: The production extraction path for Python (wasm.go
// extractPythonViaWalk) uses tree cursor traversal via
// Language.WalkNamedChildren instead of NewQuery+Exec. This const serves
// two purposes:
//  1. queryForGrammar("python") returns non-empty, marking Python as a
//     "supported" grammar (empty → regex fallback).
//  2. Documents the intended query strategy for reference and future use
//     if a Python grammar version compatible with ts_query_new ships.
//
// Background: tree-sitter-python@0.23.x with web-tree-sitter 0.26.9 causes
// ts_query_new to trigger OOB traps that permanently corrupt the runtime's
// dlmalloc after prior grammar operations. WalkNamedChildren bypasses
// ts_query_new entirely and is always safe.
const PythonTags = `
(function_definition
  name: (identifier) @name.definition.function)

(class_definition
  name: (identifier) @name.definition.class)

(import_statement
  name: (dotted_name) @name.reference.import)

(import_from_statement
  module_name: (dotted_name) @name.reference.import)

(call
  function: (identifier) @name.reference.call)

(call
  function: (attribute
    attribute: (identifier) @name.reference.call))
`
