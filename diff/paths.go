package diff

import (
	"sort"
	"strings"
)

// Paths returns the sorted, deduped set of repo-relative file paths that
// appear in the bundle: those parsed from `+++ b/<path>` headers in the
// unified diff, plus the explicit Paths of any Untracked entries. `/dev/null`
// (deletion marker) is skipped.
//
// This is used by the runner to canonicalize finding.File when the LLM
// emits a basename instead of the full path (see runner.canonicalizeFindings).
func Paths(b Bundle) []string {
	set := map[string]struct{}{}

	for _, line := range strings.Split(b.Diff, "\n") {
		if !strings.HasPrefix(line, "+++ b/") {
			continue
		}
		p := strings.TrimPrefix(line, "+++ b/")
		// Strip any trailing whitespace (tabs from git's timestamp suffix etc.)
		p = strings.TrimRight(p, " \t\r")
		if p == "" || p == "/dev/null" {
			continue
		}
		set[p] = struct{}{}
	}

	for _, u := range b.Untracked {
		if u.Path == "" {
			continue
		}
		set[u.Path] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// DiffOnlyPaths returns the sorted, deduped set of paths that appear in the
// unified diff (`+++ b/<path>` headers only), excluding any untracked-file
// paths in b.Untracked. Used by bundlelog assembly so that an untracked file
// is logged once with reason="untracked_text" and not also as "diff_file".
//
// `/dev/null` (deletion marker) is skipped.
func DiffOnlyPaths(b Bundle) []string {
	set := map[string]struct{}{}
	for _, line := range strings.Split(b.Diff, "\n") {
		if !strings.HasPrefix(line, "+++ b/") {
			continue
		}
		p := strings.TrimPrefix(line, "+++ b/")
		p = strings.TrimRight(p, " \t\r")
		if p == "" || p == "/dev/null" {
			continue
		}
		set[p] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
