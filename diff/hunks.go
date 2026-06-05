package diff

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

// Hunk represents a chunk of changed lines for a single file. FileLineStart
// and FileLineCount refer to positions in the NEW (post-change) file — i.e.,
// the `+` side of `@@ -A,B +C,D @@`.
type Hunk struct {
	FileLineStart int
	FileLineCount int
}

// HunkEnd returns the inclusive last line covered by the hunk in the
// new-file numbering. A 1-line hunk at line 10 covers exactly line 10;
// a 5-line hunk at line 10 covers lines 10-14.
func (h Hunk) HunkEnd() int {
	if h.FileLineCount <= 1 {
		return h.FileLineStart
	}
	return h.FileLineStart + h.FileLineCount - 1
}

var hunkHeaderRE = regexp.MustCompile(`^@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,([0-9]+))? @@`)

// HunksByFile parses a unified diff string and returns, for each file
// (keyed by the post-change path from the `+++ b/<path>` header), the
// list of hunk ranges in the new file. Used by VerifyFindings to detect
// when an LLM cites a file:line that falls in unchanged context.
//
// Handles standard git diff output (`+++ b/<path>`) and the legacy form
// without the `b/` prefix. Lines that aren't `@@` headers or `+++ ` file
// markers are skipped — we only need the structural metadata, not the
// content. Buffer is sized to handle pathological 4 MB long lines that
// occur in minified-asset diffs.
func HunksByFile(unifiedDiff string) map[string][]Hunk {
	out := map[string][]Hunk{}
	var currentFile string
	scanner := bufio.NewScanner(strings.NewReader(unifiedDiff))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = line[len("+++ b/"):]
			continue
		}
		if strings.HasPrefix(line, "+++ ") && !strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ ")
			continue
		}
		if currentFile == "" {
			continue
		}
		if m := hunkHeaderRE.FindStringSubmatch(line); m != nil {
			start, _ := strconv.Atoi(m[1])
			count := 1
			if m[2] != "" {
				count, _ = strconv.Atoi(m[2])
			}
			out[currentFile] = append(out[currentFile], Hunk{FileLineStart: start, FileLineCount: count})
		}
	}
	return out
}

// FindingInHunk returns true if the line range [lineStart, lineEnd]
// overlaps any hunk for the given file. Returns false if the file
// is missing from the diff or no hunk intersects the range.
func FindingInHunk(hunks map[string][]Hunk, file string, lineStart, lineEnd int) bool {
	fileHunks, ok := hunks[file]
	if !ok {
		return false
	}
	for _, h := range fileHunks {
		hunkEnd := h.HunkEnd()
		if lineStart > hunkEnd || lineEnd < h.FileLineStart {
			continue
		}
		return true
	}
	return false
}
