package providers

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

// FailureReason identifies which provider-side failure occurred. Callers
// map reasons to contract statuses without inspecting error strings.
type FailureReason string

const (
	// ReasonUnreachable — the provider endpoint could not be reached or did
	// not answer within the bound.
	ReasonUnreachable FailureReason = "unreachable"
	// ReasonUnauthorized — the provider rejected the request's credentials.
	ReasonUnauthorized FailureReason = "unauthorized"
	// ReasonMalformed — the provider answered, but the response was not a
	// usable OpenAI-compatible model list.
	ReasonMalformed FailureReason = "malformed"
)

// Failure is a typed discovery failure carrying its reason and cause.
// Status is the provider's HTTP status when it answered (0 = no
// answer), so callers can branch on a 4xx without parsing the message.
type Failure struct {
	Reason FailureReason
	Err    error
	Status int
}

func (f *Failure) Error() string { return fmt.Sprintf("providers: %s: %v", f.Reason, f.Err) }
func (f *Failure) Unwrap() error { return f.Err }

// Model is one discovered model identifier. ContextWindow is the
// model's context window in tokens when the provider reports it
// (Ollama /api/show), else 0 = unknown (the limits table or the
// fallback apply).
type Model struct {
	ID            string `json:"id"`
	ContextWindow int    `json:"context_window,omitempty"`
}

// openAIModelList is the OpenAI-compatible list-models response shape.
// Unknown fields are ignored so provider variations do not break parsing.
type openAIModelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Client performs OpenAI-compatible discovery against configured
// providers. The zero value is unusable; use NewClient.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a discovery client with a bounded timeout and no
// retries.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// ListModels calls the provider's model-list endpoint
// ({base_url}/models) and returns the discovered models. The bearer token
// is the caller-resolved credential (empty sends none — see
// ResolveCredential); the client itself never touches secrets or the
// environment.
func (c *Client) ListModels(ctx context.Context, p Provider, token string) ([]Model, error) {
	// base_url is the provider's API root used verbatim; tolerate a
	// trailing slash so "http://host/v1/" and "http://host/v1" behave alike.
	url := strings.TrimRight(p.BaseURL, "/") + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Wrap but never include the URL with an embedded credential —
		// only the Authorization header carries one, and it is not in err.
		return nil, &Failure{Reason: ReasonUnreachable, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return decodeModels(resp)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, &Failure{Reason: ReasonUnauthorized, Status: resp.StatusCode, Err: fmt.Errorf("provider answered %s", resp.Status)}
	default:
		return nil, &Failure{Reason: ReasonMalformed, Status: resp.StatusCode, Err: fmt.Errorf("provider answered %s", resp.Status)}
	}
}

// maxModelListBytes bounds how much of a provider's model-list body is
// read (a misbehaving provider cannot stream unbounded bytes into the
// daemon); anything past the cap fails to parse as malformed.
const maxModelListBytes = 8 << 20 // 8 MiB

// maxModels mirrors the contract's ModelList.models maxItems.
const maxModels = 1024

// decodeModels parses a 200 body as an OpenAI-compatible model list.
func decodeModels(resp *http.Response) ([]Model, error) {
	var parsed openAIModelList
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxModelListBytes))
	if err := dec.Decode(&parsed); err != nil {
		return nil, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("decode model list: %w", err)}
	}
	// An empty list is a valid answer (e.g. a fresh runtime with no
	// models installed) — not a failure.
	models := make([]Model, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID == "" {
			return nil, &Failure{Reason: ReasonMalformed, Err: errors.New("model entry without id")}
		}
		models = append(models, Model{ID: m.ID})
	}
	// Cap at the contract's maxItems; provider lists nowhere near this,
	// but the daemon must never emit more than the contract allows.
	if len(models) > maxModels {
		models = models[:maxModels]
	}
	return models, nil
}
