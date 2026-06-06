# llmreviewkit

A Go library that asks an LLM to review your code changes and returns the
findings as structured data.

## What it is

You give it a git target — a commit, a commit range, the working tree, or a
list of paths — and it returns a `ReviewResult` (a list of findings with
severity, file, line, and suggestion).

The provider is pluggable: any OpenAI-compatible endpoint, any
Anthropic-compatible endpoint, or your own `provider.Provider`.

There is no agent loop, no shell access, no file editing. The library reads
the diff (and, if you opt in, a few small workspace excerpts around the
symbols it touches), calls the model once (with one retry if the JSON is
malformed), and returns typed data. Turning that data into markdown is a
separate, optional step.

## How it works

```
   git target ─┐
               │
               ▼
        ┌──────────────┐    ┌────────────────────────────┐
        │ diff.Collect │ ─▶ │ Bundle                     │
        └──────────────┘    │  · unified diff            │
                            │  · (optional) referenced   │
                            │    files + symbols around  │
                            │    the diff                │
                            └─────────────┬──────────────┘
                                         │
                                         ▼
                            ┌──────────────────────────┐
                            │ prompt.Build (Standard | │
                            │   Adversarial)           │
                            └────────────┬─────────────┘
                                         │
                                         ▼
                            ┌──────────────────────────┐
                            │ provider.Chat            │
                            │   forced JSON schema     │
                            │   (OpenAI strict /       │
                            │    Anthropic tool_use)   │
                            └────────────┬─────────────┘
                                         │
                                         ▼
                            ┌──────────────────────────┐
                            │ schema.Parse (lenient —  │
                            │   strips code fences and │
                            │   extra prose)           │
                            └────────────┬─────────────┘
                                         │
                                         ▼
                                 ReviewResult ──▶ render.RenderReview
```

A few things worth knowing:

