package provider

import "context"

type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// Probe sends a tiny request to verify connectivity + auth.
	Probe(ctx context.Context) error
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	System         string
	Messages       []Message
	Model          string
	Temperature    float64
	MaxTokens      int
	JSONSchema     string // raw schema JSON, used by adapters that support structured output
	TryJSONSchema  bool   // if true, adapter attempts response_format: json_schema strict
}

type ChatResponse struct {
	Content      string
	StopReason   string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	RawResponse  []byte
}
