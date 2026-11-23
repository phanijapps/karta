package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// fakeCompleter scripts completion responses.
type fakeCompleter struct {
	mu           sync.Mutex
	responses    []providers.ChatResponse
	errs         []error
	calls        int
	seenTools    []int // tools offered per call
	lastMessages []providers.ChatMessage
}

func (f *fakeCompleter) ChatCompletion(_ context.Context, _ providers.Provider, _, _ string, messages []providers.ChatMessage, defs []providers.ChatToolDef) (providers.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenTools = append(f.seenTools, len(defs))
	f.lastMessages = messages
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

func toolCallResp(id, name, args string) providers.ChatResponse {
	r := providers.ChatResponse{FinishReason: "tool_calls"}
	r.Message.Role = "assistant"
	r.Message.ToolCalls = []providers.ChatToolCall{{ID: id, Type: "function"}}
	r.Message.ToolCalls[0].Function.Name = name
	r.Message.ToolCalls[0].Function.Arguments = args
	return r
}

func plainResp(content string) providers.ChatResponse {
	return providers.ChatResponse{Message: providers.ChatMessage{Role: "assistant", Content: content}, FinishReason: "stop"}
}

var errProvider = &providers.Failure{Reason: providers.ReasonUnreachable, Err: fmt.Errorf("dial refused")}

func newService(t *testing.T, completer Completer) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bus := eventbus.New()
	lookup := func(name string) (providers.Provider, error) {
		return providers.Provider{Name: name, BaseURL: "http://fake.invalid/v1"}, nil
	}
	resolve := func(ctx context.Context, p providers.Provider) (string, error) { return "", nil }
	return New(st, lookup, resolve, completer, tools.BuiltinProvider{}, bus, nil), st
}

func TestSendPlainRoundTrip(t *testing.T) {
	fc := &fakeCompleter{responses: []providers.ChatResponse{plainResp("hello there")}}
	svc, st := newService(t, fc)
	out := svc.Send(context.Background(), "hi", "ollama", "m1")
	if out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	if len(out.Rows) != 2 || out.Rows[0].Role != "user" || out.Rows[1].Content != "hello there" {
		t.Fatalf("rows: %+v", out.Rows)
	}
	if out.Rows[1].Provider != "ollama" || out.Rows[1].Model != "m1" {
		t.Fatalf("provenance: %+v", out.Rows[1])
	}
	if out.EndCursor != out.Rows[1].Seq {
		t.Fatalf("cursor: %d", out.EndCursor)
	}
	// Durable events: session events per row, in order.
	events, _ := st.Events(SessionID, "", 100)
	if len(events) != 2 || events[0].Kind != "session" {
		t.Fatalf("events: %+v", events)
	}
}

func TestSendOneToolRound(t *testing.T) {
	fc := &fakeCompleter{responses: []providers.ChatResponse{
		toolCallResp("c1", "clock.now", `{}`),
		plainResp("it is noon"),
	}}
	svc, st := newService(t, fc)
	out := svc.Send(context.Background(), "time?", "ollama", "m1")
	if out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	// user, intent, tool result, final = 4 rows.
	if len(out.Rows) != 4 {
		t.Fatalf("rows: %+v", out.Rows)
	}
	if out.Rows[1].ToolCalls == "" || out.Rows[2].Role != "tool" || out.Rows[2].ToolCallID != "c1" || out.Rows[2].ToolName != "clock.now" {
		t.Fatalf("tool rows: %+v", out.Rows[1:3])
	}
	if out.Rows[3].Content != "it is noon" {
		t.Fatalf("final: %+v", out.Rows[3])
	}
	events, _ := st.Events(SessionID, "", 100)
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Kind]++
	}
	if kinds["session"] != 4 || kinds["tool_call"] != 1 {
		t.Fatalf("event kinds: %+v", kinds)
	}
	// Tool result row content is the executed clock value.
	if !strings.Contains(out.Rows[2].Content, "T") {
		t.Fatalf("clock output: %q", out.Rows[2].Content)
	}
}

func TestSendToolRoundCap(t *testing.T) {
	// Model requests tools forever; the final round offers none and the
	// scripted last response is a plain answer.
	fc := &fakeCompleter{responses: []providers.ChatResponse{
		toolCallResp("c1", "echo", `{"text":"a"}`),
		toolCallResp("c2", "echo", `{"text":"b"}`),
		plainResp("stopped asking"),
	}}
	svc, _ := newService(t, fc)
	out := svc.Send(context.Background(), "loop", "ollama", "m1")
	if out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	if fc.seenTools[len(fc.seenTools)-1] != 0 {
		t.Fatalf("final round must offer no tools: %v", fc.seenTools)
	}
	if out.Rows[len(out.Rows)-1].Content != "stopped asking" {
		t.Fatalf("final: %+v", out.Rows[len(out.Rows)-1])
	}
}

func TestSendProviderFailureKeepsUserRow(t *testing.T) {
	fc := &fakeCompleter{errs: []error{errProvider}}
	svc, st := newService(t, fc)
	out := svc.Send(context.Background(), "hello?", "ollama", "m1")
	if out.Err == nil {
		t.Fatal("want typed failure")
	}
	if providers.ReasonOf(out.Err) != providers.ReasonUnreachable {
		t.Fatalf("reason: %v", out.Err)
	}
	if len(out.Rows) != 1 || out.Rows[0].Role != "user" {
		t.Fatalf("user row must persist: %+v", out.Rows)
	}
	if out.EndCursor != out.Rows[0].Seq {
		t.Fatalf("failure cursor: %d", out.EndCursor)
	}
	rows, _ := st.Replay(SessionID, 0, 100)
	if len(rows) != 1 {
		t.Fatalf("transcript: %+v", rows)
	}
}

