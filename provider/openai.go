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

type OpenAIAdapter struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func NewOpenAI(cfg OpenAIConfig) *OpenAIAdapter {
	return &OpenAIAdapter{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: HTTPTimeout},
	}
}

func (a *OpenAIAdapter) Name() string { return "openai" }

type openaiReq struct {
	Model          string             `json:"model"`
	Messages       []Message          `json:"messages"`
	Temperature    float64            `json:"temperature,omitempty"`
	MaxTokens      int                `json:"max_tokens,omitempty"`
	Stream         bool               `json:"stream"`
	ResponseFormat *openaiRespFormat  `json:"response_format,omitempty"`
}

type openaiRespFormat struct {
	Type       string                 `json:"type"`
	JSONSchema map[string]interface{} `json:"json_schema,omitempty"`
}

type openaiResp struct {
	ID      string `json:"id"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		TokensConsumed   int `json:"tokens_consumed"` // MiniMax/KizunaX-specific (includes reasoning overhead)
	} `json:"usage"`
}

type openaiErrPayload struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"` // KizunaX returns flat {message: "..."} sometimes
}

func (a *OpenAIAdapter) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	messages := make([]Message, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, Message{Role: "system", Content: req.System})
	}
	messages = append(messages, req.Messages...)

	body := openaiReq{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
	}

	if req.TryJSONSchema && req.JSONSchema != "" {
		var schemaObj map[string]interface{}
		if err := json.Unmarshal([]byte(req.JSONSchema), &schemaObj); err == nil {
			body.ResponseFormat = &openaiRespFormat{
				Type: "json_schema",
				JSONSchema: map[string]interface{}{
					"name":   "review_output",
					"strict": true,
					"schema": schemaObj,
				},
			}
		}
	}

	return a.send(ctx, body, req.TryJSONSchema)
}

func (a *OpenAIAdapter) send(ctx context.Context, body openaiReq, isStructuredAttempt bool) (ChatResponse, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, xerrors.Internal("json_marshal", "marshal request", err)
	}

	url := a.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, xerrors.Internal("http_req", "build request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, xerrors.Provider("network", fmt.Sprintf("network error: %v", err), "check connection / base_url", err)
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return ChatResponse{}, xerrors.Provider("read_body", "cannot read response body", "", err)
	}

	if httpResp.StatusCode == 400 && isStructuredAttempt {
		// Retry once without response_format (fallback to prompt-only).
		body.ResponseFormat = nil
		return a.send(ctx, body, false)
	}

	if httpResp.StatusCode != 200 {
		return ChatResponse{}, mapHTTPError(httpResp.StatusCode, raw)
	}

	var parsed openaiResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResponse{}, xerrors.Provider("parse_response",
			fmt.Sprintf("invalid JSON from provider: %v", err), "", err)
	}

	if len(parsed.Choices) == 0 {
		return ChatResponse{}, xerrors.Provider("empty_response", "provider returned no choices", "", nil)
	}

	choice := parsed.Choices[0]
	total := parsed.Usage.TotalTokens
	if total == 0 {
		// Fallback for MiniMax/KizunaX which may omit total_tokens but
		// send tokens_consumed (includes reasoning overhead). If neither
		// field is present, derive from prompt+completion.
		if parsed.Usage.TokensConsumed > 0 {
			total = parsed.Usage.TokensConsumed
		} else {
			total = parsed.Usage.PromptTokens + parsed.Usage.CompletionTokens
		}
	}
	return ChatResponse{
		Content:      choice.Message.Content,
		StopReason:   choice.FinishReason,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		TotalTokens:  total,
		RawResponse:  raw,
	}, nil
}

func mapHTTPError(status int, raw []byte) error {
	var p openaiErrPayload
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
		return xerrors.Provider("quota", fmt.Sprintf("provider 429 (quota exceeded): %s", msg),
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
// hidden reasoning tokens before producing visible content.
func (a *OpenAIAdapter) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := a.Chat(ctx, ChatRequest{
		Model:     a.model,
		Messages:  []Message{{Role: "user", Content: "Reply with the word OK and nothing else."}},
		MaxTokens: 256,
	})
	return err
}
