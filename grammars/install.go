//go:build !lite

package grammars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Install downloads the grammar for lang from its registered CDN URL,
// verifies SHA256, and writes atomically to either ~/.kizunax/grammars/
// (project=false) or ./.kizunax/grammars/ (project=true).
func Install(ctx context.Context, lang string, project bool) error {
	entry, ok := Registry[lang]
	if !ok {
		return fmt.Errorf("unknown language %q (known: see 'kizunax grammars list')", lang)
	}
	return installFromURL(ctx, entry, entry.CDNUrl(), project)
}

// installFromURL is the testable entry point that takes an explicit URL
// (so tests can use httptest.Server).
func installFromURL(ctx context.Context, entry Entry, url string, project bool) error {
	var targetDir string
	var err error
	if project {
		targetDir, err = ProjectDir()
	} else {
		targetDir, err = GlobalDir()
	}
	if err != nil {
		return fmt.Errorf("target dir: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http %d from %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	sum := sha256.Sum256(data)
	gotHash := hex.EncodeToString(sum[:])
	if gotHash != entry.SHA256 {
		return fmt.Errorf("SHA256 mismatch for %s: got %s, expected %s (registry stale?)",
			entry.Lang, gotHash, entry.SHA256)
	}

	dest := filepath.Join(targetDir, entry.GrammarName+".wasm")
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	fmt.Printf("Installed %s grammar to %s (%d bytes)\n", entry.Lang, dest, len(data))
	return nil
}