func TestSendFailureAfterToolRoundKeepsCommittedRows(t *testing.T) {
	fc := &fakeCompleter{
		responses: []providers.ChatResponse{toolCallResp("c1", "echo", `{"text":"x"}`)},
		errs:      []error{nil, errProvider},
	}
	svc, st := newService(t, fc)
	out := svc.Send(context.Background(), "try tools then fail", "ollama", "m1")
	if out.Err == nil {
		t.Fatal("want failure")
	}
	// user + intent + tool row committed; no final assistant row.
	if len(out.Rows) != 3 {
		t.Fatalf("rows: %+v", out.Rows)
	}
	all, _ := st.Replay(SessionID, 0, 100)
	if len(all) != 3 || all[2].Role == "assistant" && all[2].ToolCalls == "" {
		t.Fatalf("committed rows: %+v", all)
	}
}

func TestSendUnknownToolModelSeesError(t *testing.T) {
	fc := &fakeCompleter{responses: []providers.ChatResponse{
		toolCallResp("c1", "does-not-exist", `{}`),
		plainResp("handled"),
	}}
	svc, _ := newService(t, fc)
	out := svc.Send(context.Background(), "?", "ollama", "m1")
	if out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	if !strings.Contains(out.Rows[2].Content, "unknown tool") {
		t.Fatalf("model-visible error: %+v", out.Rows[2])
	}
}

func TestSendSerializedTwoClients(t *testing.T) {
	fc := &fakeCompleter{responses: []providers.ChatResponse{plainResp("a"), plainResp("b")}}
	svc, st := newService(t, fc)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := svc.Send(context.Background(), fmt.Sprintf("q%d", i), "ollama", "m1")
			if out.Err != nil {
				t.Error(out.Err)
			}
		}(i)
	}
	wg.Wait()
	rows, _ := st.Replay(SessionID, 0, 100)
	// 4 rows: two complete exchanges, user/assistant adjacent — never
	// interleaved (user,user,assistant,assistant).
	if len(rows) != 4 {
		t.Fatalf("rows: %+v", rows)
	}
	if rows[0].Role == rows[1].Role {
		t.Fatalf("exchanges interleaved: %+v", rows)
	}
	if rows[2].Role == rows[3].Role {
		t.Fatalf("exchanges interleaved: %+v", rows)
	}
}

func TestSyncPagination(t *testing.T) {
	fc := &fakeCompleter{}
	for i := 0; i < 3; i++ {
		fc.responses = append(fc.responses, plainResp(fmt.Sprintf("r%d", i)))
	}
	svc, _ := newService(t, fc)
	for i := 0; i < 3; i++ {
		if out := svc.Send(context.Background(), fmt.Sprintf("q%d", i), "ollama", "m"); out.Err != nil {
			t.Fatal(out.Err)
		}
	}
	rows, next, truncated, err := svc.Sync(0, 3)
	if err != nil || len(rows) != 3 || !truncated || next != 3 {
		t.Fatalf("page1: rows=%d next=%d trunc=%v err=%v", len(rows), next, truncated, err)
	}
	rows, next, truncated, _ = svc.Sync(next, 100)
	if len(rows) != 3 || truncated || next != 6 {
		t.Fatalf("page2: rows=%d next=%d trunc=%v", len(rows), next, truncated)
	}
}

func TestNeedsCompactionSurfaces(t *testing.T) {
	fc := &fakeCompleter{}
	for i := 0; i < 5; i++ {
		fc.responses = append(fc.responses, plainResp("r"))
	}
	svc, _ := newService(t, fc)
	for i := 0; i < 5; i++ {
		if out := svc.Send(context.Background(), "q", "ollama", "m"); out.Err != nil {
			t.Fatal(out.Err)
		}
	}
	hw, err := svc.store.HistoryWindow(SessionID, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	// 10 rows > bound 5 — but the service sends with bound 200, so this
	// asserts the store signal the service would forward at its bound.
	if !hw.NeedsCompaction {
		t.Fatal("store must signal compaction over bound")
	}
	var _ = json.Marshal
}

// A broken composed persona FAILS the chat exchange before the model
// call (phase-3 gate: chat never runs persona-less); a working persona
// rides as the first system message, ahead of any compaction summary.
func TestChatPersona(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fc := &fakeCompleter{}
	svc := New(st, func(string) (providers.Provider, error) { return providers.Provider{Name: "p"}, nil },
		func(context.Context, providers.Provider) (string, error) { return "tok", nil },
		fc, tools.BuiltinProvider{}, nil, nil)
	svc.SetPersona(func() (string, error) { return "", errors.New("role missing") })
	if out := svc.Send(context.Background(), "hi", "p", "m"); out.Err == nil || !strings.Contains(out.Err.Error(), "role missing") {
		t.Fatalf("broken persona must fail the exchange: %+v", out.Err)
	}
	svc.SetPersona(func() (string, error) { return "## Role\n\nI am Karta.", nil })
	if out := svc.Send(context.Background(), "hi", "p", "m"); out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	if len(fc.lastMessages) == 0 || fc.lastMessages[0].Role != "system" || !strings.Contains(fc.lastMessages[0].Content, "I am Karta.") {
		t.Fatalf("persona must lead the messages: %+v", fc.lastMessages)
	}
}
