package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNoListModels is returned when the upstream endpoint does not expose
// /models (e.g. some Anthropic-compat servers). Callers should fall back to
// a hardcoded short list.
var ErrNoListModels = errors.New("upstream does not expose /models")

// ListModels queries baseURL/models with apiKey and returns the model IDs.
// Works for both OpenAI-compat and Anthropic-compat KizunaX endpoints — both
// return the OpenAI shape {"data":[{"id":"..."}]}.
//
// Returns ErrNoListModels if the upstream responds 404. Other 4xx/5xx
// surface as wrapped errors.
func ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoListModels
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list-models HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("list-models parse: %w", err)
	}
	out := make([]string, 0, len(envelope.Data))
	for _, m := range envelope.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}
