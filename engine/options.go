// Package engine provides the public review pipeline entrypoint for
// llmreviewkit. Construct an Engine via New(cfg) and call Review() per
// diff to be reviewed. Sub-package APIs (diff, prompt, schema, render,
// etc.) remain available for callers who want to assemble a custom
// pipeline.
package engine

import (
	"io"
	"time"

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

	// ExtractionPolicy controls per-language extractor strategy choice.
	// Zero value means DefaultExtractionPolicy is applied.
	ExtractionPolicy ExtractionPolicy
}

// ExtractionStrategy selects which extractor implementation runs for a file.
// Used in ExtractionPolicy below.
type ExtractionStrategy int

const (
	// StrategyAuto chooses the best available implementation per language.
	// Currently: PHP → phpsyms (Go-native), with tree-sitter fallback on error.
	StrategyAuto ExtractionStrategy = iota
	// StrategyTreeSitter forces the tree-sitter (WASM grammar) path.
	StrategyTreeSitter
	// StrategyPhpsyms forces the Phpsyms (Go-native PHP) implementation.
	// Currently only supported for PHP.
	StrategyPhpsyms
	// StrategyRegex forces the regex fallback (last resort).
	StrategyRegex
)

// ExtractionPolicy controls per-language extractor strategy choice.
// Engine.Config gains an ExtractionPolicy field; the zero value applies
// DefaultExtractionPolicy.
type ExtractionPolicy struct {
	// PHP defaults to StrategyAuto (phpsyms preferred).
	PHP ExtractionStrategy

	// ExtractionTimeout caps per-file extraction. Zero means no cap.
	// Default (set by DefaultExtractionPolicy) is 60 seconds.
	ExtractionTimeout time.Duration

	// MaxFileSize skips extraction for files larger than this many bytes.
	// Zero means no cap. Default (set by DefaultExtractionPolicy) is 64 KiB.
	MaxFileSize int
}

// DefaultExtractionPolicy returns the policy applied when Config.ExtractionPolicy
// is the zero value. PHP defaults to StrategyAuto, ExtractionTimeout to 60s,
// and MaxFileSize to 64 KiB.
func DefaultExtractionPolicy() ExtractionPolicy {
	return ExtractionPolicy{
		PHP:               StrategyAuto,
		ExtractionTimeout: 60 * time.Second,
		MaxFileSize:       64 * 1024,
	}
}

// ReviewOptions are per-call parameters.
type ReviewOptions struct {
	// Mode picks the prompt template variant.
	Mode prompt.Mode

	// Focus is an optional free-text focus area to steer the review.
	Focus string

	// Glossary is optional inline glossary content (project-specific terms).
	Glossary string

	// ReviewContext is the body of the workspace's .kizunax/review-context.md.
	// Engine injects it as a "Prior review context" section in the system
	// prompt, ordered ABOVE glossary so behavioral hints take precedence.
	// Empty = section is omitted entirely.
	ReviewContext string

	// ContextPath is the absolute path of the loaded review-context.md.
	// Empty if no file was found. Surfaced for telemetry and stderr warnings;
	// the engine itself does not use it.
	ContextPath string

	// ContextModTime is the file mtime of the loaded review-context.md.
	// Zero if no file was found. Consumers use this to emit staleness
	// warnings without re-statting the file.
	ContextModTime time.Time

	// ScopePaths is the optional list of repo-relative subtree paths to
	// scope the resolver walk to. When empty, the resolver walks the full
	// workspace (pre-v1.5.3 behavior). When set, only files under these
	// subtrees are visited — used by kizunax to avoid whole-monorepo
	// CPU spin when the user passes --paths.
	//
	// Empty-string entries are silently filtered (a `""` element would
	// otherwise resolve to the workspace root and defeat the scope).
	ScopePaths []string

	// Model overrides the provider's default model name.
	Model string

	// Temperature for sampling. 0 = deterministic. Provider may clamp.
	Temperature float64

	// MaxTokens caps the output. Provider may clamp.
	MaxTokens int
}
