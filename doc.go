// Package llmreviewkit is a Go library for LLM-driven code review.
//
// The high-level entry point is the engine package — construct an
// engine.Engine with a provider.Provider and a workspace root, then call
// Review() per diff. Sub-packages (diff, prompt, schema, render, etc.)
// remain importable for callers who want to assemble a custom pipeline.
//
// See https://github.com/thanhhaudev/llmreviewkit for the README and
// runnable examples.
package llmreviewkit
