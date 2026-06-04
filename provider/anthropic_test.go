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

func TestAnthropic_HeadersAndPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key")
		}
		if r.Header.Get("anthropic-version") != AnthropicVersion {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		w.Write([]byte(`{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`))
	}))
	defer server.Close()

	a := NewAnthropic(AnthropicConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	resp, err := a.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q", resp.Content)
	}
}

func TestAnthropic_ToolUseExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		if _, has := req["tools"]; !has {
			t.Error("expected tools in request")
		}
		tc, ok := req["tool_choice"].(map[string]interface{})
		if !ok || tc["type"] != "tool" {
			t.Errorf("tool_choice = %v", req["tool_choice"])
		}

		// Response with thinking + tool_use blocks (real shape from KizunaX).
		w.Write([]byte(`{
			"id":"m","type":"message","role":"assistant",
			"content":[
				{"type":"thinking","thinking":"reasoning...","signature":"sig"},
				{"type":"tool_use","id":"call_1","name":"submit_review",
				 "input":{"verdict":"approve","summary":"ok","findings":[],"next_steps":[]}}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":50,"output_tokens":20,"total_tokens":70}
		}`))
	}))
	defer server.Close()

	a := NewAnthropic(AnthropicConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	resp, err := a.Chat(context.Background(), ChatRequest{
		Model: "m", Messages: []Message{{Role: "user", Content: "x"}},
		JSONSchema:    `{"type":"object","properties":{"verdict":{"type":"string"}}}`,
		TryJSONSchema: true,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Content should be the JSON of the tool_use.input, NOT thinking text.
	if !strings.Contains(resp.Content, `"verdict":"approve"`) {
		t.Errorf("expected tool_use.input JSON, got: %q", resp.Content)
	}
	if strings.Contains(resp.Content, "reasoning") {
		t.Errorf("thinking block should be ignored, but appeared in content: %q", resp.Content)
	}
}

func TestAnthropic_400FallbackDropsTools(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		calls++

		if calls == 1 {
			if _, has := req["tools"]; !has {
				t.Error("first call should include tools")
			}
			w.WriteHeader(400)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid","message":"tools unsupported"}}`))
			return
		}
		if _, has := req["tools"]; has {
			t.Error("retry should drop tools")
		}
		w.Write([]byte(`{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"{\"verdict\":\"approve\"}"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	a := NewAnthropic(AnthropicConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	resp, err := a.Chat(context.Background(), ChatRequest{
		Model: "m", Messages: []Message{{Role: "user", Content: "x"}},
		JSONSchema: `{"type":"object"}`, TryJSONSchema: true,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (probe + fallback), got %d", calls)
	}
	if !strings.Contains(resp.Content, "approve") {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestAnthropic_KizunaXFlatErrorShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		// KizunaX returns flat {"message": "..."} (no nested error object).
		w.Write([]byte(`{"message":"Model \"x\" is not available. Allowed models: foo, bar"}`))
	}))
	defer server.Close()

	a := NewAnthropic(AnthropicConfig{BaseURL: server.URL, Model: "x", APIKey: "k"})
	_, err := a.Chat(context.Background(), ChatRequest{Model: "x", Messages: []Message{{Role: "user", Content: "y"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	pe := err.(*xerrors.Error)
	if !strings.Contains(pe.Msg, "Allowed models") {
		t.Errorf("expected KizunaX flat message to surface, got: %s", pe.Msg)
	}
}

func TestAnthropic_429Quota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit","message":"slow down"}}`))
	}))
	defer server.Close()

	a := NewAnthropic(AnthropicConfig{BaseURL: server.URL, Model: "m", APIKey: "k"})
	_, err := a.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	pe := err.(*xerrors.Error)
	if pe.Code != "quota" {
		t.Errorf("code = %q, want quota", pe.Code)
	}
}
