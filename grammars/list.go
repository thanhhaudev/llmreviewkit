//go:build !lite

package grammars

import (
	"os"
	"path/filepath"
	"strings"
)

// InstalledGrammar describes one grammar found by List.
type InstalledGrammar struct {
	Lang   string // matched against Registry; "" if unknown
	Path   string
	Size   int64
	Source string // "project" or "global"
}

// List walks both lookup paths and reports installed grammars. Project
// entries appear first; an entry that exists in both project + global
// is reported as "project" (since project overrides).
func List() ([]InstalledGrammar, error) {
	out := []InstalledGrammar{}
	seen := map[string]bool{}

	dirs := []struct {
		path   string
		source string
	}{}
	if d, err := ProjectDir(); err == nil {
		dirs = append(dirs, struct{ path, source string }{d, "project"})
	}
	if d, err := GlobalDir(); err == nil {
		dirs = append(dirs, struct{ path, source string }{d, "global"})
	}

	for _, d := range dirs {
		entries, err := os.ReadDir(d.path)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".wasm") {
				continue
			}
			grammarName := strings.TrimSuffix(ent.Name(), ".wasm")
			if seen[grammarName] {
				continue
			}
			seen[grammarName] = true
			info, _ := ent.Info()

			// Match grammar name back to a Registry lang key.
			lang := ""
			for k, e := range Registry {
				if e.GrammarName == grammarName {
					lang = k
					break
				}
			}
			if lang == "" {
				// Unknown grammar file — still report it with lang set to the file stem.
				lang = grammarName
			}

			out = append(out, InstalledGrammar{
				Lang:   lang,
				Path:   filepath.Join(d.path, ent.Name()),
				Size:   info.Size(),
				Source: d.source,
			})
		}
	}
	return out, nil
}
