package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	xerrors "github.com/thanhhaudev/llmreviewkit/errors"
)

func newOpenAIServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *AnthropicAdapter) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	_ = server
	return server, nil // adapter built in caller (need correct provider)
}

func TestOpenAI_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing Bearer auth: %q", auth)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		if req["model"] != "test-model" {
			t.Errorf("model = %v", req["model"])
		}

		resp := map[string]interface{}{
			"id":     "chat-1",
			"object": "chat.completion",
			"choices": []map[string]interface{}{{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": "Hello!"},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewOpenAI(OpenAIConfig{
		BaseURL: server.URL, Model: "test-model", APIKey: "kx_test",
	})
	resp, err := a.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d", resp.TotalTokens)
	}
}

func TestOpenAI_FallbackTotalFromTokensConsumed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MiniMax shape: no total_tokens, but has tokens_consumed
		w.Write([]byte(`{
			"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":40,"completion_tokens":10,"tokens_consumed":99}
		}`))
	}))
	defer server.Close()

	a := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	resp, err := a.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.TotalTokens != 99 {
		t.Errorf("TotalTokens = %d, want 99 (from tokens_consumed)", resp.TotalTokens)
	}
}

func TestOpenAI_FallbackTotalFromSum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No total, no tokens_consumed → derive from sum
		w.Write([]byte(`{
			"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":40,"completion_tokens":10}
		}`))
	}))
	defer server.Close()

	a := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	resp, _ := a.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if resp.TotalTokens != 50 {
		t.Errorf("TotalTokens = %d, want 50 (sum)", resp.TotalTokens)
	}
}

func TestOpenAI_401Auth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"type":"auth","message":"invalid key"}}`))
	}))
	defer server.Close()

	a := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m", APIKey: "bad"})
	_, err := a.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	pe, ok := err.(*xerrors.Error)
	if !ok {
		t.Fatalf("expected *xerrors.Error, got %T", err)
	}
	if pe.Kind != xerrors.KindProvider || pe.Code != "auth" {
		t.Errorf("kind=%d code=%q, want Provider/auth", pe.Kind, pe.Code)
	}
}

func TestOpenAI_429Quota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"message":"quota exceeded"}`)) // KizunaX flat shape
	}))
	defer server.Close()

	a := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	_, err := a.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	pe := err.(*xerrors.Error)
	if pe.Code != "quota" {
		t.Errorf("code = %q, want quota", pe.Code)
	}
	if !strings.Contains(pe.Msg, "quota exceeded") {
		t.Errorf("msg should include provider message: %s", pe.Msg)
	}
}

func TestOpenAI_FallbackOn400WithJSONSchema(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		calls++

		if calls == 1 {
			// First call should have response_format. Reject it.
			if _, has := req["response_format"]; !has {
				t.Errorf("expected response_format on first call")
			}
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"type":"invalid_request","message":"response_format unsupported"}}`))
			return
		}
		// Second call: no response_format, succeed.
		if _, has := req["response_format"]; has {
			t.Errorf("retry should NOT include response_format")
		}
		w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"{\"verdict\":\"approve\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	a := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	resp, err := a.Chat(context.Background(), ChatRequest{
		Model:         "m",
		Messages:      []Message{{Role: "user", Content: "x"}},
		JSONSchema:    `{"type":"object"}`,
		TryJSONSchema: true,
	})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (probe + fallback), got %d", calls)
	}
	if !strings.Contains(resp.Content, "approve") {
		t.Errorf("content = %q", resp.Content)
	}
}
