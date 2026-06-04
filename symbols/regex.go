package symbols

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// RegexExtractor is the universal fallback extractor.
// Used for any non-Go file when WASM grammar is unavailable
// (either no grammar bundled, or building with -tags lite).
// Patterns are looked up in langPatterns by the lang field
// (set by the factory). An empty lang resolves to "default".
type RegexExtractor struct {
	lang string
}

// patternSet holds the regex patterns used by RegexExtractor for one
// language. Each slice may contain multiple alternates that are tried
// in order; the first match per line wins for defs/imports, and all
// matches across all alternates accumulate for calls.
type patternSet struct {
	defs    []*regexp.Regexp // capture group 1 = symbol name
	imports []*regexp.Regexp // capture group 1 = imported symbol or module
	calls   []*regexp.Regexp // capture group 1 = pkg/receiver, group 2 = method
}

var (
	defRe = regexp.MustCompile(
		`(?:export\s+|public\s+|private\s+|protected\s+|async\s+|abstract\s+)*` +
			`(?:func|fn|def|function|class|struct|type|interface|enum|trait|impl|module|record)` +
			`\s+([A-Za-z_]\w*)`,
	)
	importRe = regexp.MustCompile(
		`(?:^|\s)(?:import|from|use|require|using)\s+(?:\{[^}]+\}\s+from\s+)?["']?([A-Za-z_][\w\./:-]*)["']?`,
	)
	callRe = regexp.MustCompile(
		`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`,
	)
)

// langPatterns maps a language key (returned by extToLang) to its
// patternSet. The "default" key is the universal fallback used when
// the language is unknown — its contents preserve v0.12.0 behavior.
//
// Add a new language by:
//  1. Add a new map entry here.
//  2. Add the extension → language mapping in extToLang (factory.go).
//  3. Add table-driven test cases in regex_test.go.
var langPatterns = map[string]patternSet{
	"default": {
		defs:    []*regexp.Regexp{defRe},
		imports: []*regexp.Regexp{importRe},
		calls:   []*regexp.Regexp{callRe},
	},
	"php": {
		defs: []*regexp.Regexp{
			// function (with optional visibility/abstract/static/final)
			regexp.MustCompile(`(?:public\s+|private\s+|protected\s+|static\s+|final\s+|abstract\s+)*function\s+([A-Za-z_]\w*)`),
			// class / interface / trait / enum
			regexp.MustCompile(`(?:abstract\s+|final\s+)*(?:class|interface|trait|enum)\s+([A-Za-z_]\w*)`),
		},
		imports: []*regexp.Regexp{
			// use App\Foo\Bar;          → capture "Bar"
			// use App\Foo\Bar as Baz;   → capture "Baz" via the alias group
			regexp.MustCompile(`\buse\s+(?:function\s+|const\s+)?(?:\\?[A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*\\)?([A-Za-z_]\w*)(?:\s+as\s+([A-Za-z_]\w*))?\s*;`),
		},
		calls: []*regexp.Regexp{
			// Class::method(
			regexp.MustCompile(`\b([A-Za-z_]\w*)::([A-Za-z_]\w*)\s*\(`),
			// $obj->method( — receiver var + method
			regexp.MustCompile(`\$([A-Za-z_]\w*)->([A-Za-z_]\w*)\s*\(`),
			// ->intermediate->method( — chained terminal ($this->db->fetchRow())
			regexp.MustCompile(`->([A-Za-z_]\w*)->([A-Za-z_]\w*)\s*\(`),
		},
	},
	"python": {
		defs: []*regexp.Regexp{
			// def name(...) or async def name(...)
			regexp.MustCompile(`(?:async\s+)?def\s+([A-Za-z_]\w*)`),
			// class name
			regexp.MustCompile(`class\s+([A-Za-z_]\w*)`),
		},
		imports: []*regexp.Regexp{
			// from app.db import Connection           → "Connection"
			// from app.db import Connection as C      → "C" (alias via last-non-empty-capture)
			regexp.MustCompile(`^\s*from\s+[\w.]+\s+import\s+([A-Za-z_]\w*)(?:\s+as\s+([A-Za-z_]\w*))?`),
			// import app.logger as log                → "log"
			// import os                               → "os"
			regexp.MustCompile(`^\s*import\s+(?:[\w.]+\s+as\s+([A-Za-z_]\w*)|([A-Za-z_]\w*))`),
		},
		calls: []*regexp.Regexp{
			// obj.method(
			regexp.MustCompile(`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`),
		},
	},
	"ts": {
		defs: []*regexp.Regexp{
			// classic function (with optional export/async)
			regexp.MustCompile(`(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_]\w*)`),
			// const/let/var X = (…) => / X = async (…) => / single-arg arrow without parens
			regexp.MustCompile(`(?:export\s+)?(?:const|let|var)\s+([A-Za-z_]\w*)\s*(?::[^=]+)?=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z_]\w*)\s*(?::[^=]+)?=>`),
			// class / abstract class
			regexp.MustCompile(`(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_]\w*)`),
			// interface / enum / type alias
			regexp.MustCompile(`(?:export\s+)?(?:interface|enum|type)\s+([A-Za-z_]\w*)`),
		},
		imports: []*regexp.Regexp{
			// Named import: capture the whole brace body for splitting.
			// import { X, Y as Z } from '…';  or  import type { … } from …
			regexp.MustCompile(`import\s+(?:type\s+)?\{\s*([^}]+)\s*\}\s+from\s+["'][^"']+["']`),
			// Default import: import X from '…'
			regexp.MustCompile(`import\s+(?:type\s+)?([A-Za-z_]\w*)\s+from\s+["'][^"']+["']`),
			// Namespace import: import * as ns from '…'
			regexp.MustCompile(`import\s+\*\s+as\s+([A-Za-z_]\w*)\s+from\s+["'][^"']+["']`),
		},
		calls: []*regexp.Regexp{
			// obj.method( or obj?.method(
			regexp.MustCompile(`\b([A-Za-z_]\w*)\??\.([A-Za-z_]\w*)\s*\(`),
		},
	},
}

