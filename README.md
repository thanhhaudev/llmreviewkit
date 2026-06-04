# llmreviewkit

LLM-driven code review engine in Go. The provider-agnostic core extracted from [kizunax-plugin-cc](https://github.com/thanhhaudev/kizunax-plugin-cc).

## Install

```bash
go get github.com/thanhhaudev/llmreviewkit@latest
```

Requires Go 1.21+. No CGO. Stdlib + [wazero](https://github.com/tetratelabs/wazero) only.

## Quick start

```go
package main

import (
    "context"
    "fmt"

    "github.com/thanhhaudev/llmreviewkit/diff"
    "github.com/thanhhaudev/llmreviewkit/engine"
    "github.com/thanhhaudev/llmreviewkit/git"
    "github.com/thanhhaudev/llmreviewkit/prompt"
    "github.com/thanhhaudev/llmreviewkit/provider"
    "github.com/thanhhaudev/llmreviewkit/render"
)

func main() {
    prov := provider.NewOpenAI(provider.OpenAIConfig{
        BaseURL: "https://api.openai.com/v1",
        APIKey:  "sk-...",
        Model:   "gpt-4o-mini",
    })

    eng, err := engine.New(engine.Config{
        Provider:      prov,
        WorkspaceRoot: "/path/to/repo",
    })
    if err != nil {
        panic(err)
    }

    bundle, err := diff.Collect("/path/to/repo", git.Target{
        Kind:   git.TargetCommit,
        Commit: "HEAD",
    })
    if err != nil {
        panic(err)
    }

    result, err := eng.Review(context.Background(), bundle, engine.ReviewOptions{
        Mode: prompt.ModeStandard,
    })
    if err != nil {
        panic(err)
    }

    fmt.Println(render.RenderReview(result.Review, bundle, result.TotalTokens, prompt.ModeStandard))
}
```

See [`examples/`](examples/) for runnable variations.

## Providers

- **OpenAI-compatible**: `provider.NewOpenAI(provider.OpenAIConfig{...})` — works with OpenAI, Together.ai, Groq, KizunaX, local Ollama, and any HTTP endpoint speaking the OpenAI chat completions API.
- **Anthropic-compatible**: `provider.NewAnthropic(provider.AnthropicConfig{...})` — works with Anthropic Claude and any Anthropic-compatible proxy.
- **Bring your own**: implement the `provider.Provider` interface (`Name()`, `Chat()`, `Probe()`).

## Sub-packages

| Package | What it does |
|---|---|
| `engine` | High-level entry. `New(cfg).Review(ctx, bundle, opts)` runs the full pipeline. |
| `diff` | Git diff collection + reference attachment with byte budget. |
| `git` | Target abstraction (working tree, commit, range, paths). |
| `schema` | `ReviewResult` types + lenient JSON parser. Embeds the default JSON Schema. |
| `render` | Markdown rendering of a review result. |
| `prompt` | Mode (Standard / Adversarial) + template interpolation. Embeds default templates. |
| `provider` | Provider interface + OpenAI / Anthropic adapters + `mock` sub-package for tests. |
| `symbols` | AST symbol extraction (Go stdlib AST + tree-sitter WASM grammars + regex fallback). |
| `resolver` | Cross-references diff symbols against the workspace. v1 regex + v2 index-backed. |
| `index` | Workspace AST index — per-workspace symbol map with mtime-incremental updates. |
| `grammars` | Runtime registry for tree-sitter `.wasm` grammars. Call `grammars.Install` to fetch + cache. |
| `bundlelog` | Optional jsonl telemetry. `AppendTo(sink, entry)` — bring your own `io.Writer`. |
| `statedir` | Per-workspace state directory + atomic write helpers. |
| `glossary` | Project glossary file loader. |
| `errors` | Structured error kinds for user vs. provider vs. internal failures. |

## Grammars

Tree-sitter `.wasm` grammars (Python, PHP, TypeScript) are not bundled — they're fetched on demand:

```go
import "github.com/thanhhaudev/llmreviewkit/grammars"

ctx := context.Background()
if err := grammars.Install(ctx, "python", false); err != nil {
    // Falls back to regex extraction if missing — strictly additive.
}
```

Go uses the stdlib AST extractor and needs no setup. Unknown languages fall back to regex automatically.

## License

[MIT](LICENSE). Inspired by [codex-plugin-cc](https://github.com/openai/codex-plugin-cc).
