package resolver

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// Reference is a single match found by the resolver: a workspace file that
// likely defines (or strongly references) one of the input symbols.
type Reference struct {
	Symbol  symbols.Symbol
	File    string // repo-relative path
	Excerpt string // snippet around match (≤ maxExcerptBytes)
}

// ResolveStats is the v0.12.4 dual-metric return shape. Refs preserves the
// pre-v0.12.4 semantics (workspace matches, sorted, capped per symbol).
// The three counts surface where symbols are lost between scanner and
// resolver, so v0.13 direction can be decided from evidence.
type ResolveStats struct {
	Refs           []Reference // unchanged: workspace matches
	ExtractedCount int         // len(input syms) — what scanner gave us
	FilteredCount  int         // after stdlib + empty-name filter — symbols actually scanned
	ResolvedCount  int         // distinct symbol names with ≥1 workspace match (NOT len(Refs))
}

// FindReferences walks the workspace breadth-first from the directories
// containing diffFiles outward, searching for definitions of the given
// symbols. Stops per-symbol after maxRefsPerSymbol matches. Skips
// known stdlib symbols. Returns references sorted by relevance
// (Tier 0 matches first, then Tier 1, then Tier 2).
//
// Errors during walk are logged to stderr but do not fail the call —
// partial results are returned. Returns (stats, err) only on catastrophic
// failure where no work could be done at all.
func FindReferences(
	syms []symbols.Symbol,
	workspaceRoot string,
	diffFiles []string,
	maxRefsPerSymbol int,
	maxExcerptBytes int,
) (ResolveStats, error) {
	stats := ResolveStats{ExtractedCount: len(syms)}
	if len(syms) == 0 {
		return stats, nil
	}

	// Filter stdlib + empty names first; FilteredCount = what survives.
	work := make([]symbols.Symbol, 0, len(syms))
	for _, s := range syms {
		if IsStdlibSymbol(s) {
			continue
		}
		if s.Name == "" {
			continue
		}
		work = append(work, s)
	}
	stats.FilteredCount = len(work)
	if len(work) == 0 {
		return stats, nil
	}

	// Build BFS tiers from diff file directories.
	tier0Dirs := map[string]bool{}
	for _, df := range diffFiles {
		d := filepath.Dir(df)
		if d == "." || d == "" {
			d = "."
		}
		tier0Dirs[d] = true
	}

	// Compile per-symbol search patterns once.
	patterns := make(map[string]*regexp.Regexp, len(work))
	for _, s := range work {
		patterns[s.Name] = compileSymbolPattern(s.Name)
	}

	// Walk workspace. Collect candidate files into tiers.
	tier0, tier1, tier2, err := collectTiers(workspaceRoot, tier0Dirs)
	if err != nil {
		return stats, err
	}

	matches := map[string][]Reference{} // symbolName → references

	scanFile := func(absPath, relPath string) {
		f, err := os.Open(absPath)
		if err != nil {
			return
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		// Keep a rolling window of recent lines for excerpt extraction.
		var preCtx [3]string
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()

			for symName, pat := range patterns {
				if len(matches[symName]) >= maxRefsPerSymbol {
					continue
				}
				if !pat.MatchString(line) {
					continue
				}
				excerpt := buildExcerpt(absPath, lineNo, maxExcerptBytes, preCtx[:])
				matches[symName] = append(matches[symName], Reference{
					Symbol:  symbolByName(work, symName),
					File:    relPath,
					Excerpt: excerpt,
				})
			}

			// Slide context window.
			preCtx[0], preCtx[1], preCtx[2] = preCtx[1], preCtx[2], line
		}
	}

	for _, group := range [][]string{tier0, tier1, tier2} {
		for _, p := range group {
			rel, err := filepath.Rel(workspaceRoot, p)
			if err != nil {
				rel = p
			}
			scanFile(p, rel)
		}
	}

	// Flatten matches in determined order (by symbol name for stability).
	symNames := make([]string, 0, len(matches))
	for k := range matches {
		symNames = append(symNames, k)
	}
	sort.Strings(symNames)
	var out []Reference
	for _, name := range symNames {
		out = append(out, matches[name]...)
	}
	stats.Refs = out
	stats.ResolvedCount = len(matches) // distinct sym names with ≥1 match
	return stats, nil
}

func symbolByName(syms []symbols.Symbol, name string) symbols.Symbol {
	for _, s := range syms {
		if s.Name == name {
			return s
		}
	}
	return symbols.Symbol{Name: name}
}

// compileSymbolPattern builds a regex matching common definition headers
// across languages. Three forms are accepted:
//   - free declaration: `func Name(`, `def Name(`, `class Name {`, ...
//   - Go method receiver: `func (r *Type) Name(`
//   - language `extends`/`impl`/`record` keywords also match
//
// Receiver form is critical for Go projects because methods are the most
// commonly referenced symbol in diffs.
func compileSymbolPattern(name string) *regexp.Regexp {
	esc := regexp.QuoteMeta(name)
	// (?m): per-line. Two alternations:
	//   1) keyword + optional Go receiver + Name
	//   2) `Name` standing alone as a type-like declaration (legacy form)
	return regexp.MustCompile(
		`(?m)(?:^|\s)(?:func|fn|def|function|class|struct|type|interface|enum|trait|impl|module|record)` +
			`(?:\s*\([^)]*\))?` + // optional Go method receiver
			`\s+` + esc + `\b`,
	)
}

func collectTiers(ws string, tier0Dirs map[string]bool) (t0, t1, t2 []string, err error) {
	// Walk once, classify each file into a tier.
	tier0Set := map[string]bool{}
	tier1Set := map[string]bool{}
	for d := range tier0Dirs {
		abs := filepath.Join(ws, d)
		tier0Set[abs] = true
		// Tier 1 = files under sibling subtrees of Tier 0
		// (i.e., file's grandparent == one of Tier 0's parents).
		parent := filepath.Dir(abs)
		if parent != "" {
			tier1Set[parent] = true
		}
	}

	err = filepath.WalkDir(ws, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			fmt.Fprintf(os.Stderr, "[warn] resolver: skip %s: %v\n", path, walkErr)
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != ws {
				return fs.SkipDir
			}
			return nil
		}
		if shouldSkipFile(d.Name()) {
			return nil
		}
		// Classify into tier by enclosing dir.
		dir := filepath.Dir(path)
		switch {
		case tier0Set[dir]:
			t0 = append(t0, path)
		case tier1Set[filepath.Dir(dir)]:
			t1 = append(t1, path)
		default:
			t2 = append(t2, path)
		}
		return nil
	})
	sort.Strings(t0)
	sort.Strings(t1)
	sort.Strings(t2)
	return
}

// buildExcerpt reads up to maxBytes around line lineNo of file at absPath.
// preCtx is the 3 lines before the match (already in scanner context).
// Adds next ~50 lines or until a blank-line block close.
func buildExcerpt(absPath string, lineNo, maxBytes int, preCtx []string) string {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	start := lineNo - len(preCtx) - 1
	if start < 0 {
		start = 0
	}
	end := lineNo + 50
	if end > len(lines) {
		end = len(lines)
	}
	// Truncate at first blank-line block close after lineNo.
	for i := lineNo; i < end; i++ {
		if strings.TrimSpace(lines[i]) == "" && i > lineNo+5 {
			end = i
			break
		}
	}
	snippet := strings.Join(lines[start:end], "\n")
	if len(snippet) > maxBytes {
		snippet = snippet[:maxBytes]
	}
	return snippet
}
