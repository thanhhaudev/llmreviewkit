package resolver

import (
	"path/filepath"

	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// Hardcoded stdlib package skip lists per language. Symbols whose Pkg
// matches a stdlib package are skipped — the LLM already knows them.
//
// Coverage focuses on the most-referenced stdlib packages; extending each
// list is an ongoing follow-up.

var goStdlibPkgs = map[string]bool{
	"fmt": true, "os": true, "io": true, "ioutil": true,
	"strings": true, "strconv": true, "bytes": true, "bufio": true,
	"errors": true, "context": true, "time": true,
	"sync": true, "atomic": true,
	"path": true, "filepath": true,
	"regexp":        true,
	"encoding/json": true, "encoding/hex": true, "encoding/base64": true,
	"encoding/binary": true, "encoding/csv": true,
	"net/http": true, "net/url": true, "net": true,
	"go/ast": true, "go/parser": true, "go/token": true, "go/types": true,
	"reflect": true,
	"unicode": true, "unicode/utf8": true,
	"sort": true,
	"math": true, "math/rand": true, "math/big": true,
	"hash": true, "hash/sha256": true, "hash/sha1": true,
	"crypto": true, "crypto/sha256": true, "crypto/rand": true, "crypto/tls": true,
	"log": true, "log/slog": true,
	"flag":    true,
	"runtime": true, "runtime/debug": true,
	"testing": true,
	"embed":   true,
}

var pythonStdlibPkgs = map[string]bool{
	// Stdlib.
	"os": true, "sys": true, "io": true, "json": true, "yaml": true,
	"re": true, "typing": true, "collections": true, "itertools": true,
	"functools": true, "datetime": true, "time": true, "uuid": true,
	"pathlib": true, "subprocess": true, "logging": true,
	"asyncio": true, "concurrent": true, "threading": true,
	"http": true, "urllib": true, "socket": true,
	"unittest": true, "pytest": true,
	"abc": true, "dataclasses": true, "enum": true,
	// v0.12.3 stdlib adds.
	"argparse": true, "tempfile": true, "shutil": true, "pickle": true,
	"hashlib": true, "base64": true, "random": true, "math": true,
	"decimal": true, "weakref": true, "copy": true,
	// v0.12.3 third-party adds (frequently emitted by AST extraction
	// as Pkg= via the decorator method-qualifier branch).
	"flask": true, "django": true, "requests": true, "numpy": true,
	"pandas": true, "sqlalchemy": true, "pydantic": true,
	"fastapi": true, "starlette": true, "redis": true, "celery": true,
}

var tsStdlibPkgs = map[string]bool{
	"fs": true, "path": true, "os": true, "http": true, "https": true,
	"net": true, "url": true, "util": true, "crypto": true,
	"stream": true, "events": true, "buffer": true, "child_process": true,
	"process": true, "console": true,
	"react": true, "vue": true, "@angular/core": true, // common framework imports
}

// PHP vendor namespaces (Symfony/Laravel/Doctrine etc.) and PSR roots.
// Match on the top-level namespace component — kizunax emits namespaced
// calls with Pkg containing the leading segment (e.g. Pkg="Symfony" for
// Symfony\Component\HttpFoundation\Request) and SymImport-by-name for
// `use Symfony\...` statements.
var phpStdlibPkgs = map[string]bool{
	"Symfony": true, "Laravel": true, "Illuminate": true,
	"Doctrine": true, "Psr": true, "Monolog": true,
	"PHPUnit": true, "Twig": true, "Carbon": true,
	"Guzzle": true, "GuzzleHttp": true,
}

// pythonBuiltinNames are global Python builtins surfaced by AST extraction
// as bare-name SymCall (empty Pkg). Filtering here saves the resolver from
// chasing undefined-elsewhere call sites like `print("...")`. SymDef and
// attribute-call cases (non-empty Pkg) are NOT filtered — user code may
// shadow `print` etc., and resolver should still link those.
var pythonBuiltinNames = map[string]bool{
	"print": true, "len": true, "range": true, "str": true, "int": true,
	"float": true, "bool": true, "dict": true, "list": true, "set": true,
	"tuple": true, "bytes": true,
	"isinstance": true, "issubclass": true, "hasattr": true, "getattr": true,
	"setattr": true, "delattr": true,
	"open": true, "enumerate": true, "zip": true, "map": true, "filter": true,
	"sorted": true, "reversed": true, "min": true, "max": true, "sum": true,
	"abs": true, "round": true, "super": true, "type": true, "repr": true,
	"iter": true, "next": true, "any": true, "all": true,
}

// phpBuiltinNames are idiomatic PHP global functions surfaced as bare-name
// SymCall (empty Pkg). Same filter discipline as pythonBuiltinNames:
// SymDef and namespaced calls are not filtered.
var phpBuiltinNames = map[string]bool{
	"count": true, "sizeof": true, "strlen": true, "strpos": true,
	"strrpos": true, "substr": true, "str_replace": true, "str_contains": true,
	"str_starts_with": true, "str_ends_with": true,
	"explode": true, "implode": true, "trim": true, "ltrim": true, "rtrim": true,
	"array_map": true, "array_filter": true, "array_reduce": true,
	"array_keys": true, "array_values": true, "array_merge": true,
	"array_combine": true, "array_search": true, "array_walk": true,
	"is_array": true, "is_string": true, "is_int": true, "is_null": true,
	"is_bool": true, "isset": true, "empty": true,
	"var_dump": true, "print_r": true, "json_encode": true, "json_decode": true,
	"sprintf": true, "printf": true,
	"defined": true, "function_exists": true, "method_exists": true,
	"class_exists": true,
}

// IsStdlibSymbol returns true if sym refers to a known stdlib package
// for the source language of sym.File. Language-scoped to prevent
// false collisions across ecosystems — e.g. a Go project package named
// `util` would otherwise be filtered by the Node `util` module entry.
// When sym.File has an unknown extension, returns false (fail-open so
// resolver can search for a definition).
func IsStdlibSymbol(sym symbols.Symbol) bool {
	var pkgs map[string]bool
	switch filepath.Ext(sym.File) {
	case ".go":
		pkgs = goStdlibPkgs
	case ".py":
		pkgs = pythonStdlibPkgs
	case ".ts", ".tsx", ".js", ".jsx", ".mjs":
		pkgs = tsStdlibPkgs
	case ".php":
		pkgs = phpStdlibPkgs
	default:
		return false
	}
	if sym.Pkg != "" && pkgs[sym.Pkg] {
		return true
	}
	if sym.Kind == symbols.SymImport && pkgs[sym.Name] {
		return true
	}
	// Bare-name builtin filter: only applies to SymCall with empty Pkg, so
	// SymDef (user may shadow the builtin) and attribute calls (user code)
	// remain unfiltered.
	if sym.Kind == symbols.SymCall && sym.Pkg == "" {
		var builtins map[string]bool
		switch filepath.Ext(sym.File) {
		case ".py":
			builtins = pythonBuiltinNames
		case ".php":
			builtins = phpBuiltinNames
		}
		if builtins != nil && builtins[sym.Name] {
			return true
		}
	}
	return false
}
