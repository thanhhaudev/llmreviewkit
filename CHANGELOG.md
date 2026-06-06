# Changelog

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
