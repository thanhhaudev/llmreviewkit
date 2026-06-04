package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	xerrors "github.com/thanhhaudev/llmreviewkit/errors"
)

const toolNameSubmitReview = "submit_review"

type AnthropicAdapter struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func NewAnthropic(cfg AnthropicConfig) *AnthropicAdapter {
	return &AnthropicAdapter{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: HTTPTimeout},
	}
}

func (a *AnthropicAdapter) Name() string { return "anthropic" }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`           // "tool" forces a specific tool
	Name string `json:"name,omitempty"` // tool name when Type == "tool"
}

type anthropicReq struct {
	Model       string                `json:"model"`
	MaxTokens   int                   `json:"max_tokens"`
	System      string                `json:"system,omitempty"`
	Messages    []anthropicMessage    `json:"messages"`
	Temperature float64               `json:"temperature,omitempty"`
	Tools       []anthropicTool       `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice  `json:"tool_choice,omitempty"`
	Stream      bool                  `json:"stream"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`             // "text" | "tool_use" | "thinking"
	Text  string          `json:"text,omitempty"`   // when Type=="text"
	Name  string          `json:"name,omitempty"`   // when Type=="tool_use"
	Input json.RawMessage `json:"input,omitempty"`  // when Type=="tool_use"
}

type anthropicResp struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

type anthropicErrPayload struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	// KizunaX style flat error
	Message string `json:"message"`
}

func (a *AnthropicAdapter) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	messages := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	if maxTokens > MaxOutputTokens {
		maxTokens = MaxOutputTokens
	}

	body := anthropicReq{
		Model:       req.Model,
		MaxTokens:   maxTokens,
		System:      req.System,
		Messages:    messages,
		Temperature: req.Temperature,
		Stream:      false,
	}

	// Default path on KizunaX Anthropic compat: forced tool_use (probe-confirmed
	// to work). This is the most reliable structured output mechanism.
	usingTools := false
	if req.TryJSONSchema && req.JSONSchema != "" {
		var schemaObj map[string]interface{}
		if err := json.Unmarshal([]byte(req.JSONSchema), &schemaObj); err == nil {
			body.Tools = []anthropicTool{{
				Name:        toolNameSubmitReview,
				Description: "Submit your code review result as a structured object.",
				InputSchema: schemaObj,
			}}
			body.ToolChoice = &anthropicToolChoice{Type: "tool", Name: toolNameSubmitReview}
			usingTools = true
		}
	}

	return a.send(ctx, body, usingTools)
}

func (a *AnthropicAdapter) send(ctx context.Context, body anthropicReq, usingTools bool) (ChatResponse, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, xerrors.Internal("json_marshal", "marshal request", err)
	}

	url := a.baseURL + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, xerrors.Internal("http_req", "build request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, xerrors.Provider("network", fmt.Sprintf("network error: %v", err),
			"check connection / base_url", err)
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return ChatResponse{}, xerrors.Provider("read_body", "cannot read response body", "", err)
	}

	// Fallback once: 400 from forced tool_use → retry as plain message.
	if httpResp.StatusCode == 400 && usingTools {
		body.Tools = nil
		body.ToolChoice = nil
		return a.send(ctx, body, false)
	}

	if httpResp.StatusCode != 200 {
		return ChatResponse{}, mapAnthropicHTTPError(httpResp.StatusCode, raw)
	}

	var parsed anthropicResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResponse{}, xerrors.Provider("parse_response",
			fmt.Sprintf("invalid JSON from provider: %v", err), "", err)
	}

	content, err := extractContent(parsed, usingTools)
	if err != nil {
		return ChatResponse{}, err
	}

	total := parsed.Usage.TotalTokens
	if total == 0 {
		total = parsed.Usage.InputTokens + parsed.Usage.OutputTokens
	}

	return ChatResponse{
		Content:      content,
		StopReason:   parsed.StopReason,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		TotalTokens:  total,
		RawResponse:  raw,
	}, nil
}

// extractContent walks content[] to find the relevant block. With forced
// tool_use, response may have a leading "thinking" block plus a "tool_use"
// block — we want the tool_use.Input as JSON text. Without tools, we
// concatenate all text blocks.
func extractContent(r anthropicResp, expectToolUse bool) (string, error) {
	if expectToolUse {
		for _, b := range r.Content {
			if b.Type == "tool_use" && len(b.Input) > 0 {
				return string(b.Input), nil
			}
		}
		// Forced tool_use but no tool_use block — fall through to text.
	}

	var parts []string
	for _, b := range r.Content {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return "", xerrors.Provider("empty_response",
			fmt.Sprintf("provider returned no usable content (stop_reason=%s)", r.StopReason),
			"", nil)
	}
	return strings.Join(parts, "\n"), nil
}

func mapAnthropicHTTPError(status int, raw []byte) error {
	var p anthropicErrPayload
	_ = json.Unmarshal(raw, &p)
	msg := p.Error.Message
	if msg == "" {
		msg = p.Message
	}
	if msg == "" {
		msg = string(raw)
	}

	switch status {
	case 401:
		return xerrors.Provider("auth", fmt.Sprintf("provider 401: %s", msg), "check api_key in config", nil)
	case 403:
		return xerrors.Provider("forbidden", fmt.Sprintf("provider 403: %s", msg), "key may lack access to this model", nil)
	case 404:
		return xerrors.Provider("not_found", fmt.Sprintf("provider 404: %s", msg), "model may be unavailable", nil)
	case 429:
		return xerrors.Provider("quota",
			fmt.Sprintf("provider 429 (quota exceeded): %s", msg),
			"wait until quota resets, or check /usage", nil)
	case 502, 503, 504:
		return xerrors.Provider("upstream_unavailable",
			fmt.Sprintf("provider %d: %s", status, msg),
			"AI provider transient error; retry shortly", nil)
	default:
		return xerrors.Provider("http_error",
			fmt.Sprintf("provider HTTP %d: %s", status, msg), "", nil)
	}
}

// Probe is a tiny health check used by /setup --check.
// MaxTokens is generous because reasoning models (e.g., MiniMax) emit
// hidden reasoning tokens before producing visible content; too small
// a budget produces a content-less response that looks like a failure.
func (a *AnthropicAdapter) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := a.Chat(ctx, ChatRequest{
		Model:     a.model,
		Messages:  []Message{{Role: "user", Content: "Reply with the word OK and nothing else."}},
		MaxTokens: 256,
	})
	return err
}
