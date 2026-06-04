package provider

// OpenAIConfig configures NewOpenAI. Library consumers fill these
// explicitly; the kizunax CLI translates its multi-provider config
// (internal/config) into this leaner shape.
type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

// AnthropicConfig configures NewAnthropic. Same shape as OpenAIConfig
// for symmetry; the two are kept separate so future per-provider fields
// don't bleed across.
type AnthropicConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}
