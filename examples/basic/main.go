// Basic llmreviewkit usage: review a commit using OpenAI-compatible provider.
//
// Build:
//
//	go build -o /tmp/llmrev-basic ./examples/basic
//
// Run:
//
//	OPENAI_API_KEY=sk-... /tmp/llmrev-basic /path/to/repo HEAD
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/engine"
	"github.com/thanhhaudev/llmreviewkit/git"
	"github.com/thanhhaudev/llmreviewkit/prompt"
	"github.com/thanhhaudev/llmreviewkit/provider"
	"github.com/thanhhaudev/llmreviewkit/render"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("usage: basic <workspace-root> <commit-ref>")
	}
	workspace, commit := os.Args[1], os.Args[2]
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	prov := provider.NewOpenAI(provider.OpenAIConfig{
		BaseURL: "https://api.openai.com/v1",
		APIKey:  key,
		Model:   "gpt-4o-mini",
	})

	eng, err := engine.New(engine.Config{Provider: prov, WorkspaceRoot: workspace})
	if err != nil {
		log.Fatal(err)
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
}
