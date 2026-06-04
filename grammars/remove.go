//go:build !lite

package grammars

import (
	"fmt"
	"os"
	"path/filepath"
)

// Remove deletes the grammar for lang from either ~/.kizunax/grammars/
// (project=false) or ./.kizunax/grammars/ (project=true).
func Remove(lang string, project bool) error {
	entry, ok := Registry[lang]
	if !ok {
		return fmt.Errorf("unknown language %q", lang)
	}
	var dir string
	var err error
	if project {
		dir, err = ProjectDir()
	} else {
		dir, err = GlobalDir()
	}
	if err != nil {
		return err
	}
	path := filepath.Join(dir, entry.GrammarName+".wasm")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("%s not installed at %s", lang, path)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	fmt.Printf("Removed %s from %s\n", lang, path)
	return nil
}
