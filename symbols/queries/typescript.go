//go:build !lite

package queries

// TypescriptTags is the tags.scm query for TypeScript/TSX/JavaScript.
// Curated subset of upstream tree-sitter-typescript@0.23.2 queries/tags.scm.
const TypescriptTags = `
(function_declaration
  name: (identifier) @name.definition.function)

(method_definition
  name: (property_identifier) @name.definition.method)

(class_declaration
  name: (type_identifier) @name.definition.class)

(interface_declaration
  name: (type_identifier) @name.definition.interface)

(type_alias_declaration
  name: (type_identifier) @name.definition.type)

(call_expression
  function: [(identifier) @name.reference.call
             (member_expression
               property: (property_identifier) @name.reference.call)])

(import_specifier
  name: (identifier) @name.reference.import)

(import_clause
  (identifier) @name.reference.import)
`
