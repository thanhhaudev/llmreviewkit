package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thanhhaudev/llmreviewkit/bundlelog"
	"github.com/thanhhaudev/llmreviewkit/diff"
	"github.com/thanhhaudev/llmreviewkit/index"
	"github.com/thanhhaudev/llmreviewkit/prompt"
	"github.com/thanhhaudev/llmreviewkit/provider"
	"github.com/thanhhaudev/llmreviewkit/resolver"
	"github.com/thanhhaudev/llmreviewkit/schema"
	"github.com/thanhhaudev/llmreviewkit/statedir"
	"github.com/thanhhaudev/llmreviewkit/symbols"
)

// Engine is a reusable review pipeline. Construct once, call Review()
// many times with different bundles.
type Engine struct {
	cfg     Config
	stateWS statedir.WorkspaceDir
}

// New constructs an Engine from cfg. Required fields are validated and
// the state directory is created on disk.
func New(cfg Config) (*Engine, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("engine: Config.Provider is required")
	}
	// WorkspaceRoot is required unless callers explicitly skip enrichment
	// by leaving it empty (in which case Review() will skip symbol extraction).
	// SyncIndex also requires a non-empty WorkspaceRoot.
	if cfg.EnrichBudget == 0 {
		if cfg.ExpandCallers || cfg.ExpandTypeDefs || cfg.ExpandTests {
			cfg.EnrichBudget = 96 * 1024
		} else {
			cfg.EnrichBudget = 32 * 1024
		}
	}

	var ws statedir.WorkspaceDir
	if cfg.StateWorkspaceOverride != nil {
		ws = *cfg.StateWorkspaceOverride
	} else if cfg.WorkspaceRoot != "" {
		base := cfg.StateDir
		if base == "" {
			base = filepath.Join(os.TempDir(), "llmreviewkit")
		}
		var resolveErr error
		ws, resolveErr = statedir.Resolve(base, cfg.WorkspaceRoot)
		if resolveErr != nil {
			return nil, fmt.Errorf("engine: resolve state dir: %w", resolveErr)
		}
	}
	// If both WorkspaceRoot and StateWorkspaceOverride are unset, stateWS.Root
	// is empty — enrichment and SyncIndex are both no-ops in that case.
	return &Engine{cfg: cfg, stateWS: ws}, nil
}

// StateWorkspaceRoot returns the absolute path of this engine's state
// directory. Exposed for callers that want to inspect or purge state
// out-of-band (e.g. wipe the index before a fresh build).
func (e *Engine) StateWorkspaceRoot() string { return e.stateWS.Root }

// Result is the output of Review(). Review is the parsed JSON response;
// the other fields surface telemetry.
type Result struct {
	Review          schema.ReviewResult
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	ReferencedFiles []diff.ReferencedFile
	Stats           ResolveStats
}

// ResolveStats is the enrichment telemetry — extracted/filtered/resolved
// counts plus v0.13 index telemetry (hits/misses + resolver path).
type ResolveStats struct {
	ExtractedCount int
	FilteredCount  int
	ResolvedCount  int
	IndexHits      int
	IndexMisses    int
	ResolverPath   string // "v1" | "v2"

	// v1.1.0 additions:

	// ExpansionByStrategy counts attached candidates per strategy
	// ("def_match" + "caller" + "type_def" + "test"). Empty/nil when no
	// expansion flag is set. Telemetry only — does not affect review.
	ExpansionByStrategy map[string]int

	// ExpansionDropped is the count of ranked candidates that fell
	// outside EnrichBudget. Useful for tuning budget per workspace.
	ExpansionDropped int
}

