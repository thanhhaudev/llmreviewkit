package symbols

import "path/filepath"

// sourceExtensions are file extensions kizunax knows how to extract symbols from.
// Files with other extensions are skipped by ForFile (returns nil).
var sourceExtensions = map[string]bool{
	".go": true,
	".js": true, ".jsx": true, ".mjs": true,
	".ts": true, ".tsx": true,
	".py":   true,
	".rs":   true,
	".java": true,
	".cs":   true,
	".rb":   true,
	".php":  true,
	".kt":   true, ".kts": true,
	".swift": true,
	".scala": true,
	".cpp":   true, ".hpp": true, ".cc": true, ".hh": true,
	".c": true, ".h": true,
	".m": true, ".mm": true,
	".dart": true,
	".ex":   true, ".exs": true,
}

// ForFile returns the right Extractor for path based on file extension.
// Routing:
//   - .go → GoASTExtractor (stdlib parser, 100% precise)
//   - other known source extensions → WASMExtractor (when grammar bundled
//     and not building with -tags lite) or RegexExtractor (fallback)
//   - unknown extension → nil (skip)
func ForFile(path string) Extractor {
	ext := filepath.Ext(path)
	if ext == ".go" {
		return &GoASTExtractor{}
	}
	if !sourceExtensions[ext] {
		return nil
	}
	if useWASM(ext) {
		return newWASMExtractor(ext)
	}
	return &RegexExtractor{lang: extToLang(ext)}
}

// extToLang maps a file extension to the language key used by
// RegexExtractor + langPatterns. Unknown extensions fall back to
// "default" so the v0.12.0 universal patterns continue to apply.
func extToLang(ext string) string {
	switch ext {
	case ".php":
		return "php"
	case ".ts", ".tsx", ".js", ".jsx", ".mjs":
		return "ts"
	case ".py":
		return "python"
	default:
		return "default"
	}
}