// splitNamedImports parses the body of a TS/JS named-import brace.
// Input: `Foo, Bar as Baz, Qux`. Output: ["Foo", "Baz", "Qux"].
// Whitespace and trailing commas are tolerated. The "type" modifier
// (e.g. `type X` in `import { type X, Y } from …`) is stripped.
func splitNamedImports(body string) []string {
	var out []string
	for _, raw := range strings.Split(body, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		// "X as Y" → take Y; "type X as Y" → take Y; "X" → take X.
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		name := fields[len(fields)-1]
		// Strip TS-only modifier prefix if it landed alone (e.g. "type").
		if name == "type" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func (e *RegexExtractor) Extract(path string, content []byte) []Symbol {
	key := e.lang
	if key == "" {
		key = "default"
	}
	ps, ok := langPatterns[key]
	if !ok {
		ps = langPatterns["default"]
	}

	var syms []Symbol
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()

		for _, re := range ps.defs {
			if m := re.FindSubmatch(line); m != nil {
				syms = append(syms, Symbol{
					Name: string(m[1]),
					Kind: SymDef,
					File: path,
					Line: lineNo,
				})
				break
			}
		}
		for ri, re := range ps.imports {
			m := re.FindSubmatch(line)
			if m == nil {
				continue
			}
			// Lang "ts" special case: the first imports pattern's group 1
			// is a brace body like "Foo, Bar as Baz" — split it and emit
			// one symbol per name (using alias when present).
			if key == "ts" && ri == 0 {
				for _, name := range splitNamedImports(string(m[1])) {
					syms = append(syms, Symbol{
						Name: name,
						Kind: SymImport,
						File: path,
						Line: lineNo,
					})
				}
				break
			}
			// General path: last non-empty capture group.
			name := ""
			for i := len(m) - 1; i >= 1; i-- {
				if len(m[i]) > 0 {
					name = string(m[i])
					break
				}
			}
			if name == "" {
				break
			}
			syms = append(syms, Symbol{
				Name: name,
				Kind: SymImport,
				File: path,
				Line: lineNo,
			})
			break
		}
		for _, re := range ps.calls {
			for _, m := range re.FindAllSubmatch(line, -1) {
				syms = append(syms, Symbol{
					Name: string(m[2]),
					Pkg:  string(m[1]),
					Kind: SymCall,
					File: path,
					Line: lineNo,
				})
			}
		}
	}
	return syms
}
