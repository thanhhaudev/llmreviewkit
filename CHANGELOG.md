# Changelog

## v1.5.3 — 2026-06-06

### Added
- `resolver.FindReferences` accepts `scopePaths []string` as a new 4th
  positional arg. When non-empty, the workspace walk is limited to the
  union of those subtrees instead of the entire repo. Threaded through
  `engine.ReviewOptions.ScopePaths` so consumers pass `--paths`-style scope
  to the resolver. Empty-string scope entries are silently filtered.
- `glossary.DefaultPaths()` and `context.DefaultPaths()` — exported
  helpers returning the library's default candidate paths. Both exclude
  `.kizunax/` (consumer concern).

### Changed
- `engine.ReviewOptions` gains `ScopePaths []string` (was originally
  drafted as `Paths` but renamed pre-tag for clarity vs `bundle.Paths` /
  `target.Paths`).
- `glossary.Load` and `context.Load` signatures change from
  `Load(workspaceRoot)` to `Load(workspaceRoot, candidatePaths []string)`.
  Passing nil uses `DefaultPaths()`. Breaking for direct callers; the
  fix in kizunax v0.28.0 is to pass `[]string{".kizunax/glossary.md",
  "docs/glossary.md", "GLOSSARY.md"}` (and the review-context analogue)
  explicitly at the call site.
- `engine.shouldEnrich` now bypasses `WorkspaceFileCap` when scope is
  set. Rationale: the cap exists to prevent whole-workspace CPU spins;
  a scoped walk does not hit that hot path. Without this, Oneplat
  (6062 files, cap 3000) would never benefit from `--paths`-scoped
  reviews.

### Notes
- All internal call sites updated; tests pinned via the `-race`
  detector locally before tag.

## v1.5.2 — 2026-06-06

### Fixed
- Data races on `symbols.currentPolicy` and `symbols.extractObserver` globals
  flagged by `go test -race` (pre-existing since v1.5.0). `ExtractWithPolicy`
  spawns a worker goroutine for timeout handling; concurrent
  `SetExtractionPolicy` / `SetExtractObserver` calls now go through
  `sync.RWMutex` (single `policyMu` guards both globals). Snapshot helper
  `snapshotPolicy()` returns a value copy for safe read access.
- Affects production `engine.New()` callers that update policy after
  spawning extraction work — race was real, not test-only.

## v1.5.1 — 2026-06-06

### Added
- `context` package — loads `.kizunax/review-context.md` (priority paths:
  `.kizunax/`, `docs/`, root) with 8 KiB UTF-8-safe truncation. Exposes
  ModTime so consumers can emit staleness warnings without re-statting.
- `engine.ReviewOptions` gains `ReviewContext`, `ContextPath`, `ContextModTime`
  fields.
- `prompt.Build` accepts a `reviewContext` parameter and prepends a
  "Prior review context" section to the system prompt, ordered ABOVE the
  glossary section so behavioral hints take precedence.

### Notes
- Consumers (kizunax v0.27.0+) load the file and pass content via the new
  options. The engine itself does not read disk.
- File format is free-form markdown; engine treats verbatim, no parsing.

## v1.5.0 — 2026-06-06

### Added
- `engine.ExtractionPolicy` — per-language extractor strategy choice
  (`PHP: StrategyAuto|TreeSitter|Phpsyms|Regex`), per-file extraction
  timeout (default 60s), and max file size cap (default 64 KiB).
- `symbols.DispatchPHP` — strategy-routed PHP extraction. Auto path prefers
  the Go-native phpsyms extractor and falls back to tree-sitter only when
  phpsyms returns no symbols.
- `symbols.ExtractWithPolicy` — opt-in wrapper that applies the file-size
  and timeout gates from the current ExtractionPolicy.
- `symbols.ExtractCache` — persistent on-disk cache keyed by
  sha256(path,content,strategy). Layout `<root>/<sha256[:2]>/<sha256>.json`.
- `symbols.ExtractEvent` + `SetExtractObserver` — per-extraction telemetry
  hook. Engine.ResolveStats gains `ExtractorPath map[string]int` for
  external observers to populate.
- `symbols.ExtractPHPViaPhpsyms` — adapter to the new `phpsyms` v0.2.1
  Go-native PHP symbol extractor (https://github.com/thanhhaudev/phpsyms,
  sibling repo, stdlib-only, ~5000 files/sec).
- Parity test (`TestParity_PhpsymsVsTreeSitter`) on the 51-file
  laravel-framework corpus pins per-Kind Jaccard overlap floors:

  | Kind       | Measured | Floor | Notes |
  |------------|----------|-------|-------|
  | SymDef     | 95%      | 90%   | Near-identical AST coverage |
  | SymCall    | 78%      | 70%   | phpsyms call detection is narrower |
  | SymImport  | 6%       | 5%    | Design divergence: phpsyms emits FQN, ts emits short alias |
  | SymTypeRef | 21%      | 15%   | phpsyms includes PHPDoc @param/@return; ts uses only native hints |

  The import and typeref divergences are intentional design differences, not
  regressions. The floors detect total breakage only.

### Changed
- Tree-sitter PHP path is preserved as a fallback and is the default for
  `StrategyTreeSitter`. Removal scheduled for v1.8.0.

### Notes
- Phpsyms is currently vendored via `replace github.com/thanhhaudev/phpsyms => ../phpsyms`
  since it isn't yet published to a public remote. Downstream consumers
  (e.g. kizunax-plugin-cc v0.25.0) need a matching `replace` directive
  during local development.

(no prior entries; v1.5.0 is the first CHANGELOG-tracked release)
