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
	out := svc.SendIn(context.Background(), DefaultSessionID, "hi", "ollama", "m1")
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
	events, _ := st.Events(DefaultSessionID, "", 100)
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
	out := svc.SendIn(context.Background(), DefaultSessionID, "time?", "ollama", "m1")
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
	events, _ := st.Events(DefaultSessionID, "", 100)
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
	out := svc.SendIn(context.Background(), DefaultSessionID, "loop", "ollama", "m1")
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
	out := svc.SendIn(context.Background(), DefaultSessionID, "hello?", "ollama", "m1")
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
	rows, _ := st.Replay(DefaultSessionID, 0, 100)
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
	out := svc.SendIn(context.Background(), DefaultSessionID, "try tools then fail", "ollama", "m1")
	if out.Err == nil {
		t.Fatal("want failure")
	}
	// user + intent + tool row committed; no final assistant row.
	if len(out.Rows) != 3 {
		t.Fatalf("rows: %+v", out.Rows)
	}
	all, _ := st.Replay(DefaultSessionID, 0, 100)
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
	out := svc.SendIn(context.Background(), DefaultSessionID, "?", "ollama", "m1")
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
			out := svc.SendIn(context.Background(), DefaultSessionID, fmt.Sprintf("q%d", i), "ollama", "m1")
			if out.Err != nil {
				t.Error(out.Err)
			}
		}(i)
	}
	wg.Wait()
	rows, _ := st.Replay(DefaultSessionID, 0, 100)
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
		if out := svc.SendIn(context.Background(), DefaultSessionID, fmt.Sprintf("q%d", i), "ollama", "m"); out.Err != nil {
			t.Fatal(out.Err)
		}
	}
	rows, next, truncated, err := svc.SyncIn(DefaultSessionID, 0, 3)
	if err != nil || len(rows) != 3 || !truncated || next != 3 {
		t.Fatalf("page1: rows=%d next=%d trunc=%v err=%v", len(rows), next, truncated, err)
	}
	rows, next, truncated, _ = svc.SyncIn(DefaultSessionID, next, 100)
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
		if out := svc.SendIn(context.Background(), DefaultSessionID, "q", "ollama", "m"); out.Err != nil {
			t.Fatal(out.Err)
		}
	}
	hw, err := svc.store.HistoryWindow(DefaultSessionID, "", 5)
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
	if out := svc.SendIn(context.Background(), DefaultSessionID, "hi", "p", "m"); out.Err == nil || !strings.Contains(out.Err.Error(), "role missing") {
		t.Fatalf("broken persona must fail the exchange: %+v", out.Err)
	}
	svc.SetPersona(func() (string, error) { return "## Role\n\nI am Karta.", nil })
	if out := svc.SendIn(context.Background(), DefaultSessionID, "hi", "p", "m"); out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	if len(fc.lastMessages) == 0 || fc.lastMessages[0].Role != "system" || !strings.Contains(fc.lastMessages[0].Content, "I am Karta.") {
		t.Fatalf("persona must lead the messages: %+v", fc.lastMessages)
	}
}

// The ward tool plane mounts on chat exchanges, and the Active Ward
// section composes onto the prompt once a ward is entered (the chat IS
// the research surface — operator direction).
func TestChatWardPlane(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fc := &fakeCompleter{}
	svc := New(st, func(string) (providers.Provider, error) { return providers.Provider{Name: "p"}, nil },
		func(context.Context, providers.Provider) (string, error) { return "tok", nil },
		fc, tools.BuiltinProvider{}, nil, nil)

	svc.SetExtraTools(func() []tools.Tool {
		return []tools.Tool{stubTool{name: "wards"}, stubTool{name: "ward_use"}}
	})
	svc.SetPersona(func() (string, error) { return "## Role\n\nI am Karta.", nil })
	svc.SetActiveWard(func() string { return "" }) // no ward yet

	if out := svc.SendIn(context.Background(), DefaultSessionID, "what wards exist", "p", "m"); out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	if len(fc.seenTools) == 0 || fc.seenTools[0] < 2 {
		t.Fatalf("ward tools not offered: %v", fc.seenTools)
	}
	// First system message: persona, no Active Ward.
	if !strings.Contains(fc.lastMessages[0].Content, "I am Karta.") || strings.Contains(fc.lastMessages[0].Content, "Active Ward") {
		t.Fatalf("prompt: %.120s", fc.lastMessages[0].Content)
	}

	// A ward becomes active → the section composes after the persona.
	svc.SetActiveWard(func() string { return "## Active Ward\n\nstock-analysis" })
	if out := svc.SendIn(context.Background(), DefaultSessionID, "continue", "p", "m"); out.Err != nil {
		t.Fatalf("send 2: %v", out.Err)
	}
	if !strings.Contains(fc.lastMessages[0].Content, "## Active Ward") || !strings.Contains(fc.lastMessages[0].Content, "stock-analysis") {
		t.Fatalf("active ward missing: %.200s", fc.lastMessages[0].Content)
	}
}

