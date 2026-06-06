//go:build !lite

package symbols

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExtractCache stores extracted Symbol slices on disk keyed by content hash
// and strategy. Layout:
//
//	<root>/<sha256-hex[0:2]>/<sha256-hex>.json
//
// Entries are immutable — rewrites overwrite atomically via temp-file rename.
// On any I/O or JSON error during read or write, the cache falls back to
// running the extractor and returning the result without caching, so the
// cache never blocks extraction.
//
// The cache is process-safe but not designed for high concurrent contention
// on the same key. Multiple processes writing the same key may race on the
// rename; the loser's write is discarded (atomic rename).
//
// Thread-safety: GetOrExtract is safe to call from multiple goroutines as
// long as the underlying extractor is. No internal locks; relies on the OS
// for atomic rename + file open semantics.
type ExtractCache struct {
	Root string
}

// NewExtractCache returns a cache rooted at the given directory. The directory
// is created on the first write via os.MkdirAll. Pass an absolute path.
func NewExtractCache(root string) *ExtractCache {
	return &ExtractCache{Root: root}
}

type cacheEntry struct {
	Path     string   `json:"path"`
	Strategy int      `json:"strategy"`
	Symbols  []Symbol `json:"symbols"`
}

// key derives the on-disk filename for a (path, content, strategy) tuple.
// Including strategy means switching strategies invalidates cached entries
// without manual cleanup.
func (c *ExtractCache) key(path string, content []byte, strategy int) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(content)
	h.Write([]byte{0})
	fmt.Fprintf(h, "strategy:%d", strategy)
	return hex.EncodeToString(h.Sum(nil))
}

func (c *ExtractCache) entryPath(key string) string {
	if len(key) < 2 {
		return filepath.Join(c.Root, key+".json")
	}
	return filepath.Join(c.Root, key[:2], key+".json")
}

// GetOrExtract returns the cached symbol slice for (path, content) if present
// and the strategy matches, otherwise runs extract() and persists the result.
//
// The strategy int parameter is for cache-key isolation only; the actual
// extractor implementation is provided as the extract callback. This keeps
// ExtractCache decoupled from DispatchPHP.
//
// Returns: (syms, true, nil) on a hit, (syms, false, nil) on a miss
// (extraction ran and was written to cache), (extracted-syms, false, error)
// when extraction ran but persistence failed (the symbols are still returned).
func (c *ExtractCache) GetOrExtract(
	path string,
	content []byte,
	strategy int,
	extract func(path string, content []byte) []Symbol,
) ([]Symbol, bool, error) {
	key := c.key(path, content, strategy)
	fp := c.entryPath(key)

	// Read path. Soft-fail on any error.
	if data, err := os.ReadFile(fp); err == nil {
		var entry cacheEntry
		if json.Unmarshal(data, &entry) == nil && entry.Strategy == strategy {
			return entry.Symbols, true, nil
		}
	}

	// Miss: extract + persist.
	syms := extract(path, content)

	entry := cacheEntry{
		Path:     path,
		Strategy: strategy,
		Symbols:  syms,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return syms, false, fmt.Errorf("marshal cache entry: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return syms, false, fmt.Errorf("mkdir cache shard: %w", err)
	}
	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return syms, false, fmt.Errorf("write cache temp: %w", err)
	}
	if err := os.Rename(tmp, fp); err != nil {
		// Clean up the orphaned temp on rename failure (best-effort).
		_ = os.Remove(tmp)
		return syms, false, fmt.Errorf("rename cache file: %w", err)
	}
	return syms, false, nil
}
