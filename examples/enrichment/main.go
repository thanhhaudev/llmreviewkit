// Enrichment example: enables the v0.13 index-backed resolver and writes
// telemetry to a bundle log file. First run builds the index synchronously
// via SyncIndex; subsequent runs use the incremental update path.
//
// Build:
//
//	go build -o /tmp/llmrev-enrich ./examples/enrichment
//
// Run:
//
//	OPENAI_API_KEY=sk-... /tmp/llmrev-enrich /path/to/repo HEAD
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/engine"
	"github.com/thanhhaudev/llmreviewkit/git"
	"github.com/thanhhaudev/llmreviewkit/prompt"
	"github.com/thanhhaudev/llmreviewkit/provider"
	"github.com/thanhhaudev/llmreviewkit/render"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("usage: enrichment <workspace-root> <commit-ref>")
	}
	workspace, commit := os.Args[1], os.Args[2]
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	stateDir := filepath.Join(os.TempDir(), "llmreviewkit-example")
	_ = os.MkdirAll(stateDir, 0o700)

	logPath := filepath.Join(stateDir, "bundle-log.jsonl")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatalf("open bundle log: %v", err)
	}
	defer logFile.Close()

	prov := provider.NewOpenAI(provider.OpenAIConfig{
		BaseURL: "https://api.openai.com/v1",
		APIKey:  key,
		Model:   "gpt-4o-mini",
	})

	eng, err := engine.New(engine.Config{
		Provider:      prov,
		WorkspaceRoot: workspace,
		StateDir:      stateDir,
		UseIndex:      true,
		BundleLogSink: logFile,
		Verbose:       true,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Warm the index synchronously the first time.
	if err := eng.SyncIndex(); err != nil {
		log.Printf("[warn] SyncIndex: %v (review will fall back to v1)", err)
	}

	bundle, err := diff.Collect(workspace, git.Target{
		Kind:   git.TargetCommit,
		Commit: commit,
	})
	if err != nil {
		log.Fatal(err)
	}

	res, err := eng.Review(context.Background(), bundle, engine.ReviewOptions{Mode: prompt.ModeStandard})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(render.RenderReview(res.Review, bundle, res.TotalTokens, prompt.ModeStandard))
	fmt.Printf("\n[telemetry] resolver_path=%s index_hits=%d index_misses=%d\n",
		res.Stats.ResolverPath, res.Stats.IndexHits, res.Stats.IndexMisses)
}
