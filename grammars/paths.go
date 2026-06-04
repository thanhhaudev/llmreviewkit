//go:build !lite

package grammars

import (
	"os"
	"path/filepath"
)

// GlobalDir returns ~/.kizunax/grammars/, creating it on first call.
func GlobalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".kizunax", "grammars")
	if err := os.MkdirAll(d, 0755); err != nil {
		return "", err
	}
	return d, nil
}

// ProjectDir returns ./.kizunax/grammars/ relative to cwd, creating it.
func ProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	d := filepath.Join(cwd, ".kizunax", "grammars")
	if err := os.MkdirAll(d, 0755); err != nil {
		return "", err
	}
	return d, nil
}
