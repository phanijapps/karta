package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ChatMessage is one turn sent to / received from a chat completion.
// ToolCalls is OpenAI-shaped JSON on assistant responses. Reasoning is the
// model's thinking text on assistant responses (decoded from the
// reasoning_content / reasoning spellings; never sent back to providers).
type ChatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Reasoning  string         `json:"-"`
	ToolCalls  []ChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

// UnmarshalJSON decodes the two reasoning spellings (reasoning_content is
// the Z.ai/DeepSeek/Qwen/vLLM de-facto field; reasoning is the OpenAI-native
// spelling) into the single Reasoning field.
func (m *ChatMessage) UnmarshalJSON(b []byte) error {
	type plain ChatMessage
	var raw struct {
		plain
		Reasoning        string  `json:"reasoning"`
		ReasoningContent *string `json:"reasoning_content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = ChatMessage(raw.plain)
	if raw.ReasoningContent != nil {
		m.Reasoning = *raw.ReasoningContent
	} else {
		m.Reasoning = raw.Reasoning
	}
	return nil
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
	Stream   bool          `json:"stream,omitempty"`
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
	return c.ChatCompletionWithBody(ctx, p, model, token, messages, tools, nil)
}

// ChatCompletionWithBody is ChatCompletion with extra top-level
// request-body parameters merged in (provider knobs such as z.ai's
// reasoning_effort). nil extra behaves exactly like ChatCompletion.
func (c *ChatClient) ChatCompletionWithBody(ctx context.Context, p Provider, model, token string, messages []ChatMessage, tools []ChatToolDef, extra map[string]any) (ChatResponse, error) {
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	base, err := json.Marshal(chatRequest{Model: model, Messages: messages, Tools: tools})
	if err != nil {
		return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: err}
	}
	body := base
	if len(extra) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(base, &payload); err != nil {
			return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: err}
		}
		for k, v := range extra {
			payload[k] = v
		}
		if body, err = json.Marshal(payload); err != nil {
			return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: err}
		}
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

// ChatDeltaKind labels which stream a fragment belongs to.
type ChatDeltaKind string

const (
	// DeltaReasoning is a thinking-token fragment.
	DeltaReasoning ChatDeltaKind = "reasoning"
	// DeltaContent is a visible-answer fragment.
	DeltaContent ChatDeltaKind = "content"
)

// ChatStreamDelta is one streamed text fragment.
type ChatStreamDelta struct {
	Kind ChatDeltaKind
	Text string
}

// chatChunkRaw is one SSE data payload of a streamed completion.
type chatChunkRaw struct {
	Choices []struct {
		Delta struct {
			Content          string          `json:"content"`
			Reasoning        string          `json:"reasoning"`
			ReasoningContent string          `json:"reasoning_content"`
			ToolCalls        []chatToolCallD `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *ChatUsage `json:"usage,omitempty"`
}

// chatToolCallD is the fragmented tool-call shape of streamed chunks:
// indexed fragments whose function name arrives once and arguments
// concatenate across fragments.
type chatToolCallD struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ChatCompletionStream posts {base_url}/chat/completions with stream:true.
// onDelta receives reasoning fragments (as the model thinks) and content
// fragments (as it writes), in provider order. The returned response is the
// assembled outcome: full content and reasoning on the message, tool calls
// accumulated from fragments, finish reason, and usage when the provider
// sent it in a final chunk (absent usage is zero, not an error). A provider
// that ignores stream:true and answers JSON degrades to one synthetic
// content (and reasoning, if present) delta — the stream contract holds.
// The typed-failure taxonomy matches ChatCompletion.
func (c *ChatClient) ChatCompletionStream(ctx context.Context, p Provider, model, token string, messages []ChatMessage, tools []ChatToolDef, onDelta func(ChatStreamDelta)) (ChatResponse, error) {
	if onDelta == nil {
		onDelta = func(ChatStreamDelta) {}
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages, Tools: tools, Stream: true})
	if err != nil {
		return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ChatResponse{}, &Failure{Reason: ReasonUnreachable, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	// Pre-stream failures use the same status taxonomy as the sync path.
	switch resp.StatusCode {
	case http.StatusOK:
	default:
		return ChatResponse{}, statusFailure(resp.StatusCode)
	}

	// JSON fallback: a provider that ignores stream:true answers JSON.
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "event-stream") {
		var raw chatResponseRaw
		if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 32<<20)).Decode(&raw); err != nil {
			return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("decode completion: %w", err)}
		}
		if len(raw.Choices) == 0 {
			return ChatResponse{}, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("completion had no choices")}
		}
		msg := raw.Choices[0].Message
		if msg.Reasoning != "" {
			onDelta(ChatStreamDelta{Kind: DeltaReasoning, Text: msg.Reasoning})
		}
		if msg.Content != "" {
			onDelta(ChatStreamDelta{Kind: DeltaContent, Text: msg.Content})
		}
		return ChatResponse{Message: msg, FinishReason: raw.Choices[0].FinishReason, Usage: raw.Usage}, nil
	}

	// SSE walk: data lines accumulate into events (multi-line data joins
	// with newlines per the SSE spec) until the blank-line delimiter; the
	// [DONE] sentinel or EOF ends the stream. Completion state is tracked:
	// a stream that ends with NEITHER the sentinel NOR a finish reason is a
	// TRUNCATION, not a success.
	var out ChatResponse
	out.Message.Role = "assistant"
	type frag struct{ id, name, args string }
	frags := map[int]*frag{}
	order := []int{}
	var event assembly
	sawDone := false
	sawFinish := false
	process := func(payload string) error {
		if payload == "[DONE]" {
			sawDone = true
			return errStopStream
		}
		var chunk chatChunkRaw
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("decode stream chunk: %w", err)}
		}
		if chunk.Usage != nil {
			out.Usage = *chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			return nil // usage-only chunks
		}
		ch := chunk.Choices[0]
		if ch.FinishReason != "" {
			sawFinish = true
			out.FinishReason = ch.FinishReason
		}
		d := ch.Delta
		if rc := firstNonEmptyStr(d.ReasoningContent, d.Reasoning); rc != "" {
			out.Message.Reasoning += rc
			onDelta(ChatStreamDelta{Kind: DeltaReasoning, Text: rc})
		}
		if d.Content != "" {
			out.Message.Content += d.Content
			onDelta(ChatStreamDelta{Kind: DeltaContent, Text: d.Content})
		}
		for _, tc := range d.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			f, ok := frags[idx]
			if !ok {
				f = &frag{}
				frags[idx] = f
				order = append(order, idx)
			}
			if tc.ID != "" {
				f.id = tc.ID
			}
			// Name fragments ACCUMULATE (a provider may split the name).
			f.name += tc.Function.Name
			f.args += tc.Function.Arguments
		}
		return nil
	}
	br := http.MaxBytesReader(nil, resp.Body, 64<<20)
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			if payload := event.take(); payload != "" {
				if err := process(payload); err != nil {
					if errors.Is(err, errStopStream) {
						sawDone = true
						err = nil
					}
					if err != nil {
						return out, err
					}
					break
				}
			}
		case strings.HasPrefix(line, "data:"):
			event.data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			event.data.WriteString("\n")
		default:
			// event:/comment lines are not payloads
		}
		if sawDone {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return out, &Failure{Reason: ReasonUnreachable, Err: fmt.Errorf("read stream: %w", err)}
	}
	if payload := event.take(); payload != "" && !sawDone {
		if err := process(payload); err != nil && !errors.Is(err, errStopStream) {
			return out, err
		}
	}
	if !sawDone && !sawFinish {
		return out, &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("stream truncated: ended without [DONE] or a finish reason")}
	}
	if err := sc.Err(); err != nil {
		return out, &Failure{Reason: ReasonUnreachable, Err: fmt.Errorf("read stream: %w", err)}
	}
	for _, idx := range order {
		f := frags[idx]
		var call ChatToolCall
		call.ID = f.id
		call.Type = "function"
		call.Function.Name = f.name
		call.Function.Arguments = f.args
		out.Message.ToolCalls = append(out.Message.ToolCalls, call)
	}
	return out, nil
}

// assembly accumulates multi-line SSE data payloads.
type assembly struct{ data strings.Builder }

// take returns the joined payload (trailing newline trimmed) and resets.
func (a *assembly) take() string {
	if a.data.Len() == 0 {
		return ""
	}
	payload := strings.TrimSuffix(a.data.String(), "\n")
	a.data.Reset()
	return payload
}

// errStopStream is the internal [DONE] stop signal.
var errStopStream = errors.New("stream done")

// statusFailure maps a non-200 completion status to the typed taxonomy.
func statusFailure(code int) *Failure {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &Failure{Reason: ReasonUnauthorized, Err: fmt.Errorf("provider answered %s", http.StatusText(code))}
	case http.StatusTooManyRequests:
		return &Failure{Reason: ReasonUnreachable, Err: fmt.Errorf("provider rate limited (%s)", http.StatusText(code))}
	default:
		return &Failure{Reason: ReasonMalformed, Err: fmt.Errorf("provider answered %s", http.StatusText(code))}
	}
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
