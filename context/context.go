// Package context loads .kizunax/review-context.md from a workspace.
// Mirrors the glossary package: priority paths, UTF-8 safe truncation,
// verbatim content (no parsing).
//
// Engine consumers inject the loaded content into the LLM system prompt
// as a "Prior review context" section, ordered ABOVE glossary so the
// strongest behavioral hints come first.
package context

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

const maxContextBytes = 8 * 1024

// Context is the loaded review-context.md content + metadata for the
// kizunax runner to surface staleness warnings.
type Context struct {
	Path      string    // empty if no file found
	Content   string    // verbatim, possibly truncated
	Truncated bool      // true if file was > 8 KiB
	ModTime   time.Time // file mtime, zero if no file
}

// DefaultPaths returns the default candidate paths searched by Load when
// the caller passes nil. Order is priority-descending. The `.kizunax/`
// path is intentionally excluded — that path belongs to the consumer
// (kizunax), not this library. Consumers that want it should compose:
//
//	paths := append([]string{".kizunax/review-context.md"}, context.DefaultPaths()...)
//	c, err := context.Load(workspaceRoot, paths)
func DefaultPaths() []string {
	return []string{
		filepath.Join("docs", "review-context.md"),
		"REVIEW-CONTEXT.md",
	}
}

// Load searches workspaceRoot for review-context.md in the order given
// by candidatePaths and returns the verbatim content capped at 8 KiB.
// Passing nil candidatePaths uses DefaultPaths(). No file found → empty
// Context, nil error.
func Load(workspaceRoot string, candidatePaths []string) (Context, error) {
	if candidatePaths == nil {
		candidatePaths = DefaultPaths()
	}
	for _, rel := range candidatePaths {
		abs := filepath.Join(workspaceRoot, rel)
		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Context{}, fmt.Errorf("stat %s: %w", abs, err)
		}
		if info.IsDir() {
			continue
		}
		f, err := os.Open(abs)
		if err != nil {
			return Context{}, fmt.Errorf("open %s: %w", abs, err)
		}
		defer f.Close()

		buf, err := io.ReadAll(io.LimitReader(f, int64(maxContextBytes)+1))
		if err != nil {
			return Context{}, fmt.Errorf("read %s: %w", abs, err)
		}
		truncated := len(buf) > maxContextBytes
		content := string(buf)
		if truncated {
			content = truncateUTF8(content, maxContextBytes)
		}
		return Context{
			Path:      abs,
			Content:   content,
			Truncated: truncated,
			ModTime:   info.ModTime(),
		}, nil
	}
	return Context{}, nil
}

// truncateUTF8 returns s shortened to at most maxBytes, snapped back to the
// nearest rune boundary so the result is always valid UTF-8.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for i := maxBytes; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return s[:i]
		}
	}
	return ""
}
