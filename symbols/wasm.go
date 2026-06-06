//go:build !lite

package symbols

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/thanhhaudev/llmreviewkit/symbols/treesitter"
)

//go:embed all:grammars
var grammarFS embed.FS

// workspaceRoot is set by the runner before bundle extraction so the
// resolver knows where to look for project-local grammars.
var (
	workspaceRoot   string
	workspaceRootMu sync.Mutex
)

// SetWorkspaceRoot configures the project root used for project-local
// grammar lookup (./.kizunax/grammars/). Must be called by the runner
// before ExtractFromBundle. Safe to call concurrently.
func SetWorkspaceRoot(ws string) {
	workspaceRootMu.Lock()
	defer workspaceRootMu.Unlock()
	workspaceRoot = ws
}

func getWorkspaceRoot() string {
	workspaceRootMu.Lock()
	defer workspaceRootMu.Unlock()
	return workspaceRoot
}

// hintEmitted tracks per-grammar verbose hints to avoid log spam.
var (
	hintEmitted = map[string]bool{}
	hintMu      sync.Mutex
)

func emitHintOnce(grammarName string) {
	hintMu.Lock()
	defer hintMu.Unlock()
	if hintEmitted[grammarName] {
		return
	}
	hintEmitted[grammarName] = true
	fmt.Fprintf(os.Stderr,
		"[verbose] grammars: no .wasm found for %s — try 'kizunax grammars install %s'\n",
		grammarName, grammarName)
}

// langCache caches loaded grammars across extractor instances so each
// grammar only loads once per session.
var (
	langCache   = map[string]*treesitter.Language{}
	langCacheMu sync.Mutex
)

// wasmGrammarNameFor maps a file extension to a tree-sitter grammar
// name. Add entries here when new grammars are bundled. Returns ""
// for extensions not handled by WASM.
var wasmGrammarNameFor = func(ext string) string {
	switch ext {
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".kt", ".kts":
		return "kotlin"
	case ".swift":
		return "swift"
	case ".scala":
		return "scala"
	case ".cpp", ".hpp", ".cc", ".hh":
		return "cpp"
	case ".c", ".h":
		return "c"
	}
	return ""
}

// useWASM returns true if a grammar is BUNDLED for ext. (It may not be
// COMPILED yet — see fallback in WASMExtractor.Extract.)
func useWASM(ext string) bool {
	return wasmGrammarNameFor(ext) != ""
}

// newWASMExtractor returns a WASMExtractor configured for ext.
// If the .wasm grammar file is missing at runtime (not yet compiled),
// Extract falls back to regex behavior — strictly additive: enrichment
// works, just less precise.
func newWASMExtractor(ext string) Extractor {
	name := wasmGrammarNameFor(ext)
	return &wasmExtractor{grammarName: name}
}

type wasmExtractor struct {
	grammarName string
}

func (e *wasmExtractor) Extract(path string, content []byte) []Symbol {
	ctx := context.Background()

	// Resolve grammar path via project-local then global lookup.
	resolver := DefaultResolver(getWorkspaceRoot())
	grammarPath := resolver.Find(e.grammarName)
	if grammarPath == "" {
		// No compiled .wasm found — emit a once-per-session verbose hint
		// and fall back to regex so enrichment is still useful.
		emitHintOnce(e.grammarName)
		return regexFallback(path, content)
	}

	lang, err := getCachedLang(ctx, e.grammarName, grammarPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] treesitter load %s: %v\n", e.grammarName, err)
		return regexFallback(path, content)
	}

	queryStr := queryForGrammar(e.grammarName)
	if queryStr == "" {
		// No tags.scm shipped for this grammar yet (others pending compilation
		// in Task 14). typescript and python are wired in Tasks 17/18.
		return regexFallback(path, content)
	}

	// Python + PHP use cursor-based tree walking instead of ts_query_new.
	//
	// web-tree-sitter 0.26.9 + tree-sitter-python@0.23.6 + tree-sitter-php@0.24.2
	// reliably trap ts_query_new with OOB memory access from the very first call,
	// regardless of whether the runtime is fresh or has been used for other
	// grammars. The cursor-based walker bypasses ts_query_new entirely and
	// produces equivalent (or richer) symbol coverage compared to the tags.scm
	// query path.
	//
	// TypeScript / TSX still use the query path because that grammar's
	// ts_query_new succeeds after a small number of dlmalloc warm-up retries
	// (see internal/symbols/treesitter/query.go).
	if e.grammarName == "python" {
		return extractPythonViaWalk(ctx, lang, content, path)
	}
	if e.grammarName == "php" {
		return DispatchPHP(ctx, lang, content, path)
	}

	// IMPORTANT: NewQuery must be called BEFORE Parse — see Task 8 finding.
	q, err := lang.NewQuery(ctx, queryStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] treesitter query %s: %v\n", e.grammarName, err)
		return regexFallback(path, content)
	}
	defer q.Close(ctx)

	tree, err := lang.Parse(ctx, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] treesitter parse %s: %v\n", path, err)
		return regexFallback(path, content)
	}
	defer tree.Close(ctx)

	caps, err := q.Exec(ctx, tree.RootNode(ctx))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] treesitter exec %s: %v\n", e.grammarName, err)
		return regexFallback(path, content)
	}

	return scanCaptures(caps, content, path)
}

