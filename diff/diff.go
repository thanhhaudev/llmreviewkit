package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	xerrors "github.com/thanhhaudev/llmreviewkit/errors"
	"github.com/thanhhaudev/llmreviewkit/git"
)

type Bundle struct {
	TargetLabel     string
	Diff            string
	Untracked       []UntrackedFile
	TotalBytes      int
	Truncated       []string
	Warnings        []string
	ReferencedFiles []ReferencedFile // v0.12+: pre-flight context
}

type UntrackedFile struct {
	Path    string
	Content string
	Bytes   int
}

// ReferencedFile is a workspace file included in the prompt as context
// because the diff references symbols defined in it. Read-only — the LLM
// must not flag findings in referenced files.
type ReferencedFile struct {
	Path    string   // repo-relative
	Excerpt string   // ≤ per-file budget
	Symbols []string // symbol names matched here (priority sort signal)
}

func (b Bundle) IsEmpty() bool {
	return strings.TrimSpace(b.Diff) == "" && len(b.Untracked) == 0
}

// Collect dispatches based on target kind. For working-tree, also pulls in
// text-like untracked files (size <= 64KB each) so they get reviewed.
// For other targets, untracked is irrelevant.
func Collect(cwd string, target git.Target) (Bundle, error) {
	bundle := Bundle{TargetLabel: target.Label()}

	diffStr, err := git.Diff(cwd, target)
	if err != nil {
		return bundle, xerrors.Diff("collect_diff", fmt.Sprintf("collect diff: %v", err), "")
	}
	bundle.Diff = diffStr
	bundle.TotalBytes = len(diffStr)

	if target.Kind == git.TargetWorkingTree {
		bundle = appendUntracked(cwd, bundle, target.Paths)
	}

	if bundle.TotalBytes > MaxDiffBytes {
		bundle = applyCap(bundle)
	}

	return bundle, nil
}

func appendUntracked(cwd string, b Bundle, pathFilter []string) Bundle {
	untrackedPaths, err := git.UntrackedFiles(cwd)
	if err != nil {
		return b
	}

	root, _ := git.RootOf(cwd)
	pathSet := pathFilterSet(pathFilter)

	for _, rel := range untrackedPaths {
		if !matchesPathFilter(rel, pathSet) {
			continue
		}
		abs := filepath.Join(root, rel)
		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() {
			continue
		}
		if info.Size() > 64*1024 {
			b.Warnings = append(b.Warnings, fmt.Sprintf("skipped untracked file >64KB: %s", rel))
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			continue
		}
		if !isTextLike(data) {
			b.Warnings = append(b.Warnings, fmt.Sprintf("skipped binary untracked file: %s", rel))
			continue
		}
		b.Untracked = append(b.Untracked, UntrackedFile{
			Path:    rel,
			Content: string(data),
			Bytes:   len(data),
		})
		b.TotalBytes += len(data)
	}
	return b
}

func pathFilterSet(paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[strings.TrimRight(p, "/")] = true
	}
	return out
}

func matchesPathFilter(path string, set map[string]bool) bool {
	if set == nil {
		return true
	}
	for prefix := range set {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func applyCap(b Bundle) Bundle {
	for len(b.Untracked) > 0 && b.TotalBytes > MaxDiffBytes {
		idx := largestUntrackedIdx(b.Untracked)
		dropped := b.Untracked[idx]
		b.Untracked = append(b.Untracked[:idx], b.Untracked[idx+1:]...)
		b.TotalBytes -= dropped.Bytes
		b.Truncated = append(b.Truncated, dropped.Path)
	}

	if b.TotalBytes > MaxDiffBytes {
		over := b.TotalBytes - MaxDiffBytes
		if over < len(b.Diff) {
			b.Diff = b.Diff[:len(b.Diff)-over] + "\n[... diff truncated ...]\n"
			b.TotalBytes = len(b.Diff)
			b.Truncated = append(b.Truncated, "(diff body trimmed)")
		}
	}

	b.Warnings = append(b.Warnings,
		fmt.Sprintf("diff exceeded %d byte cap; truncated %d items", MaxDiffBytes, len(b.Truncated)))
	return b
}

func largestUntrackedIdx(files []UntrackedFile) int {
	idx := 0
	for i, f := range files {
		if f.Bytes > files[idx].Bytes {
			idx = i
		}
	}
	return idx
}

func isTextLike(data []byte) bool {
	limit := len(data)
	if limit > 512 {
		limit = 512
	}
	for i := 0; i < limit; i++ {
		if data[i] == 0 {
			return false
		}
	}
	return true
}

func RenderForPrompt(b Bundle) string {
	var sb strings.Builder
	if strings.TrimSpace(b.Diff) != "" {
		sb.WriteString("### diff\n\n```diff\n")
		sb.WriteString(b.Diff)
		sb.WriteString("\n```\n\n")
	}
	for _, f := range b.Untracked {
		sb.WriteString(fmt.Sprintf("### untracked: %s\n\n```\n", f.Path))
		sb.WriteString(f.Content)
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}