// stubTool is a named no-op tool for inventory assertions.
type stubTool struct{ name string }

func (s stubTool) Def() tools.Def                            { return tools.Def{Name: s.name, Description: "stub"} }
func (s stubTool) Exec(context.Context, string) tools.Result { return tools.Result{Output: "ok"} }

// TestSessionRouting (add-chat-modes 2.1): the session's TYPE routes
// the exchange — chat sessions run the lean exchange, research
// sessions run the wired research path (the engine pipeline), unknown
// ids fail typed, and the default session is created on first use.
func TestSessionRouting(t *testing.T) {
	t.Run("chat routes lean", func(t *testing.T) {
		fc := &fakeCompleter{responses: []providers.ChatResponse{plainResp("lean reply")}}
		svc, st := newService(t, fc)
		called := false
		svc.SetResearchPath(func(ctx context.Context, sessionID, text, providerName, model string) SendOutcome {
			called = true
			return SendOutcome{}
		})
		out := svc.SendIn(context.Background(), DefaultSessionID, "hi", "p", "m")
		if out.Err != nil || called {
			t.Fatalf("lean route: err=%v researchCalled=%v", out.Err, called)
		}
		if len(out.Rows) != 2 || out.Rows[1].Content != "lean reply" {
			t.Fatalf("rows: %+v", out.Rows)
		}
		sess, err := st.Session(DefaultSessionID)
		if err != nil || sess == nil || sess.Type != "chat" {
			t.Fatalf("default session: %v %+v", err, sess)
		}
	})
	t.Run("research routes the engine path", func(t *testing.T) {
		fc := &fakeCompleter{responses: []providers.ChatResponse{plainResp("should not run")}}
		svc, st := newService(t, fc)
		if _, err := st.EnsureSession("research-1", "research"); err != nil {
			t.Fatal(err)
		}
		gotSession := ""
		svc.SetResearchPath(func(ctx context.Context, sessionID, text, providerName, model string) SendOutcome {
			gotSession = sessionID
			return SendOutcome{Rows: []store.Message{{ID: "x", SessionID: sessionID, Seq: 1, Role: "assistant", Content: "engine ran"}}}
		})
		out := svc.SendIn(context.Background(), "research-1", "deep question", "p", "m")
		if out.Err != nil {
			t.Fatalf("research route: %v", out.Err)
		}
		if gotSession != "research-1" {
			t.Fatalf("research path session: %q", gotSession)
		}
		if len(out.Rows) != 1 || out.Rows[0].Content != "engine ran" {
			t.Fatalf("rows: %+v", out.Rows)
		}
	})
	t.Run("research without wiring falls back lean", func(t *testing.T) {
		fc := &fakeCompleter{responses: []providers.ChatResponse{plainResp("lean fallback")}}
		svc, st := newService(t, fc)
		if _, err := st.EnsureSession("research-2", "research"); err != nil {
			t.Fatal(err)
		}
		out := svc.SendIn(context.Background(), "research-2", "q", "p", "m")
		if out.Err != nil {
			t.Fatalf("fallback: %v", out.Err)
		}
		if out.Rows[1].Content != "lean fallback" {
			t.Fatalf("rows: %+v", out.Rows)
		}
	})
	t.Run("unknown session fails typed", func(t *testing.T) {
		fc := &fakeCompleter{}
		svc, _ := newService(t, fc)
		out := svc.SendIn(context.Background(), "nope", "hi", "p", "m")
		if !errors.Is(out.Err, ErrUnknownSession) {
			t.Fatalf("want ErrUnknownSession, got %v", out.Err)
		}
	})
	t.Run("streaming routes research as a unit", func(t *testing.T) {
		fc := &fakeCompleter{}
		svc, st := newService(t, fc)
		if _, err := st.EnsureSession("research-3", "research"); err != nil {
			t.Fatal(err)
		}
		svc.SetResearchPath(func(ctx context.Context, sessionID, text, providerName, model string) SendOutcome {
			return SendOutcome{Rows: []store.Message{{ID: "y", SessionID: sessionID, Seq: 1, Role: "assistant", Content: "unit"}}}
		})
		var deltas int
		out := svc.SendStreamIn(context.Background(), "research-3", "q", "p", "m", func(Delta) { deltas++ })
		if out.Err != nil || deltas != 0 {
			t.Fatalf("unit completion: err=%v deltas=%d", out.Err, deltas)
		}
	})
}

