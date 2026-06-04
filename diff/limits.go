package diff

// MaxDiffBytes caps the total size of a diff bundle that the resolver
// will process. Diffs larger than this are truncated with a warning;
// the cap exists because LLM context windows + per-request token costs
// scale with prompt size, and v0.13.0 measurement showed >256 KiB
// diffs are almost always too noisy for useful review.
const MaxDiffBytes = 256 * 1024