// Review runs the review pipeline on bundle and returns the parsed
// response + telemetry. Errors during enrichment (resolver / index) do
// not fail the call — they downgrade to v1 transparently and are
// reflected in Stats.ResolverPath.
func (e *Engine) Review(ctx context.Context, bundle diff.Bundle, opts ReviewOptions) (*Result, error) {
	schemaJSON, err := schema.LoadSchemaJSON(e.cfg.PromptRoot)
	if err != nil {
		return nil, fmt.Errorf("engine: load schema: %w", err)
	}

	result := &Result{Stats: ResolveStats{ResolverPath: "v1"}}

	// Enrichment — strictly additive: any failure → empty refs, review proceeds.
	// Gated by shouldEnrich (v1.2.0): WorkspaceRoot non-empty AND
	// SkipEnrichment false AND WorkspaceFileCap not exceeded AND there's
	// something to enrich.
	if e.shouldEnrich(bundle) {
		symbols.SetWorkspaceRoot(e.cfg.WorkspaceRoot)
		syms := symbols.ExtractFromBundle(bundle)
		diffPaths := diff.Paths(bundle)

		var rstats resolver.ResolveStats
		var rerr error
		usedV2 := false
		var idx *index.Index

		if e.cfg.UseIndex {
			var idxErr error
			idx, idxErr = e.tryLoadIndex()
			if idxErr == nil && idx.Healthy() {
				v2, v2err := resolver.FindReferencesV2(syms, e.cfg.WorkspaceRoot, idx, diffPaths, 5)
				if v2err == nil {
					rstats = v2.ToV1()
					result.Stats.IndexHits = v2.IndexHits
					result.Stats.IndexMisses = v2.IndexMisses
					result.Stats.ResolverPath = "v2"
					usedV2 = true
				} else if e.cfg.Verbose {
					e.logf("[verbose] resolver v2 failed, falling back to v1: %v\n", v2err)
				}
			} else if idxErr != nil && e.cfg.Verbose {
				e.logf("[verbose] index unavailable for this review (%v); using v1.\n", idxErr)
			}
		}
		if !usedV2 {
			rstats, rerr = resolver.FindReferences(syms, e.cfg.WorkspaceRoot, diffPaths, 5, 4*1024)
		}
		if rerr != nil && e.cfg.Verbose {
			e.logf("[warn] resolver: %v\n", rerr)
		}

		result.Stats.ExtractedCount = rstats.ExtractedCount
		result.Stats.FilteredCount = rstats.FilteredCount
		result.Stats.ResolvedCount = rstats.ResolvedCount

		if e.cfg.Verbose {
			e.logf("[verbose] enrichment: scanner=%d filtered=%d resolved=%d (%d refs) path=%s\n",
				rstats.ExtractedCount, rstats.FilteredCount, rstats.ResolvedCount, len(rstats.Refs), result.Stats.ResolverPath)
		}

		before := len(bundle.Warnings)
		var attachRes diff.AttachResult
		if !e.expansionEnabled() {
			// v1.0.0 path: direct refs only.
			attachRes = diff.AttachReferenced(&bundle, refsToInputs(rstats.Refs), e.cfg.EnrichBudget)
			result.Stats.ExpansionByStrategy = nil
			result.Stats.ExpansionDropped = 0
		} else {
			// v1.1.0 path: run expansion → rank → pack, then attach.
			ar := e.runExpansion(syms, diffPaths, rstats.Refs, idx)
			attachRes = diff.AttachReferenced(&bundle, attachResultToInputs(ar), e.cfg.EnrichBudget)
			result.Stats.ExpansionByStrategy = ar.ByStrategy
			result.Stats.ExpansionDropped = len(ar.Dropped)
		}
		result.ReferencedFiles = bundle.ReferencedFiles
		for _, w := range bundle.Warnings[before:] {
			if strings.HasPrefix(w, "referenced files dropped") && e.cfg.Verbose {
				e.logf("[warn] %s\n", w)
			}
		}

		if e.cfg.BundleLogSink != nil {
			entry := assembleBundleLogEntry(bundle, attachRes, rstats, e.stateWS, result.Stats.IndexHits, result.Stats.IndexMisses, result.Stats.ResolverPath, result.Stats.ExpansionByStrategy, result.Stats.ExpansionDropped)
			_ = bundlelog.AppendTo(e.cfg.BundleLogSink, entry)
		}
	}

	// Build prompt.
	pr, err := prompt.Build(e.cfg.PromptRoot, opts.Mode, bundle, schemaJSON, opts.Focus, opts.Glossary)
	if err != nil {
		return nil, fmt.Errorf("engine: build prompt: %w", err)
	}

	// Chat + parse with one retry on parse error.
	req := provider.ChatRequest{
		System:        pr.System,
		Messages:      []provider.Message{{Role: "user", Content: pr.User}},
		Model:         opts.Model,
		Temperature:   opts.Temperature,
		MaxTokens:     opts.MaxTokens,
		JSONSchema:    schemaJSON,
		TryJSONSchema: true,
	}
	resp, err := e.cfg.Provider.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	review, perr := schema.Parse(resp.Content)
	if perr != nil {
		req.Messages = append(req.Messages,
			provider.Message{Role: "assistant", Content: resp.Content},
			provider.Message{Role: "user", Content: fmt.Sprintf("Your previous response could not be parsed as JSON.\nError: %v\nReturn ONLY a JSON object matching the schema. No prose, no fences.", perr)},
		)
		req.TryJSONSchema = false
		resp2, err2 := e.cfg.Provider.Chat(ctx, req)
		if err2 != nil {
			return nil, err2
		}
		review, perr = schema.Parse(resp2.Content)
		if perr != nil {
			return nil, fmt.Errorf("engine: parse review JSON after retry: %w", perr)
		}
		resp = resp2
	}

	result.Review = review
	result.InputTokens = resp.InputTokens
	result.OutputTokens = resp.OutputTokens
	result.TotalTokens = resp.TotalTokens
	return result, nil
}

// SyncIndex forces a full index rebuild for this engine's workspace.
// Useful for warming the index before a series of Reviews. Safe under
// concurrent calls — uses the same file lock as the lazy build path.
func (e *Engine) SyncIndex() error {
	_, err := index.LoadOrBuild(e.stateWS.Root, e.cfg.WorkspaceRoot)
	return err
}

// tryLoadIndex returns the on-disk index if present and not stale, else
// returns an error so the caller falls back to v1. Staleness check uses
// pkg/index.StaleThreshold (24h in v0.14).
func (e *Engine) tryLoadIndex() (*index.Index, error) {
	idxPath := filepath.Join(e.stateWS.Root, "index", "index.json")
	idx, err := index.LoadJSON(idxPath)
	if err != nil {
		return nil, err
	}
	// Staleness check kept local to engine; background sync (kizunax-
	// specific subprocess) is the wrapper's responsibility.
	return idx, nil
}

// EnrichBudget returns the resolved budget for this engine — either the
// user-supplied Config.EnrichBudget or the auto-bumped default. Exposed
// for tests and for callers introspecting their pipeline.
func (e *Engine) EnrichBudget() int { return e.cfg.EnrichBudget }

// logf writes a verbose log line to stderr. BundleLogSink intentionally
// is NOT used here: that sink receives ONLY jsonl entries from
// bundlelog.AppendTo. Mixing plain-text verbose lines with jsonl breaks
// downstream tooling that pipes the file to jq. Library consumers that
// want to redirect verbose output should plug into stderr or set
// Verbose=false and emit their own diagnostics from telemetry instead.
func (e *Engine) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
