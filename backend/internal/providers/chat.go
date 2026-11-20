package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ChatMessage is one turn sent to / received from a chat completion.
// ToolCalls is OpenAI-shaped JSON on assistant responses.
type ChatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []ChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

// ChatToolCall is one OpenAI-style tool call on an assistant response.
type ChatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ChatToolDef describes one offered tool.
type ChatToolDef struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"` // JSON-schema object
	} `json:"function"`
}

// ChatUsage is REQUEST usage from the response (not content size).
type ChatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// ChatResponse is one completion outcome.
type ChatResponse struct {
	Message      ChatMessage
	FinishReason string
	Usage        ChatUsage
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []ChatToolDef `json:"tools,omitempty"`
}

type chatResponseRaw struct {
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage ChatUsage `json:"usage"`
}

// chatTimeout is the generous, fixed bound for completions (the eino
// spike proved local 32B models exceed 60s; discovery keeps its own
// short 10s client). Callers cancel earlier via context.
const chatTimeout = 180 * time.Second

// ChatClient performs OpenAI-compatible chat completions with its own
// generous deadline, separate from the short-bound discovery client.
type ChatClient struct{ HTTP *http.Client }

// NewChatClient returns the chat completion client.
func NewChatClient() *ChatClient { return &ChatClient{HTTP: &http.Client{Timeout: chatTimeout}} }

// ChatCompletion posts {base_url}/chat/completions. token is the
// caller-resolved credential (empty sends none). A nil tools slice
// offers none (the final round).
func (c *ChatClient) ChatCompletion(ctx context.Context, p Provider, model, token string, messages []ChatMessage, tools []ChatToolDef) (ChatResponse, error) {
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages, Tools: tools})
	if err != nil {
		return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ChatResponse{}, &Failure{Reason: ReasonUnreachable, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var raw chatResponseRaw
		dec := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 32<<20))
		if err := dec.Decode(&raw); err != nil {
			return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("decode completion: %w", err)}
		}
		if len(raw.Choices) == 0 {
			return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("completion had no choices")}
		}
		return ChatResponse{Message: raw.Choices[0].Message, FinishReason: raw.Choices[0].FinishReason, Usage: raw.Usage}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ChatResponse{}, &Failure{Reason: ReasonUnauthorized, Err: fmt.Errorf("provider answered %s", resp.Status)}
	case http.StatusTooManyRequests:
		return ChatResponse{}, &Failure{Reason: ReasonUnreachable, Err: fmt.Errorf("provider rate limited (%s)", resp.Status)}
	default:
		return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("provider answered %s", resp.Status)}
	}
}
