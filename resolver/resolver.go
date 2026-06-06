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
	scopePaths []string,
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
	tier0, tier1, tier2, err := collectTiers(workspaceRoot, tier0Dirs, scopePaths)
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

// collectTiers classifies workspace files into Tier 0 (diff dirs), Tier 1
// (sibling subtrees), and Tier 2 (everything else). When scopePaths is
// non-empty, the walk is limited to the union of those subtrees — files
// outside them are not classified at all. When scopePaths is nil/empty the
// entire workspace is walked (pre-v1.5.3 behavior).
//
// Scope paths are repo-relative (matches the kizunax --paths flag values).
// Overlapping scopes are deduped so a file is never visited twice.
// Empty-string scope entries are silently dropped; nil/empty-after-filter
// scopes walk the full workspace.
func collectTiers(ws string, tier0Dirs map[string]bool, scopePaths []string) (t0, t1, t2 []string, err error) {
	tier0Set := map[string]bool{}
	tier1Set := map[string]bool{}
	for d := range tier0Dirs {
		abs := filepath.Join(ws, d)
		tier0Set[abs] = true
		parent := filepath.Dir(abs)
		if parent != "" {
			tier1Set[parent] = true
		}
	}

	walkOnce := func(root string) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				fmt.Fprintf(os.Stderr, "[warn] resolver: skip %s: %v\n", path, walkErr)
				return nil
			}
			if d.IsDir() {
				if shouldSkipDir(d.Name()) && path != root {
					return fs.SkipDir
				}
				return nil
			}
			if shouldSkipFile(d.Name()) {
				return nil
			}
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
	}

	// Filter empty-string scope entries — filepath.Join(ws, "") == ws would
	// silently walk the entire workspace and defeat the scope purpose.
	var clean []string
	for _, sp := range scopePaths {
		if sp == "" {
			continue
		}
		clean = append(clean, sp)
	}

	if len(clean) == 0 {
		err = walkOnce(ws)
	} else {
		// Dedup absolute scope roots so overlapping --paths args
		// (e.g. "app" + "app/Http") visit each file at most once.
		// Sort by length ascending so shorter (ancestor) paths walk
		// first; nested sub-paths are then skipped because they're
		// already covered by the ancestor's walk.
		abs := make([]string, 0, len(clean))
		for _, sp := range clean {
			abs = append(abs, filepath.Join(ws, sp))
		}
		sort.Slice(abs, func(i, j int) bool { return len(abs[i]) < len(abs[j]) })
		walked := []string{}
		for _, root := range abs {
			covered := false
			for _, w := range walked {
				if root == w || strings.HasPrefix(root, w+string(filepath.Separator)) {
					covered = true
					break
				}
			}
			if covered {
				continue
			}
			walked = append(walked, root)
			if e := walkOnce(root); e != nil && err == nil {
				err = e
			}
		}
	}

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
