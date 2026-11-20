package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// newChatDaemon wires real chat handlers over an in-memory store and the
// fake completer — hermetic end-to-end coverage of the HTTP surface.
func newChatDaemon(t *testing.T, scripted []providers.ChatResponse, errs []error) (string, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fc := &fakeChatCompleter{responses: scripted, errs: errs}
	lookup := func(string) (providers.Provider, error) {
		return providers.Provider{BaseURL: "http://fake.invalid/v1"}, nil
	}
	resolve := func(context.Context, providers.Provider) (string, error) { return "", nil }
	svc := chat.New(db, lookup, resolve, fc, tools.BuiltinProvider{}, nil, nil)
	daemon := startTestServer(New(nil, nil, nil, svc, nil, nil, nil, "test").Routes())
	t.Cleanup(daemon.Close)
	return daemon.URL, db
}

type fakeChatCompleter struct {
	responses []providers.ChatResponse
	errs      []error
	calls     int
}

func (f *fakeChatCompleter) ChatCompletion(_ context.Context, _ providers.Provider, _, _ string, _ []providers.ChatMessage, _ []providers.ChatToolDef) (providers.ChatResponse, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return providers.ChatResponse{}, f.errs[i]
	}
	if i >= len(f.responses) {
		return providers.ChatResponse{Message: providers.ChatMessage{Role: "assistant", Content: "done"}}, nil
	}
	return f.responses[i], nil
}

func startTestServer(handler http.Handler) *httptest.Server {
	srv := httptest.NewServer(handler)
	return srv
}

func TestChatEndpoints(t *testing.T) {
	toolResp := providers.ChatResponse{FinishReason: "tool_calls"}
	toolResp.Message.Role = "assistant"
	toolResp.Message.ToolCalls = []providers.ChatToolCall{{ID: "c1", Type: "function"}}
	toolResp.Message.ToolCalls[0].Function.Name = "clock.now"
	toolResp.Message.ToolCalls[0].Function.Arguments = "{}"
	final := providers.ChatResponse{Message: providers.ChatMessage{Role: "assistant", Content: "the time is now"}, FinishReason: "stop"}

	t.Run("send with a tool round returns all rows", func(t *testing.T) {
		url, _ := newChatDaemon(t, []providers.ChatResponse{toolResp, final}, nil)
		code, body := mutate(t, "POST", url+"/api/chat/messages", map[string]any{"text": "what time?", "model": "ollama/qwen"})
		if code != http.StatusOK {
			t.Fatalf("code %d body %v", code, body)
		}
		msgs := body["messages"].([]any)
		if len(msgs) != 4 {
			t.Fatalf("want 4 rows (user,intent,tool,final): %v", body)
		}
		first := msgs[0].(map[string]any)
		if first["role"] != "user" || first["id"] == "" {
			t.Fatalf("user row: %v", first)
		}
		toolRow := msgs[2].(map[string]any)
		if toolRow["role"] != "tool" || toolRow["tool_name"] != "clock.now" {
			t.Fatalf("tool row: %v", toolRow)
		}
		last := msgs[3].(map[string]any)
		if last["provider"] != "ollama" || last["model"] != "qwen" {
			t.Fatalf("provenance: %v", last)
		}
		if body["end_cursor"].(float64) == 0 {
			t.Fatal("end cursor must be set")
		}
	})

	t.Run("model_required without a model", func(t *testing.T) {
		url, _ := newChatDaemon(t, nil, nil)
		code, body := mutate(t, "POST", url+"/api/chat/messages", map[string]any{"text": "hi"})
		if code != http.StatusBadRequest || body["code"] != "model_required" {
			t.Fatalf("code %d %v", code, body)
		}
	})

	t.Run("provider failure returns the persisted user row", func(t *testing.T) {
		url, db := newChatDaemon(t, nil, []error{&providers.Failure{Reason: providers.ReasonUnreachable, Err: fmt.Errorf("dial refused")}})
		code, body := mutate(t, "POST", url+"/api/chat/messages", map[string]any{"text": "hello", "model": "ollama/m"})
		if code != http.StatusBadGateway {
			t.Fatalf("code %d", code)
		}
		msgs := body["messages"].([]any)
		if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" {
			t.Fatalf("failure must return the user row: %v", body)
		}
		if body["error"].(map[string]any)["reason"] != "unreachable" {
			t.Fatalf("typed error: %v", body["error"])
		}
		rows, _ := db.Replay(chat.SessionID, 0, 10)
		if len(rows) != 1 {
			t.Fatalf("transcript: %+v", rows)
		}
	})

	t.Run("sync pages with page cursors", func(t *testing.T) {
		plain := providers.ChatResponse{Message: providers.ChatMessage{Role: "assistant", Content: "r"}, FinishReason: "stop"}
		url, _ := newChatDaemon(t, []providers.ChatResponse{plain, plain, plain}, nil)
		for i := 0; i < 3; i++ {
			code, _ := mutate(t, "POST", url+"/api/chat/messages", map[string]any{"text": fmt.Sprintf("q%d", i), "model": "p/m"})
			if code != http.StatusOK {
				t.Fatalf("send %d failed: %d", i, code)
			}
		}
		// Page size 3 over 6 rows: two pages, second not truncated.
		code, page1 := getJSON(t, url+"/api/chat/messages?limit=3")
		if code != 200 || !page1["truncated"].(bool) || page1["next_cursor"].(float64) != 3 {
			t.Fatalf("page1: %v", page1)
		}
		cursor := int(page1["next_cursor"].(float64))
		code, page2 := getJSON(t, fmt.Sprintf("%s/api/chat/messages?limit=3&after_seq=%d", url, cursor))
		if code != 200 || page2["truncated"].(bool) || len(page2["messages"].([]any)) != 3 {
			t.Fatalf("page2: %v", page2)
		}
		all := append(page1["messages"].([]any), page2["messages"].([]any)...)
		if len(all) != 6 {
			t.Fatalf("paged rows: %d", len(all))
		}
	})
}
