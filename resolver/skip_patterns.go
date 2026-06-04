package resolver

import "path/filepath"

// skipDirs are directory base names the BFS walker will not descend into.
var skipDirs = map[string]bool{
	".git": true, ".svn": true, ".hg": true,
	"node_modules":     true,
	"vendor":           true,
	"bower_components": true,
	"target":           true,
	"build":            true,
	"dist":             true,
	"out":              true,
	"__pycache__":      true,
	".pytest_cache":    true,
	".idea":            true,
	".vscode":          true,
	".worktrees":       true,
	"coverage":         true,
	".nyc_output":      true,
}

// skipFilePatterns are filename glob patterns to skip.
var skipFilePatterns = []string{
	"*_test.go",
	"*.test.ts", "*.test.tsx", "*.test.js", "*.test.jsx",
	"*.spec.ts", "*.spec.tsx", "*.spec.js", "*.spec.jsx",
	"*_test.py", "test_*.py",
	"*.generated.*",
	"*.gen.go",
	"*.pb.go",
	"*_string.go",
	".DS_Store",
}

// skipBinaryExts are file extensions the walker won't read.
var skipBinaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
	".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bz2": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".wasm": true, // we definitely don't grep our embedded grammars.
}

func shouldSkipDir(name string) bool {
	return skipDirs[name]
}

func shouldSkipFile(name string) bool {
	for _, pat := range skipFilePatterns {
		matched, _ := filepath.Match(pat, name)
		if matched {
			return true
		}
	}
	ext := filepath.Ext(name)
	return skipBinaryExts[ext]
}