// pythonTypeIdentRE matches capitalized (PEP-8 CamelCase) identifiers in a
// Python type annotation byte range. Built-in lowercase types (int, str, bool,
// None) don't match, which is intentional: the workspace index targets
// user-defined types, not stdlib primitives.
var pythonTypeIdentRE = regexp.MustCompile(`[A-Z][A-Za-z0-9_]*`)

// extractPythonTypeNames returns capitalized identifiers from a type annotation
// string. Filters per Python PEP-8: user-defined type names are CamelCase;
// lowercase identifiers (int, str, bool, etc.) are not user-defined types in
// the workspace index.
func extractPythonTypeNames(text string) []string {
	return pythonTypeIdentRE.FindAllString(text, -1)
}

// extractPythonViaWalk extracts Python symbols using tree cursor traversal,
// bypassing ts_query_new which traps OOB for tree-sitter-python@0.23.x +
// web-tree-sitter 0.26.9 even on a fresh runtime.
//
// Coverage matches the v0.12.1 regex baseline plus better precision:
//
//   - function_definition.name           → SymDef
//   - class_definition.name              → SymDef
//   - import_statement names             → SymImport
//   - import_from_statement names        → SymImport (each imported name)
//   - call.function                      → SymCall (plain function call: foo())
//   - call.function = attribute          → SymCall (method call: obj.foo()) — emits the method name
func extractPythonViaWalk(ctx context.Context, lang *treesitter.Language, content []byte, path string) []Symbol {
	tree, err := lang.Parse(ctx, content)
	if err != nil {
		return regexFallback(path, content)
	}
	defer tree.Close(ctx)
	if tree.RootNode(ctx).Type(ctx) == "ERROR" {
		return regexFallback(path, content)
	}

	// Symbol type IDs.
	fnDefID := lang.SymbolIDForName(ctx, "function_definition", true)
	classDefID := lang.SymbolIDForName(ctx, "class_definition", true)
	importID := lang.SymbolIDForName(ctx, "import_statement", true)
	importFromID := lang.SymbolIDForName(ctx, "import_from_statement", true)
	callID := lang.SymbolIDForName(ctx, "call", true)
	attributeID := lang.SymbolIDForName(ctx, "attribute", true)
	dottedNameID := lang.SymbolIDForName(ctx, "dotted_name", true)
	aliasID := lang.SymbolIDForName(ctx, "aliased_import", true)
	decoratorID := lang.SymbolIDForName(ctx, "decorator", true)
	// v1.1.0: annotated assignment for body-level type refs (e.g. z: Bar = None).
	assignmentID := lang.SymbolIDForName(ctx, "assignment", true)

	// Field IDs.
	nameFieldID := lang.FieldIDForName(ctx, "name")
	functionFieldID := lang.FieldIDForName(ctx, "function")
	attributeFieldID := lang.FieldIDForName(ctx, "attribute")
	moduleNameFieldID := lang.FieldIDForName(ctx, "module_name")
	// v1.1.0: type annotation fields for parameter + return + assignment type refs.
	parametersFieldID := lang.FieldIDForName(ctx, "parameters")
	returnTypeFieldID := lang.FieldIDForName(ctx, "return_type")
	typeFieldID := lang.FieldIDForName(ctx, "type")

	matchIDs := make([]uint16, 0, 8)
	for _, id := range []uint16{fnDefID, classDefID, importID, importFromID, callID, decoratorID} {
		if id != 0 {
			matchIDs = append(matchIDs, id)
		}
	}
	// Include assignment nodes to catch body-level annotated assignments.
	if assignmentID != 0 {
		matchIDs = append(matchIDs, assignmentID)
	}
	if len(matchIDs) == 0 {
		return regexFallback(path, content)
	}

	nodes, err := lang.WalkAllNamedNodesNoCursor(ctx, tree, matchIDs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] treesitter walk python %s: %v\n", path, err)
		return regexFallback(path, content)
	}
	if len(nodes) == 0 {
		return regexFallback(path, content)
	}

	out := make([]Symbol, 0, len(nodes))
	emit := func(name string, kind SymbolKind, byteOff uint32) {
		if name == "" {
			return
		}
		out = append(out, Symbol{Name: name, Kind: kind, File: path, Line: lineAt(content, byteOff)})
	}

	for _, n := range nodes {
		switch n.TypeID {
		case fnDefID, classDefID:
			if s, e, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], nameFieldID); ok {
				emit(sliceBytes(content, s, e), SymDef, s)
			}
			// v1.1.0: emit SymTypeRef for parameter annotations + return annotation.
			// Strategy: slice the byte range of the parameters / return_type field and
			// regex-extract CamelCase identifiers (PEP-8 user-defined types). Built-in
			// lowercase types (int, str, bool) don't match and are intentionally skipped.
			if n.TypeID == fnDefID {
				if parametersFieldID != 0 && typeFieldID != 0 {
					if ps, pe, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], parametersFieldID); ok {
						paramText := sliceBytes(content, ps, pe)
						for _, name := range extractPythonTypeNames(paramText) {
							if name == "None" || name == "True" || name == "False" {
								continue
							}
							emit(name, SymTypeRef, ps)
						}
					}
				}
				if returnTypeFieldID != 0 {
					if rs, re_, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], returnTypeFieldID); ok {
						retText := sliceBytes(content, rs, re_)
						for _, name := range extractPythonTypeNames(retText) {
							if name == "None" || name == "True" || name == "False" {
								continue
							}
							emit(name, SymTypeRef, rs)
						}
					}
				}
			}
		case assignmentID:
			// Annotated assignment (`z: Bar = None`) has a `type:` field.
			if typeFieldID != 0 {
				if ts, te, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], typeFieldID); ok {
					tText := sliceBytes(content, ts, te)
					for _, name := range extractPythonTypeNames(tText) {
						if name == "None" || name == "True" || name == "False" {
							continue
						}
						emit(name, SymTypeRef, ts)
					}
				}
			}
		case importID:
			// Children: dotted_name | aliased_import (with `name` field=dotted_name).
			extractPythonImportNames(ctx, lang, tree, n.NodeRaw[:], dottedNameID, aliasID, nameFieldID, content, emit)
		case importFromID:
			// module_name field + a list of dotted_name | aliased_import children.
			if s, e, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], moduleNameFieldID); ok {
				emit(sliceBytes(content, s, e), SymImport, s)
			}
			// Iterate named children, skip the module_name child (already emitted).
			extractPythonImportNames(ctx, lang, tree, n.NodeRaw[:], dottedNameID, aliasID, nameFieldID, content, emit)
		case callID:
			if s, e, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], functionFieldID); ok {
				fnText := sliceBytes(content, s, e)
				if attributeID != 0 && strings.Contains(fnText, ".") {
					// Method call: emit terminal name as Name, chain as Pkg.
					// e.g. self.db.session.commit() → Name="commit", Pkg="self.db.session"
					name, pkg := splitDottedPath(fnText)
					if name != "" {
						dot := strings.LastIndex(fnText, ".")
						out = append(out, Symbol{
							Name: name,
							Pkg:  pkg,
							Kind: SymCall,
							File: path,
							Line: lineAt(content, s+uint32(dot)+1),
						})
					}
				} else {
					emit(fnText, SymCall, s)
				}
				_ = e
				_ = attributeFieldID
			}
		case decoratorID:
			// @app.route("/login") → emit "app.route" with split (Name="route", Pkg="app").
			// Bare decorators like @staticmethod and plain calls like @deco()
			// are handled by other emission paths (no receiver to surface here).
			decoratorText := sliceBytes(content, n.StartByte, n.EndByte)
			receiver := pythonDecoratorReceiver(decoratorText)
			if receiver == "" {
				break
			}
			name, pkg := splitDottedPath(receiver)
			if name == "" {
				break
			}
			// Find the offset of the receiver inside the decorator text so the
			// emitted Line is the decorator line, not the function-definition line.
			relOff := strings.Index(decoratorText, receiver)
			byteOff := n.StartByte + uint32(relOff)
			out = append(out, Symbol{
				Name: name,
				Pkg:  pkg,
				Kind: SymCall,
				File: path,
				Line: lineAt(content, byteOff),
			})
		}
	}
	return out
}

