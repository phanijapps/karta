package providers

// Ollama window discovery (intent-analyst-agent S1). Every Ollama model
// reports `<family>.context_length` in POST /api/show → model_info
// (verified live 2026-11-24: glm5_next 1048576, qwen3 40960, llama
// 131072, gptoss 131072 — cloud models too), and NONE reports a separate
// output cap, so one suffix match is the whole discovery. The native API
// lives at the server root while providers are configured with the
// OpenAI-compatible base (…/v1), so the root is derived by stripping
// that suffix and "is this Ollama" is one GET /api/version.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// contextLengthSuffix is the model_info key suffix every family shares.
const contextLengthSuffix = ".context_length"

// windowsBudget bounds the whole per-model discovery pass.
const windowsBudget = 10 * time.Second

// ollamaRoot derives the native API root from an OpenAI-compatible
// base_url ("http://host:11434/v1/" → "http://host:11434").
func ollamaRoot(baseURL string) string {
	return strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
}

// DiscoverWindows fills each model's ContextWindow from Ollama's
// /api/show when the provider is an Ollama server; a non-Ollama provider
// costs exactly one short request and every entry keeps its window
// absent. Per-model failures leave that model's window absent — window
// discovery never fails a refresh.
func (c *Client) DiscoverWindows(ctx context.Context, p Provider, token string, models []Model) []Model {
	if len(models) == 0 {
		return models
	}
	ctx, cancel := context.WithTimeout(ctx, windowsBudget)
	defer cancel()
	root := ollamaRoot(p.BaseURL)
	if !c.isOllama(ctx, root) {
		return models
	}
	for i := range models {
		if ctx.Err() != nil {
			return models // budget spent: the rest stay unknown
		}
		if w, err := c.ollamaContextLength(ctx, root, token, models[i].ID); err == nil && w > 0 {
			models[i].ContextWindow = w
		}
	}
	return models
}

// isOllama reports whether the root answers Ollama's version endpoint.
func (c *Client) isOllama(ctx context.Context, root string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"/api/version", nil)
	if err != nil {
		return false
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// ollamaContextLength reads one model's `*.context_length`.
func (c *Client) ollamaContextLength(ctx context.Context, root, token, model string) (int, error) {
	body, _ := json.Marshal(map[string]string{"model": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, root+"/api/show", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ollama: /api/show %s: %s", model, resp.Status)
	}
	// model_info mixes types (general.architecture is a string); only the
	// numeric *.context_length matters.
	var out struct {
		ModelInfo map[string]json.RawMessage `json:"model_info"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20)).Decode(&out); err != nil {
		return 0, err
	}
	for k, raw := range out.ModelInfo {
		if !strings.HasSuffix(k, contextLengthSuffix) {
			continue
		}
		var v float64
		if json.Unmarshal(raw, &v) == nil && v > 0 {
			return int(v), nil
		}
	}
	return 0, fmt.Errorf("ollama: %s: no *.context_length in model_info", model)
}
