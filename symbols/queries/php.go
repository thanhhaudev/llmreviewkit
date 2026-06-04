//go:build !lite

package queries

// PHPTags is the tags.scm query for PHP. Curated subset of
// upstream tree-sitter-php@0.24.2 queries/tags.scm.
const PHPTags = `
(function_definition
  name: (name) @name.definition.function)

(method_declaration
  name: (name) @name.definition.method)

(class_declaration
  name: (name) @name.definition.class)

(interface_declaration
  name: (name) @name.definition.interface)

(trait_declaration
  name: (name) @name.definition.trait)

(scoped_call_expression
  scope: (name) @receiver
  name: (name) @name.reference.call)

(nullsafe_member_call_expression
  name: (name) @name.reference.call)

(namespace_use_clause
  (qualified_name (name) @name.reference.import))

(namespace_use_clause
  (name) @name.reference.import)
`