// extractPythonImportNames iterates the named children of an import_statement
// or import_from_statement and emits one SymImport per importable name.
// Handles `import x`, `import x.y.z`, `import x as y`, `from m import x, y as z`.
func extractPythonImportNames(ctx context.Context, lang *treesitter.Language, tree *treesitter.Tree, parentNodeRaw []byte, dottedNameID, aliasID uint16, nameFieldID uint32, content []byte, emit func(string, SymbolKind, uint32)) {
	count := lang.NamedChildCount(ctx, tree, parentNodeRaw)
	for i := uint32(0); i < count; i++ {
		start, end, childType, ok := lang.NamedChild(ctx, tree, parentNodeRaw, i)
		if !ok {
			continue
		}
		switch childType {
		case dottedNameID:
			emit(sliceBytes(content, start, end), SymImport, start)
		case aliasID:
			// `import x as y`: emit the `name` field child (the dotted_name).
			// We need the aliased_import's raw bytes to look up its `name`
			// field. Read them now.
			if aliasID == 0 {
				continue
			}
			// Re-fetch the child node's raw bytes via NamedChild (which fills
			// transfer buffer); we already have start/end but not the 20-byte
			// TSNode struct. NodeChildByFieldID on the parent doesn't help here
			// because that returns a single field. As a fallback we just emit
			// the entire aliased_import text up to " as ".
			text := sliceBytes(content, start, end)
			if idx := strings.Index(text, " as "); idx > 0 {
				emit(text[:idx], SymImport, start)
			} else {
				emit(text, SymImport, start)
			}
		}
	}
	_ = nameFieldID
}