1. **Code context is optional.** Set `WorkspaceRoot` on `engine.Config` and
   the library extracts symbols from the diff, finds their definitions in
   the workspace (regex-based by default), and attaches short excerpts to
   the prompt within a byte budget. Add `UseIndex: true` *and* populate the
   on-disk index (call `engine.SyncIndex()` once, or build it out-of-band)
   to upgrade resolution to the AST-backed v2 path. Setting `UseIndex: true`
   alone is not enough — if no index is on disk yet, the engine silently
   falls back to the v1 regex resolver. The
   [`enrichment` example](examples/enrichment) shows the warm-up call.
   Leave `WorkspaceRoot` empty and you get a pure diff review. (See
   [Grammars](#grammars) for the languages covered.)
2. **JSON output is enforced at the API level, not by prompting.** The
   OpenAI adapter uses `response_format: {type:"json_schema", strict:true}`
   (falling back to plain message with the schema in the prompt on a 400
   error). The Anthropic adapter forces `tool_choice: {type:"tool",
   name:"submit_review"}`. The model can't reply with free-form prose.
3. **One retry on parse failure.** If `schema.Parse` rejects the response
   (truncated JSON, extra wrapper object, code fence around the JSON, …),
   the library sends one corrective prompt and tries again before giving
   up.
4. **Prompt templates are overridable.** Defaults for both modes are embedded
   in the `prompt` package (`embedded/review.md`,
   `embedded/adversarial-review.md`). Set `PromptRoot` on `engine.Config` to
   a directory and the engine reads `<PromptRoot>/prompts/review.md` (for
   standard mode) or `<PromptRoot>/prompts/adversarial-review.md` (for
   adversarial mode) instead. The filenames are fixed by
   `prompt.Mode.TemplateFile()`; use those exact names, or the read fails.

## Install

```bash
go get github.com/thanhhaudev/llmreviewkit@latest
```

Requires Go 1.21+. No CGO. Stdlib + [wazero](https://github.com/tetratelabs/wazero)
only.

## Quick start

Review `HEAD` against an OpenAI-compatible endpoint:

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

## Anatomy of a review

A walk-through of what crosses the wire for a realistic, multi-file commit
that touches a token parser, adds a new cache layer, and updates the login
handler to use it.

**1. The diff** (what `diff.Collect` reads):

```diff
diff --git a/internal/auth/token.go b/internal/auth/token.go
--- a/internal/auth/token.go
+++ b/internal/auth/token.go
@@ -40,9 +40,10 @@ var ErrInvalidToken = errors.New("invalid token format")
 func ParseToken(raw string) (*Token, error) {
        parts := strings.Split(raw, ".")
-       if len(parts) != 3 {
+       if len(parts) < 3 {
                return nil, ErrInvalidToken
        }
+       // TODO: verify signature once we have the public key wired up.
        return &Token{Header: parts[0], Payload: parts[1], Signature: parts[2]}, nil
 }

diff --git a/internal/auth/cache.go b/internal/auth/cache.go
new file mode 100644
--- /dev/null
+++ b/internal/auth/cache.go
@@ -0,0 +1,24 @@
+package auth
+
+import "time"
+
+type entry struct {
+       tok     *Token
+       expires time.Time
+}
+
+var cache = map[string]entry{}
+
+func CacheGet(raw string) (*Token, bool) {
+       e, ok := cache[raw]
+       if !ok || time.Now().After(e.expires) {
+               return nil, false
+       }
+       return e.tok, true
+}
+
+func CachePut(raw string, tok *Token, ttl time.Duration) {
+       cache[raw] = entry{tok: tok, expires: time.Now().Add(ttl)}
+}

diff --git a/internal/handlers/login.go b/internal/handlers/login.go
--- a/internal/handlers/login.go
+++ b/internal/handlers/login.go
@@ -23,11 +23,16 @@ func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
                http.Error(w, "missing token", http.StatusBadRequest)
                return
        }
-       tok, err := auth.ParseToken(raw)
-       if err != nil {
-               http.Error(w, "unauthorized", http.StatusUnauthorized)
-               return
+       tok, ok := auth.CacheGet(raw)
+       if !ok {
+               var err error
+               tok, err = auth.ParseToken(raw)
+               if err != nil {
+                       http.Error(w, "unauthorized", http.StatusUnauthorized)
+                       return
+               }
+               auth.CachePut(raw, tok, 5*time.Minute)
        }
        h.writeSession(w, tok)
 }
```

**2. The prompt sent to the model** (Standard mode template, abbreviated):

```
You are a senior code reviewer. Your job is to review a diff and identify
real, actionable issues.

Review target: commit abc1234

Focus on:
- Correctness bugs (off-by-one, nil pointers, error handling gaps)
- Race conditions, concurrency issues
- Security concerns (injection, auth bypass, data leak)
- ...

Return ONLY a JSON object matching this schema:

{ "verdict": "approve" | "needs-attention",
  "summary": "string",
  "findings": [{ "severity": "low|medium|high|critical", "title": "string",
                 "body": "string", "file": "string",
                 "line_start": int, "line_end": int,
                 "confidence": float, "recommendation": "string" }],
  "next_steps": ["string"] }

Diff to review:

<the diff from step 1 here>
```

**3. With enrichment enabled** — when `WorkspaceRoot` is set on
`engine.Config` (and ideally `UseIndex: true` for AST-backed resolution),
the engine extracts symbols from the diff — here, the `Token` type
referenced by the new `cache.go` and the `Handler.writeSession` method
called by the login handler — resolves them against the workspace, and
appends matching excerpts (≤ ~512 B per file) to the prompt before
sending. The extra block looks like:

````
## Referenced files for context (read-only)

These files contain definitions referenced by symbols in the diff.
Use them to understand types, constants, and helpers.
DO NOT flag findings in these files — they are unchanged context.

### internal/auth/types.go (matched: Token)
```
type Token struct {
    Header    string
    Payload   string
    Signature string
}

func (t *Token) Valid() bool {
    return t.Header != "" && t.Payload != "" && t.Signature != ""
}
```

### internal/handlers/handler.go (matched: writeSession)
```
func (h *Handler) writeSession(w http.ResponseWriter, tok *auth.Token) {
    cookie := &http.Cookie{Name: "sid", Value: tok.Signature, HttpOnly: true}
    http.SetCookie(w, cookie)
}
```
````

Each referenced file is annotated with the symbols that pulled it in
(`(matched: ...)`). The excerpts are short byte slices around the matched
line, so neighboring definitions (like `Valid()` above) often come along
for free. Total enrichment size is capped by `EnrichBudget` (96 KiB by
default when expansion is on, 32 KiB otherwise) — anything beyond gets
dropped with a warning in `Stats`.

**4. The model's response** — JSON matching the schema (the enforcement
mechanism is described in [How it works](#how-it-works)):

```json
{
  "verdict": "needs-attention",
  "summary": "Three issues: ParseToken now accepts malformed tokens, the new cache has a data race under concurrent logins, and a TODO returns a token that hasn't been signature-verified.",
  "findings": [
    {
      "severity": "high",
      "title": "ParseToken silently accepts tokens with more than 3 segments",
      "body": "Loosening `len(parts) != 3` to `len(parts) < 3` lets a 4-segment input through, but the code still reads only parts[0..2] — the trailing segments are silently discarded.",
      "file": "internal/auth/token.go",
      "line_start": 41,
      "line_end": 44,
      "confidence": 0.92,
      "recommendation": "Revert to `!= 3`, or reject `> 3` with a distinct error."
    },
    {
      "severity": "high",
      "title": "Token cache map is mutated without synchronization",
      "body": "`cache` is a plain map written by `CachePut` and read by `CacheGet`. Under concurrent logins this is a data race and will panic with a concurrent map read/write under -race.",
      "file": "internal/auth/cache.go",
      "line_start": 10,
      "line_end": 24,
      "confidence": 0.98,
      "recommendation": "Guard the map with a `sync.RWMutex`, or switch to `sync.Map`. Add a test that stress-tests it with `t.Parallel()` + `-race`."
    },
    {
      "severity": "medium",
      "title": "ParseToken returns a token before signature verification",
      "body": "The new TODO acknowledges the gap, but callers (including the new cache path) treat the returned token as authenticated. Until the signature check is added, any well-formed `a.b.c` string creates a session.",
      "file": "internal/auth/token.go",
      "line_start": 45,
      "line_end": 46,
      "confidence": 0.85,
      "recommendation": "Either add the signature check in this commit or guard callers with a feature flag that defaults to off in production."
    }
  ],
  "next_steps": [
    "Add a race-detector test for the cache.",
    "Decide whether signature verification belongs in this commit or behind a flag."
  ]
}
```

**5. The rendered markdown** that `render.RenderReview` produces:

```markdown
## ⚠ Review verdict: needs-attention

_Target: commit abc1234_

Three issues: ParseToken now accepts malformed tokens, the new cache has
a data race under concurrent logins, and a TODO returns a token that
hasn't been signature-verified.

### Findings (3)

| # | Severity | Location | Title |
|---|---|---|---|
| 1 | 🟠 high | `internal/auth/token.go:41-44` | ParseToken silently accepts tokens with more than 3 segments |
| 2 | 🟠 high | `internal/auth/cache.go:10-24` | Token cache map is mutated without synchronization |
| 3 | 🟡 medium | `internal/auth/token.go:45-46` | ParseToken returns a token before signature verification |

#### 1. ParseToken silently accepts tokens with more than 3 segments `[high, confidence 0.92]`

**File**: `internal/auth/token.go:41-44`

Loosening `len(parts) != 3` to `len(parts) < 3` lets a 4-segment input
through, but the code still reads only parts[0..2] — the trailing segments
are silently discarded.

**Recommendation**: Revert to `!= 3`, or reject `> 3` with a distinct error.

#### 2. Token cache map is mutated without synchronization `[high, confidence 0.98]`

**File**: `internal/auth/cache.go:10-24`

`cache` is a plain map written by `CachePut` and read by `CacheGet`. Under
concurrent logins this is a data race and will panic with a concurrent map
read/write under -race.

**Recommendation**: Guard the map with a `sync.RWMutex`, or switch to
`sync.Map`. Add a test that stress-tests it with `t.Parallel()` + `-race`.

#### 3. ParseToken returns a token before signature verification `[medium, confidence 0.85]`

**File**: `internal/auth/token.go:45-46`

The new TODO acknowledges the gap, but callers (including the new cache
path) treat the returned token as authenticated...

**Recommendation**: Either add the signature check in this commit or guard
callers with a feature flag that defaults to off in production.

### Next steps

1. Add a race-detector test for the cache.
2. Decide whether signature verification belongs in this commit or behind a flag.

_Tokens used: 2418_
```

## Examples

Runnable variants live under [`examples/`](examples/):

| Example | What it shows |
|---|---|
| [`basic`](examples/basic) | Bare-minimum review of a commit: provider + diff + `Review` call. Uses the default regex resolver when `WorkspaceRoot` is set. |
| [`enrichment`](examples/enrichment) | Adds the AST-backed index (`UseIndex: true`) and a `BundleLogSink` for telemetry. First run builds the index synchronously; later runs only re-scan changed files. |

Each is a self-contained `main.go`. Build with `go build ./examples/<name>`,
run with `OPENAI_API_KEY` set.

## Providers

- **OpenAI-compatible**: `provider.NewOpenAI(provider.OpenAIConfig{...})` — works
  with OpenAI, Together.ai, Groq, local Ollama, and any HTTP endpoint speaking
  the OpenAI chat completions API.
- **Anthropic-compatible**: `provider.NewAnthropic(provider.AnthropicConfig{...})`
  — works with Anthropic Claude and any Anthropic-compatible proxy.
- **Bring your own**: implement the `provider.Provider` interface (`Name()`,
  `Chat()`, `Probe()`).

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
| `symbols` | Symbol extraction (Go stdlib AST + [phpsyms](https://github.com/thanhhaudev/phpsyms) for PHP + tree-sitter WASM for TypeScript / Python + regex fallback). PHP routed via `engine.ExtractionPolicy`. |
| `resolver` | Cross-references diff symbols against the workspace. v1 regex + v2 index-backed. |
| `index` | Workspace AST index — per-workspace symbol map with mtime-incremental updates. |
| `grammars` | Runtime registry for tree-sitter `.wasm` grammars. Call `grammars.Install` to fetch + cache. |
| `bundlelog` | Optional jsonl telemetry. `AppendTo(sink, entry)` — bring your own `io.Writer`. |
| `statedir` | Per-workspace state directory + atomic write helpers. |
| `glossary` | Project glossary file loader. |
| `context` | Project review-context file loader. Injects behavioral hints (intentional patterns, suppressed categories) above the glossary in the system prompt. v1.5.1+. |
| `errors` | Structured error kinds for user vs. provider vs. internal failures. |

## Grammars

Symbol extraction has five paths. Pick the row that matches your source files:

| Extraction path | Languages |
|---|---|
| Go stdlib AST | Go |
| Go-native [phpsyms](https://github.com/thanhhaudev/phpsyms) | PHP — default since v1.5.0 (~5000 files/sec, stdlib-only, no WASM) |
| Tree-sitter WASM | Python, TypeScript (and TSX); PHP as a fallback when `ExtractionPolicy.PHP = StrategyTreeSitter` |
| Regex (per-language tuned) | PHP, Python, TypeScript — used when the chosen path returns no symbols or the WASM grammar isn't installed |
| Regex (universal default) | Every other extension — JavaScript / JSX / MJS, Rust, Java, C#, Ruby, Kotlin, Swift, Scala, C / C++. Catches `func / def / class / import / …` well enough to surface symbols, but it isn't AST-quality. |

More languages will be promoted from regex to tree-sitter over time.

PHP routes through `engine.Config.ExtractionPolicy.PHP`:

```go
cfg.ExtractionPolicy.PHP = engine.StrategyAuto         // default
// engine.StrategyPhpsyms    — skip the fallback
// engine.StrategyTreeSitter — force the WASM walker
// engine.StrategyRegex      — force the regex path
```

Tree-sitter PHP stays as a fallback until v1.8.0.

Go works out of the box. The tree-sitter `.wasm` grammars are not bundled —
fetch them on demand:

```go
import "github.com/thanhhaudev/llmreviewkit/grammars"

ctx := context.Background()
if err := grammars.Install(ctx, "python", false); err != nil {
    // Falls back to regex extraction if missing — strictly additive.
}
```

If a grammar isn't installed, the resolver silently falls back to the regex
path for that language. Nothing breaks; you just lose AST-grade accuracy.

## License

[MIT](LICENSE).
