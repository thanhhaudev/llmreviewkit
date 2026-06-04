// Package mock provides a Provider implementation suitable for tests of
// the engine pipeline and any consumer code that needs to exercise the
// review flow without making real HTTP calls.
package mock

import (
	"context"

	"github.com/thanhhaudev/llmreviewkit/provider"
)

// Provider is a test double for provider.Provider. Set ChatFunc / ProbeFunc
// to control responses. Inspect Calls to assert what the engine sent.
type Provider struct {
	NameValue string
	ChatFunc  func(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error)
	ProbeFunc func(ctx context.Context) error
	Calls     []provider.ChatRequest
}

// New returns a Provider whose Chat always returns the supplied response
// content and token counts, recording every request in Calls.
func New(responseContent string, inTok, outTok int) *Provider {
	return &Provider{
		NameValue: "mock",
		ChatFunc: func(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
			return provider.ChatResponse{
				Content:      responseContent,
				InputTokens:  inTok,
				OutputTokens: outTok,
				TotalTokens:  inTok + outTok,
			}, nil
		},
		ProbeFunc: func(ctx context.Context) error { return nil },
	}
}

// Name returns the provider name; defaults to "mock".
func (p *Provider) Name() string {
	if p.NameValue == "" {
		return "mock"
	}
	return p.NameValue
}

// Chat records the request in Calls then delegates to ChatFunc. If
// ChatFunc is nil, returns a zero-value ChatResponse with no error.
func (p *Provider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	p.Calls = append(p.Calls, req)
	if p.ChatFunc == nil {
		return provider.ChatResponse{}, nil
	}
	return p.ChatFunc(ctx, req)
}

// Probe delegates to ProbeFunc. If ProbeFunc is nil, returns nil (probe
// succeeds).
func (p *Provider) Probe(ctx context.Context) error {
	if p.ProbeFunc == nil {
		return nil
	}
	return p.ProbeFunc(ctx)
}
