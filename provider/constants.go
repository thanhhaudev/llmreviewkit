package provider

import "time"

// HTTPTimeout is the default per-request HTTP timeout for OpenAI/Anthropic
// adapter calls. Set generously because LLM responses for code review can
// take 60+ seconds.
const HTTPTimeout = 120 * time.Second

// DefaultMaxTokens is the per-request output token cap when ReviewOptions.
// MaxTokens is zero. Picked to fit a typical multi-finding review.
const DefaultMaxTokens = 8192

// MaxOutputTokens caps the highest value clients can request. Provider
// adapters clamp at this value to avoid surprise billing.
const MaxOutputTokens = 16384

// AnthropicVersion is the API version header value sent by the Anthropic
// adapter. Pinned because Anthropic's API requires it.
const AnthropicVersion = "2023-06-01"