// sliceBytes returns a bounded substring of src[start:end]. Returns "" if
// bounds are out of range.
func sliceBytes(src []byte, start, end uint32) string {
	if start >= end || int(end) > len(src) {
		return ""
	}
	return string(src[start:end])
}

// splitDottedPath splits a dotted-name string into a (name, pkg) pair
// matching the Symbol Pkg convention: the last segment is Name, everything
// before is Pkg. Examples:
//
//	"app"          → ("app", "")
//	"app.route"    → ("route", "app")
//	"a.b.c"        → ("c", "a.b")
//	""             → ("", "")
//	".leading"     → ("leading", "")
//	"trailing."    → ("", "trailing")
//
// Used by extractPythonViaWalk for decorator method qualifiers and
// dotted-import path emission.
func splitDottedPath(s string) (name, pkg string) {
	if s == "" {
		return "", ""
	}
	dot := strings.LastIndex(s, ".")
	if dot < 0 {
		return s, ""
	}
	return s[dot+1:], s[:dot]
}

// pythonDecoratorReceiver extracts the receiver path of a Python decorator
// that is an attribute call. Returns "" when the decorator is bare
// (@staticmethod) or a plain identifier call (@deco()) — both cases are
// already covered by other emission paths in extractPythonViaWalk, so this
// helper is the dedicated attribute-call extraction step.
//
// Examples:
//
//	"@app.route(\"/login\")" → "app.route"
//	"@a.b.c(arg)"            → "a.b.c"
//	"@staticmethod"          → ""    (bare decorator)
//	"@deco()"                → ""    (plain call, not attribute)
//
// Used by extractPythonViaWalk for decorator method-qualifier capture.
func pythonDecoratorReceiver(decoratorText string) string {
	s := strings.TrimSpace(decoratorText)
	if !strings.HasPrefix(s, "@") {
		return ""
	}
	s = s[1:] // drop leading '@'

	// Locate the opening paren of the call. If none, the decorator is
	// bare (e.g. @staticmethod, @property) — no receiver to extract.
	paren := strings.IndexByte(s, '(')
	if paren < 0 {
		return ""
	}
	head := strings.TrimSpace(s[:paren])

	// The receiver must be an attribute path (contains '.') — a plain
	// identifier call like @deco() has no method-qualifier to surface.
	if !strings.Contains(head, ".") {
		return ""
	}
	return head
}

