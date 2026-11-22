package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer serves one canned SSE completion body.
func sseServer(t *testing.T, status int, chunks []string, contentType string) *httptest.Server {
	t.Helper()
	if contentType == "" {
		contentType = "text/event-stream"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		for _, c := range chunks {
			if strings.Contains(contentType, "json") {
				fmt.Fprint(w, c) // raw JSON body (stream-ignoring provider)
			} else {
				fmt.Fprintf(w, "data: %s\n\n", c)
			}
		}
	}))
}

func TestChatCompletionStreamReasoningAndContent(t *testing.T) {
	t.Parallel()
	srv := sseServer(t, http.StatusOK, []string{
		`{"choices":[{"delta":{"reasoning_content":"think "}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"hard"}}]}`,
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`,
		`[DONE]`,
	}, "")
	var kinds []string
	res, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "",
		[]ChatMessage{{Role: "user", Content: "hi"}}, nil,
		func(d ChatStreamDelta) { kinds = append(kinds, string(d.Kind)+":"+d.Text) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if res.Message.Reasoning != "think hard" || res.Message.Content != "Hello world" {
		t.Fatalf("assembled wrong: %+v", res.Message)
	}
	if res.FinishReason != "stop" || res.Usage.PromptTokens != 11 || res.Usage.CompletionTokens != 7 {
		t.Fatalf("finish/usage wrong: %q %+v", res.FinishReason, res.Usage)
	}
	want := []string{"reasoning:think ", "reasoning:hard", "content:Hello", "content: world"}
	if strings.Join(kinds, "|") != strings.Join(want, "|") {
		t.Fatalf("deltas wrong: %v", kinds)
	}
}

func TestChatCompletionStreamReasoningAlias(t *testing.T) {
	t.Parallel()
	srv := sseServer(t, http.StatusOK, []string{
		`{"choices":[{"delta":{"reasoning":"via alias"}}]}`,
		`{"choices":[{"delta":{"content":"answer"}}]}`,
		`[DONE]`,
	}, "")
	res, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Reasoning != "via alias" || res.Message.Content != "answer" {
		t.Fatalf("alias decode wrong: %+v", res.Message)
	}
}

func TestChatCompletionStreamToolCallFragments(t *testing.T) {
	t.Parallel()
	srv := sseServer(t, http.StatusOK, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"clock_now","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	}, "")
	res, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Message.ToolCalls) != 1 {
		t.Fatalf("want 1 assembled tool call, got %+v", res.Message.ToolCalls)
	}
	tc := res.Message.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "clock_now" || tc.Function.Arguments != "{}" {
		t.Fatalf("assembled call wrong: %+v", tc)
	}
	if res.FinishReason != "tool_calls" {
		t.Fatalf("finish wrong: %q", res.FinishReason)
	}
}

func TestChatCompletionStreamJSONFallback(t *testing.T) {
	t.Parallel()
	// Provider ignores stream:true and answers plain JSON.
	srv := sseServer(t, http.StatusOK, []string{
		`{"choices":[{"message":{"role":"assistant","reasoning_content":"thoughts","content":"full reply"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
	}, "application/json")
	var got []string
	res, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil,
		func(d ChatStreamDelta) { got = append(got, string(d.Kind)) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Content != "full reply" || res.Message.Reasoning != "thoughts" {
		t.Fatalf("fallback decode wrong: %+v", res.Message)
	}
	if len(got) != 2 || got[0] != "reasoning" || got[1] != "content" {
		t.Fatalf("synthetic deltas wrong: %v", got)
	}
}

func TestChatCompletionStreamPreStreamFailuresAndNoDone(t *testing.T) {
	t.Parallel()
	// Typed pre-stream failures mirror the sync taxonomy.
	for _, tc := range []struct {
		status int
		reason string
	}{
		{http.StatusUnauthorized, string(ReasonUnauthorized)},
		{http.StatusTooManyRequests, string(ReasonUnreachable)},
		{http.StatusInternalServerError, string(ReasonMalformed)},
	} {
		srv := sseServer(t, tc.status, nil, "")
		_, err := NewChatClient().ChatCompletionStream(context.Background(),
			Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil, nil)
		var f *Failure
		if !errors.As(err, &f) || string(f.Reason) != tc.reason {
			t.Fatalf("status %d: want %s, got %v", tc.status, tc.reason, err)
		}
		srv.Close()
	}
	// EOF with NEITHER [DONE] nor a finish reason is a typed truncation.
	srv := sseServer(t, http.StatusOK, []string{`{"choices":[{"delta":{"content":"partial"}}]}`}, "")
	_, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil, nil)
	var f2 *Failure
	if !errors.As(err, &f2) || f2.Reason != ReasonMalformed {
		t.Fatalf("truncated stream must fail typed, got %v", err)
	}
	// A finish reason without [DONE] is an acceptable completion.
	srv2 := sseServer(t, http.StatusOK, []string{
		`{"choices":[{"delta":{"content":"full"},"finish_reason":"stop"}]}`,
	}, "")
	res, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv2.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil, nil)
	if err != nil || res.Message.Content != "full" {
		t.Fatalf("finish-reason completion must succeed: %v %+v", err, res.Message)
	}
}

func TestChatCompletionNonStreamingReasoning(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok","reasoning_content":"because"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	res, err := NewChatClient().ChatCompletion(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Reasoning != "because" {
		t.Fatalf("sync path dropped reasoning: %+v", res.Message)
	}
}

func TestChatCompletionStreamMultiLineData(t *testing.T) {
	t.Parallel()
	// Two well-formed events; the spec's multi-line data joining is
	// exercised at the assembler level below. Here: normal framing.
	chunk1 := `{"choices":[{"delta":{"content":"line1\n"}}]}`
	chunk2 := `{"choices":[{"delta":{"content":"line2"},"finish_reason":"stop"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", chunk1, chunk2)
	}))
	defer srv.Close()
	res, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Content != "line1\nline2" {
		t.Fatalf("assembly wrong: %q", res.Message.Content)
	}
}

func TestSSEAssemblyJoinsMultiLineData(t *testing.T) {
	t.Parallel()
	// The SSE spec allows one event's payload across multiple data: lines,
	// joined with newlines. The assembler must join and trim correctly.
	var a assembly
	a.data.WriteString("{\"half\":")
	a.data.WriteString("\n")
	a.data.WriteString("1}")
	got := a.take()
	if got != "{\"half\":\n1}" {
		t.Fatalf("join wrong: %q", got)
	}
	if a.take() != "" {
		t.Fatal("take must reset")
	}
}

func TestChatCompletionStreamSplitToolName(t *testing.T) {
	t.Parallel()
	srv := sseServer(t, http.StatusOK, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"clock_","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"now","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	}, "")
	res, err := NewChatClient().ChatCompletionStream(context.Background(),
		Provider{Name: "p", BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Function.Name != "clock_now" {
		t.Fatalf("split name assembly wrong: %+v", res.Message.ToolCalls)
	}
}
