package expand

import (
	"os"
	"path/filepath"
	"strings"
)

// Tests returns candidate test files for each diff file, using
// per-language filename conventions:
//
//	Go        <base>_test.go
//	Python    test_<base>.py | <base>_test.py | tests/test_<base>.py
//	PHP       tests/<Base>Test.php
//	TS/TSX    <base>.test.ts(x) | <base>.spec.ts(x)
//
// Filesystem-only — does not require the index. Missing test files are
// silently skipped. Unknown extensions return no candidates.
func Tests(diffFiles []string, workspaceRoot string) []Candidate {
	if workspaceRoot == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []Candidate
	for _, df := range diffFiles {
		for _, pattern := range testPatternsFor(df) {
			if seen[pattern] {
				continue
			}
			full := filepath.Join(workspaceRoot, pattern)
			info, err := os.Stat(full)
			if err != nil || info.IsDir() {
				continue
			}
			seen[pattern] = true
			out = append(out, Candidate{
				File:          pattern,
				Strategy:      "test",
				SymbolMatches: 1,
				Recency:       info.ModTime().UnixNano(),
			})
		}
	}
	return out
}

// testPatternsFor returns candidate relative paths for the given diff
// file. Empty slice if the extension is not handled.
func testPatternsFor(diffFile string) []string {
	ext := strings.ToLower(filepath.Ext(diffFile))
	base := strings.TrimSuffix(filepath.Base(diffFile), filepath.Ext(diffFile))
	dir := filepath.Dir(diffFile)

	switch ext {
	case ".go":
		if strings.HasSuffix(base, "_test") {
			return nil
		}
		return []string{filepath.Join(dir, base+"_test.go")}
	case ".py":
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test") {
			return nil
		}
		return []string{
			filepath.Join(dir, "test_"+base+".py"),
			filepath.Join(dir, base+"_test.py"),
			filepath.Join("tests", "test_"+base+".py"),
		}
	case ".php":
		if strings.HasSuffix(base, "Test") {
			return nil
		}
		return []string{
			filepath.Join("tests", base+"Test.php"),
		}
	case ".ts", ".tsx":
		if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec") {
			return nil
		}
		return []string{
			filepath.Join(dir, base+".test"+ext),
			filepath.Join(dir, base+".spec"+ext),
		}
	}
	return nil
}
