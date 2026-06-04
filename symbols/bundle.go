package symbols

import (
	"regexp"
	"strings"

	"github.com/thanhhaudev/llmreviewkit/diff"
)

// hunkHeaderRe matches "+++ b/path" lines in unified diff output.
var hunkHeaderRe = regexp.MustCompile(`(?m)^\+\+\+ b/(.+?)\s*$`)

// ExtractFromBundle scans every modified or untracked file in the bundle,
// dispatches to the right Extractor for each, and returns a deduped flat
// list of symbols. Files with unknown extensions are skipped silently.
func ExtractFromBundle(b diff.Bundle) []Symbol {
	seen := map[string]bool{}
	var out []Symbol

	add := func(syms []Symbol) {
		for _, s := range syms {
			key := s.Name + "|" + s.Pkg + "|" + s.File
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, s)
		}
	}

	// 1. Each "+++ b/<path>" header marks a modified file. We can't easily
	// reconstruct the post-change content from a unified diff alone, so we
	// extract from the additions only (lines starting with '+').
	// For simplicity in v0.12, we feed the WHOLE diff text to the extractor —
	// since extractor methods scan line-by-line, identifiers in added lines
	// are picked up. False positives in context lines tolerated (resolver
	// would just find no definition for spurious symbols).
	paths := hunkHeaderRe.FindAllStringSubmatch(b.Diff, -1)
	for _, m := range paths {
		path := strings.TrimSpace(m[1])
		if path == "" || path == "/dev/null" {
			continue
		}
		e := ForFile(path)
		if e == nil {
			continue
		}
		// Filter to added lines only to reduce noise.
		added := extractAddedLines(b.Diff, path)
		if len(added) == 0 {
			continue
		}
		// Go files need a valid syntactic envelope for the AST parser.
		// Added lines from a diff are usually function-body fragments
		// (statements, calls) that won't parse standalone. Wrap them in a
		// fake function body when no package clause is present so the AST
		// extractor can walk the call/type/identifier expressions.
		src := added
		if strings.HasSuffix(path, ".go") && !strings.Contains(added, "package ") {
			src = "package main\nfunc _kizunaxDiffFragment() {\n" + added + "\n}\n"
		}
		syms := e.Extract(path, []byte(src))
		// If Go AST returned nothing (snippet still didn't parse — e.g. a
		// diff that adds an import block with no function context), fall
		// back to RegexExtractor so we still surface call sites for the
		// resolver to look up.
		if len(syms) == 0 && strings.HasSuffix(path, ".go") {
			syms = (&RegexExtractor{lang: extToLang(".go")}).Extract(path, []byte(added))
		}
		add(syms)
	}

	// 2. Untracked files: extractor gets the full content.
	for _, uf := range b.Untracked {
		e := ForFile(uf.Path)
		if e == nil {
			continue
		}
		add(e.Extract(uf.Path, []byte(uf.Content)))
	}

	return out
}

// extractAddedLines pulls just the "+" lines for a specific file from
// the unified diff, stripping the leading "+". Returns reconstructed
// snippet (not fully parseable code — but extractors are lenient).
func extractAddedLines(diffText, targetPath string) string {
	var sb strings.Builder
	inFile := false
	for _, line := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			inFile = strings.TrimSpace(strings.TrimPrefix(line, "+++ b/")) == targetPath
			continue
		}
		if strings.HasPrefix(line, "--- a/") || strings.HasPrefix(line, "diff --git") {
			inFile = false
			continue
		}
		if !inFile {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			sb.WriteString(line[1:])
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