// TestLeanContextRecall (add-chat-modes): the lean exchange injects
// recalled facts as a ## Context system message (the type gates the
// pipeline, never the knowledge).
func TestLeanContextRecall(t *testing.T) {
	fc := &fakeCompleter{responses: []providers.ChatResponse{plainResp("ok")}}
	svc, _ := newService(t, fc)
	svc.SetContext(func(ctx context.Context, text string) []string {
		return []string{"user prefers concise answers", "user is in Bangalore"}
	})
	out := svc.SendIn(context.Background(), DefaultSessionID, "hi", "p", "m")
	if out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	var contextMsg string
	for _, m := range fc.lastMessages {
		if m.Role == "system" && strings.HasPrefix(m.Content, "## Context") {
			contextMsg = m.Content
		}
	}
	if contextMsg == "" {
		t.Fatalf("no ## Context system message: %+v", fc.lastMessages)
	}
	for _, want := range []string{"user prefers concise answers", "user is in Bangalore"} {
		if !strings.Contains(contextMsg, want) {
			t.Fatalf("context missing %q: %q", want, contextMsg)
		}
	}
}

// TestResearchSessionBusyGuard (gate fix): one live engine run per
// research session — an overlapping send fails typed ErrSessionBusy
// instead of interleaving transcripts; the guard releases when the
// run drains.
func TestResearchSessionBusyGuard(t *testing.T) {
	fc := &fakeCompleter{}
	svc, st := newService(t, fc)
	if _, err := st.EnsureSession("research-busy", "research"); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{})
	svc.SetResearchPath(func(ctx context.Context, sessionID, text, providerName, model string) SendOutcome {
		close(started)
		<-release
		return SendOutcome{Rows: []store.Message{{ID: "x", SessionID: sessionID, Seq: 1, Role: "assistant", Content: "done"}}}
	})
	go func() {
		defer close(done)
		out := svc.SendIn(context.Background(), "research-busy", "first", "p", "m")
		if out.Err != nil {
			t.Errorf("first send: %v", out.Err)
		}
	}()
	<-started
	// Overlapping sends (sync and streaming) fail typed while the run is live.
	for _, send := range []func() SendOutcome{
		func() SendOutcome { return svc.SendIn(context.Background(), "research-busy", "second", "p", "m") },
		func() SendOutcome { return svc.SendStreamIn(context.Background(), "research-busy", "second", "p", "m", nil) },
	} {
		out := send()
		if !errors.Is(out.Err, ErrSessionBusy) {
			t.Fatalf("want ErrSessionBusy, got %v", out.Err)
		}
	}
	// A different session is unaffected (swap to an immediate path so
	// the probe does not block on the first run's release).
	svc.SetResearchPath(func(ctx context.Context, sessionID, text, providerName, model string) SendOutcome {
		return SendOutcome{Rows: []store.Message{{ID: "z", SessionID: sessionID, Seq: 1, Role: "assistant", Content: "other"}}}
	})
	if _, err := st.EnsureSession("research-other", "research"); err != nil {
		t.Fatal(err)
	}
	out := svc.SendIn(context.Background(), "research-other", "q", "p", "m")
	if errors.Is(out.Err, ErrSessionBusy) || out.Err != nil {
		t.Fatalf("busy leaked across sessions: %v", out.Err)
	}
	close(release)
	<-done // the first run drained and released the guard
	// The guard released: a new send in the first session routes again.
	out = svc.SendIn(context.Background(), "research-busy", "third", "p", "m")
	if out.Err != nil || len(out.Rows) != 1 || out.Rows[0].Content != "other" {
		t.Fatalf("after release: %+v (%v)", out.Rows, out.Err)
	}
}
