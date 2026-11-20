package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newFakeChat(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *ChatClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, NewChatClient()
}

func TestChatCompletion(t *testing.T) {
	t.Run("plain completion with usage", func(t *testing.T) {
		srv, client := newFakeChat(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/chat/completions" {
				t.Errorf("path %q", r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
		})
		res, err := client.ChatCompletion(context.Background(), Provider{BaseURL: srv.URL + "/v1"}, "m", "", []ChatMessage{{Role: "user", Content: "q"}}, nil)
		if err != nil || res.Message.Content != "hi" || res.Usage.PromptTokens != 10 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})

	t.Run("tool-call response parses", func(t *testing.T) {
		srv, client := newFakeChat(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"clock.now","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)
		})
		res, err := client.ChatCompletion(context.Background(), Provider{BaseURL: srv.URL}, "m", "", []ChatMessage{{Role: "user", Content: "time?"}}, []ChatToolDef{{Type: "function"}})
		if err != nil || len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Function.Name != "clock.now" || res.FinishReason != "tool_calls" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})

	t.Run("token sent as bearer", func(t *testing.T) {
		srv, client := newFakeChat(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer sekret" {
				t.Error("missing bearer")
			}
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
		})
		if _, err := client.ChatCompletion(context.Background(), Provider{BaseURL: srv.URL}, "m", "sekret", []ChatMessage{{Role: "user"}}, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("typed failures", func(t *testing.T) {
		srv, client := newFakeChat(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
		_, err := client.ChatCompletion(context.Background(), Provider{BaseURL: srv.URL}, "m", "", nil, nil)
		if ReasonOf(err) != ReasonUnauthorized {
			t.Fatalf("want unauthorized, got %v", err)
		}
		srv2, client2 := newFakeChat(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `<html>`) })
		_, err = client2.ChatCompletion(context.Background(), Provider{BaseURL: srv2.URL}, "m", "", nil, nil)
		if ReasonOf(err) != ReasonMalformed {
			t.Fatalf("want malformed, got %v", err)
		}
	})

	t.Run("chat deadline is generous and caller-cancelable", func(t *testing.T) {
		if NewChatClient().HTTP.Timeout < 170*time.Second {
			t.Fatal("chat deadline must be generous (spike: local 32B models exceed 60s)")
		}
		srv, _ := newFakeChat(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := NewChatClient().ChatCompletion(ctx, Provider{BaseURL: srv.URL}, "m", "", nil, nil)
		if err == nil {
			t.Fatal("caller cancellation must abort")
		}
	})
}
