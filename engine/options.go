// Package engine provides the public review pipeline entrypoint for
// llmreviewkit. Construct an Engine via New(cfg) and call Review() per
// diff to be reviewed. Sub-package APIs (diff, prompt, schema, render,
// etc.) remain available for callers who want to assemble a custom
// pipeline.
package engine

import (
	"io"

	"github.com/thanhhaudev/llmreviewkit/expand"
	"github.com/thanhhaudev/llmreviewkit/prompt"
	"github.com/thanhhaudev/llmreviewkit/provider"
	"github.com/thanhhaudev/llmreviewkit/statedir"
)

// Config configures an Engine. Provider and WorkspaceRoot are required;
// all other fields have sensible defaults.
type Config struct {
	// Required.

	// Provider is the LLM backend. Any implementation of provider.Provider
	// works — bundled openai/anthropic adapters or a user-written impl
	// for Bedrock, Vertex, Ollama, etc.
	Provider provider.Provider

	// WorkspaceRoot is the absolute path to the project being reviewed.
	WorkspaceRoot string

	// Optional.

	// StateDir is the on-disk directory base for index files + telemetry.
	// If empty, defaults to os.TempDir() + "/llmreviewkit". The actual
	// workspace state lives under <StateDir>/<workspace-hash>/.
	// Ignored when StateWorkspaceOverride is set.
	StateDir string

	// StateWorkspaceOverride, if non-nil, bypasses the statedir.Resolve hash
	// computation entirely and uses the provided directory as the workspace
	// state root. Intended for kizunax wrapper callers that have already
	// resolved the state directory via internal/state.Resolve and want to
	// guarantee the engine uses the exact same path without re-hashing.
	StateWorkspaceOverride *statedir.WorkspaceDir

	// PromptRoot is a directory containing custom prompt templates. If
	// empty, embedded defaults are used (pkg/prompt/embedded/*.md).
	PromptRoot string

	// UseIndex enables the v0.13 index-backed resolver. Default false
	// (regex resolver used). When true and no usable index exists,
	// Review() falls back to v1 transparently for the current call.
	UseIndex bool

	// EnrichBudget caps the bytes of referenced-file content attached to
	// the prompt. Default 32*1024 (32 KB).
	EnrichBudget int

	// BundleLogSink, if non-nil, receives one jsonl line per Review() call
	// with the enrichment + resolver telemetry. nil suppresses logging.
	BundleLogSink io.Writer

	// Verbose, if true, emits [verbose] lines to BundleLogSink (or
	// os.Stderr if BundleLogSink is nil).
	Verbose bool

	// === v1.1.0 additions ===

	// ExpandCallers enables Strategy A (caller pulling via index.LookupRefs).
	// Requires UseIndex=true to have any effect. Default false.
	ExpandCallers bool

	// ExpandTypeDefs enables Strategy B (type-def pulling via
	// index.LookupDefs filtered on SymTypeRef). Requires UseIndex=true.
	// Default false.
	ExpandTypeDefs bool

	// ExpandTests enables Strategy D (test-file pulling via filename
	// patterns). Independent of UseIndex — filesystem-only. Default false.
	ExpandTests bool

	// RankerWeights overrides the default scoring formula. nil = use
	// expand.DefaultRankerWeights(). Provided as a pointer so "not set"
	// is unambiguous from "all zeros".
	RankerWeights *expand.RankerWeights

	// === v1.2.0 additions ===

	// SkipEnrichment, when true, disables the entire workspace symbol
	// enrichment pipeline (extraction + resolver + referenced-file
	// attachment). The review proceeds with the bundle's diff content
	// only.
	//
	// Use this on monorepos where the WASM tree-sitter walker would
	// spin the binary at 100% CPU for minutes per call. Empirically the
	// walker scales O(symbols × workspace_files), so a Laravel project
	// with thousands of PHP classes can stall reviews indefinitely.
	//
	// Equivalent to leaving WorkspaceRoot empty in v1.0/v1.1 (the
	// implicit gate inside Review), but explicit, named, and documented.
	// Prefer this field over blanking WorkspaceRoot — the latter also
	// disables index sync and other workspace-aware features that
	// callers may still want.
	//
	// Default false (enrichment enabled when WorkspaceRoot is set).
	SkipEnrichment bool

	// WorkspaceFileCap is the maximum tracked-file count above which the
	// engine auto-skips enrichment and emits a warning via Verbose. 0
	// means no cap (preserves v1.0/v1.1 behavior). The count is obtained
	// once per Review() call via `git ls-files -z`; sub-second on any
	// repo we've measured.
	//
	// Independent of SkipEnrichment: with the cap set, normal-sized
	// workspaces still get full enrichment; only oversize ones are
	// auto-degraded.
	//
	// Recommended values:
	//   - 0     — keep current behavior (default)
	//   - 3000  — empirically safe for Laravel/Django monorepos
	//
	// Default 0.
	WorkspaceFileCap int
}

// ReviewOptions are per-call parameters.
type ReviewOptions struct {
	// Mode picks the prompt template variant.
	Mode prompt.Mode

	// Focus is an optional free-text focus area to steer the review.
	Focus string

	// Glossary is optional inline glossary content (project-specific terms).
	Glossary string

	// Model overrides the provider's default model name.
	Model string

	// Temperature for sampling. 0 = deterministic. Provider may clamp.
	Temperature float64

	// MaxTokens caps the output. Provider may clamp.
	MaxTokens int
}