// extractPHPViaWalk extracts PHP symbols using tree cursor traversal. See
// extractPythonViaWalk for the rationale (ts_query_new traps OOB on the
// PHP 0.24.2 grammar).
//
// Coverage:
//
//   - function_definition.name             → SymDef
//   - method_declaration.name              → SymDef
//   - class_declaration.name               → SymDef
//   - interface_declaration.name           → SymDef
//   - trait_declaration.name               → SymDef
//   - namespace_use_clause's first named child → SymImport
//   - scoped_call_expression.name (with receiver in scope field) → SymCall
//   - member_call_expression.name          → SymCall
//   - nullsafe_member_call_expression.name → SymCall
//   - function_call_expression.function    → SymCall (plain calls like foo())
func extractPHPViaWalk(ctx context.Context, lang *treesitter.Language, content []byte, path string) []Symbol {
	tree, err := lang.Parse(ctx, content)
	if err != nil {
		// Don't warn at INFO level — the PHP grammar has known false-OOB
		// traps on patterns like User::method($var). Fall back silently to
		// regex; the user already got the v0.12.1 behaviour as a floor.
		return regexFallback(path, content)
	}
	defer tree.Close(ctx)
	// If the parser produced an ERROR-rooted tree, treat it as a parse
	// failure and fall back to regex.
	if tree.RootNode(ctx).Type(ctx) == "ERROR" {
		return regexFallback(path, content)
	}

	// Definition node IDs.
	fnDefID := lang.SymbolIDForName(ctx, "function_definition", true)
	methodDeclID := lang.SymbolIDForName(ctx, "method_declaration", true)
	classDeclID := lang.SymbolIDForName(ctx, "class_declaration", true)
	interfaceDeclID := lang.SymbolIDForName(ctx, "interface_declaration", true)
	traitDeclID := lang.SymbolIDForName(ctx, "trait_declaration", true)

	// Import.
	useClauseID := lang.SymbolIDForName(ctx, "namespace_use_clause", true)

	// Calls.
	scopedCallID := lang.SymbolIDForName(ctx, "scoped_call_expression", true)
	memberCallID := lang.SymbolIDForName(ctx, "member_call_expression", true)
	nullsafeCallID := lang.SymbolIDForName(ctx, "nullsafe_member_call_expression", true)
	funcCallID := lang.SymbolIDForName(ctx, "function_call_expression", true)

	// Field IDs.
	nameFieldID := lang.FieldIDForName(ctx, "name")
	functionFieldID := lang.FieldIDForName(ctx, "function")
	// v1.1.0: type-hint field IDs for SymTypeRef emission.
	parametersFieldID := lang.FieldIDForName(ctx, "parameters")
	returnTypeFieldID := lang.FieldIDForName(ctx, "return_type")

	matchIDs := make([]uint16, 0, 16)
	for _, id := range []uint16{
		fnDefID, methodDeclID, classDeclID, interfaceDeclID, traitDeclID,
		useClauseID,
		scopedCallID, memberCallID, nullsafeCallID, funcCallID,
	} {
		if id != 0 {
			matchIDs = append(matchIDs, id)
		}
	}
	if len(matchIDs) == 0 {
		return regexFallback(path, content)
	}

	nodes, err := lang.WalkAllNamedNodesNoCursor(ctx, tree, matchIDs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] treesitter walk php %s: %v\n", path, err)
		return regexFallback(path, content)
	}
	// If the walker found nothing, parsing likely produced an ERROR-rooted
	// tree (PHP grammar bug on certain inputs — e.g. User::method($var)
	// triggers a ts_parser_parse_wasm OOB on a fresh runtime and the OOB
	// path returns from Parse with a tree but no usable children). Fall
	// back to regex so enrichment still works.
	if len(nodes) == 0 {
		return regexFallback(path, content)
	}

	out := make([]Symbol, 0, len(nodes))
	emit := func(name string, kind SymbolKind, byteOff uint32) {
		if name == "" {
			return
		}
		out = append(out, Symbol{Name: name, Kind: kind, File: path, Line: lineAt(content, byteOff)})
	}

	for _, n := range nodes {
		switch n.TypeID {
		case fnDefID, methodDeclID, classDeclID, interfaceDeclID, traitDeclID:
			if s, e, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], nameFieldID); ok {
				emit(sliceBytes(content, s, e), SymDef, s)
			}
			// v1.1.0 — emit SymTypeRef for parameter type hints + return type hint.
			// Only meaningful for function/method nodes (not class/interface/trait).
			if n.TypeID == fnDefID || n.TypeID == methodDeclID {
				if parametersFieldID != 0 {
					if ps, pe, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], parametersFieldID); ok {
						paramText := sliceBytes(content, ps, pe)
						for _, name := range extractPythonTypeNames(paramText) {
							// Skip PHP pseudo-constants that match the CamelCase pattern.
							switch name {
							case "True", "False", "Null":
								continue
							}
							emit(name, SymTypeRef, ps)
						}
					}
				}
				if returnTypeFieldID != 0 {
					if rs, re_, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], returnTypeFieldID); ok {
						rtText := sliceBytes(content, rs, re_)
						for _, name := range extractPythonTypeNames(rtText) {
							switch name {
							case "True", "False", "Null":
								continue
							}
							emit(name, SymTypeRef, rs)
						}
					}
				}
			}
		case useClauseID:
			// First named child is a qualified_name or name. Emit its full text
			// as the import (the leaf 'name' child of a qualified_name is the
			// short alias the rest of the file uses; we want that).
			s, e, _, ok := lang.NamedChild(ctx, tree, n.NodeRaw[:], 0)
			if ok {
				text := sliceBytes(content, s, e)
				// For qualified_name like \Foo\Bar, the alias is the last segment.
				if idx := strings.LastIndex(text, `\`); idx != -1 {
					emit(text[idx+1:], SymImport, s+uint32(idx)+1)
				} else {
					emit(text, SymImport, s)
				}
			}
		case scopedCallID, memberCallID, nullsafeCallID:
			if s, e, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], nameFieldID); ok {
				emit(sliceBytes(content, s, e), SymCall, s)
			}
		case funcCallID:
			if s, e, ok := lang.NodeChildByFieldID(ctx, tree, n.NodeRaw[:], functionFieldID); ok {
				text := sliceBytes(content, s, e)
				// Strip leading namespace separator for fully-qualified calls.
				if idx := strings.LastIndex(text, `\`); idx != -1 {
					emit(text[idx+1:], SymCall, s+uint32(idx)+1)
				} else {
					emit(text, SymCall, s)
				}
				_ = e
			}
		}
	}
	return out
}

// regexFallback delegates to RegexExtractor for the given file, providing
// the same symbol precision as the -tags lite build.
func regexFallback(path string, content []byte) []Symbol {
	return (&RegexExtractor{lang: extToLang(filepath.Ext(path))}).Extract(path, content)
}

// getCachedLang returns a cached Language for the given grammar path.
//
// Each grammar gets its OWN isolated wazero runtime — this is Fix 1 for
// the v0.12.2 PHP-after-TS regression. Sharing a single runtime across
// grammars causes ts_query_new OOB warm-up traps to corrupt dlmalloc
// in a way that prevents subsequent grammars from working. Per-grammar
// isolation eliminates that cross-contamination entirely.
//
// Cost: ~90 ms per grammar cold-start. With 3 grammars in a typical
// review (php/ts/python) that adds ~270 ms — acceptable because the
// alternative is silently regressing to regex.
func getCachedLang(ctx context.Context, name, path string) (*treesitter.Language, error) {
	langCacheMu.Lock()
	defer langCacheMu.Unlock()
	if lang, ok := langCache[path]; ok {
		return lang, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read grammar %s: %w", path, err)
	}
	rt, err := treesitter.NewRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("runtime: %w", err)
	}
	lang, err := rt.LoadGrammar(ctx, name, data)
	if err != nil {
		// Best-effort close to release the wazero runtime; the load
		// failure is the real error returned to the caller.
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("load grammar: %w", err)
	}
	langCache[path] = lang
	return lang, nil
}

// phpTags is the tags.scm query for PHP (mirrors queries.PHPTags).
// Inlined here to avoid an import cycle: symbols → symbols/queries → symbols.
const phpTags = `
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

// typescriptTags is the tags.scm query for TypeScript/TSX (mirrors
// queries.TypescriptTags). Inlined here to avoid an import cycle:
// symbols → symbols/queries → symbols. Curated subset of upstream
// tree-sitter-typescript@0.23.2 queries/tags.scm. Note: some patterns
// (e.g. function_declaration) require the NewQuery retry mechanism in
// the treesitter package to handle the dlmalloc warm-up needed by
// grammars compiled with tree-sitter-cli 0.24.x.
const typescriptTags = `
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

(required_parameter
  type: (type_annotation (type_identifier) @name.reference.type))

(optional_parameter
  type: (type_annotation (type_identifier) @name.reference.type))

(function_declaration
  return_type: (type_annotation (type_identifier) @name.reference.type))

(method_definition
  return_type: (type_annotation (type_identifier) @name.reference.type))

(arrow_function
  return_type: (type_annotation (type_identifier) @name.reference.type))

(variable_declarator
  type: (type_annotation (type_identifier) @name.reference.type))
`

// pythonTags is the tags.scm query for Python (mirrors queries.PythonTags).
// Inlined here to avoid an import cycle: symbols → symbols/queries → symbols.
//
// NOTE: Python's production extraction path (extractPythonViaWalk) bypasses
// ts_query_new entirely and uses tree cursor traversal instead. This const
// is kept so queryForGrammar("python") returns non-empty, marking Python as
// a "supported" grammar (non-empty → proceed; "" → regex fallback).
//
// Background: tree-sitter-python@0.23.x with web-tree-sitter 0.26.9 causes
// ts_query_new to trigger OOB traps that corrupt the runtime's dlmalloc after
// prior grammar operations, making all subsequent queries fail permanently.
const pythonTags = `
(function_definition) @name.definition.function

(class_definition) @name.definition.class
`

// queryForGrammar returns the tags.scm query string for the given grammar
// name. Returns "" for grammars not yet wired (others pending).
func queryForGrammar(name string) string {
	switch name {
	case "php":
		return phpTags
	case "typescript", "tsx":
		return typescriptTags
	case "python":
		return pythonTags
	}
	return ""
}

// scanCaptures translates treesitter captures to Symbols. Inlined from
// queries.ScanCaptures to avoid an import cycle (symbols → queries → symbols).
func scanCaptures(captures []treesitter.Capture, src []byte, path string) []Symbol {
	out := make([]Symbol, 0, len(captures))
	for _, c := range captures {
		if c.StartByte >= c.EndByte || int(c.EndByte) > len(src) {
			continue
		}
		name := string(src[c.StartByte:c.EndByte])
		var kind SymbolKind
		switch {
		case strings.HasPrefix(c.Name, "name.definition."):
			kind = SymDef
		case c.Name == "name.reference.call":
			kind = SymCall
		case c.Name == "name.reference.import" || c.Name == "module":
			kind = SymImport
		case c.Name == "name.reference.type":
			kind = SymTypeRef
		default:
			continue
		}
		line := lineAt(src, c.StartByte)
		out = append(out, Symbol{Name: name, Kind: kind, File: path, Line: line})
	}
	return out
}

// lineAt counts newlines in src up to byteOff to compute a 1-based line number.
func lineAt(src []byte, byteOff uint32) int {
	if int(byteOff) > len(src) {
		byteOff = uint32(len(src))
	}
	line := 1
	for i := uint32(0); i < byteOff; i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}
