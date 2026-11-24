package chat

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// fakeStreamCompleter scripts streamed completions: responses arrive as
// delta scripts; the assembled ChatResponse mirrors what the provider
// client returns.
type fakeStreamCompleter struct {
	mu      sync.Mutex
	scripts [][]providers.ChatStreamDelta
	errs    []error
	calls   int
}

func (f *fakeStreamCompleter) ChatCompletion(ctx context.Context, p providers.Provider, model, token string, messages []providers.ChatMessage, defs []providers.ChatToolDef) (providers.ChatResponse, error) {
	// Streaming completer also satisfies the sync seam (unused on the
	// stream path; panic-free degenerate path).
	deltas, err := f.ChatCompletionStreamFull(ctx, p, model, token, messages, defs, nil)
	return deltas, err
}

// ChatCompletionStreamFull exposes the assembled response without the
// interface's onDelta requirement.
func (f *fakeStreamCompleter) ChatCompletionStreamFull(_ context.Context, _ providers.Provider, _, _ string, _ []providers.ChatMessage, _ []providers.ChatToolDef, onDelta func(providers.ChatStreamDelta)) (providers.ChatResponse, error) {
	f.mu.Lock()
	i := f.calls
	f.calls++
	var script []providers.ChatStreamDelta
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	if i < len(f.scripts) {
		script = f.scripts[i]
	}
	f.mu.Unlock()
	if err != nil {
		return providers.ChatResponse{}, err
	}
	return assembleStream(script, onDelta), nil
}

func (f *fakeStreamCompleter) ChatCompletionStream(ctx context.Context, p providers.Provider, model, token string, messages []providers.ChatMessage, defs []providers.ChatToolDef, onDelta func(providers.ChatStreamDelta)) (providers.ChatResponse, error) {
	return f.ChatCompletionStreamFull(ctx, p, model, token, messages, defs, onDelta)
}

// assembleStream replays a delta script the way a provider would:
// fragments stream, then the response carries the assembled text.
func assembleStream(script []providers.ChatStreamDelta, onDelta func(providers.ChatStreamDelta)) providers.ChatResponse {
	var res providers.ChatResponse
	res.Message.Role = "assistant"
	for _, d := range script {
		if onDelta != nil {
			onDelta(d)
		}
		switch d.Kind {
		case providers.DeltaReasoning:
			res.Message.Reasoning += d.Text
		case providers.DeltaContent:
			if d.Text == "\x00tool_calls" {
				// Script marker: this round wants a tool call.
				res.FinishReason = "tool_calls"
				res.Message.ToolCalls = []providers.ChatToolCall{{ID: "c1", Type: "function"}}
				res.Message.ToolCalls[0].Function.Name = "clock_now"
				res.Message.ToolCalls[0].Function.Arguments = "{}"
				return res
			}
			res.Message.Content += d.Text
		}
	}
	res.FinishReason = "stop"
	return res
}

func newStreamService(t *testing.T, fc *fakeStreamCompleter, bus *eventbus.Bus) *Service {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStoreBacked(db, streamProvStore{}, fc, tools.BuiltinProvider{}, bus, fixedIDGen())
}

type streamProvStore struct{}

func (streamProvStore) Lookup(string) (providers.Provider, error) {
	return providers.Provider{Name: "p"}, nil
}

func (streamProvStore) ResolvedLookup(_ context.Context, _ string) (providers.Provider, string, error) {
	return providers.Provider{Name: "p"}, "tok", nil
}

func (streamProvStore) Secrets() providers.SecretStore { return nil }

func fixedIDGen() func() string {
	var n int
	return func() string {
		n++
		return idOf(n)
	}
}
func idOf(n int) string {
	return "msg-test-" + string(rune('A'+n-1%26)) + time.Now().Format("150405.000000000")
}

// TestSendStreamReasoningPersistsOnce: deltas stream (direct leg), the
// assistant row persists ONCE with reasoning, and the sync-shape outcome
// is identical.
func TestSendStreamReasoningPersistsOnce(t *testing.T) {
	t.Parallel()
	fc := &fakeStreamCompleter{scripts: [][]providers.ChatStreamDelta{{
		{Kind: providers.DeltaReasoning, Text: "think "},
		{Kind: providers.DeltaReasoning, Text: "hard"},
		{Kind: providers.DeltaContent, Text: "Hello"},
		{Kind: providers.DeltaContent, Text: " world"},
	}}}
	bus := eventbus.New()
	svc := newStreamService(t, fc, bus)

	var got []string
	out := svc.SendStreamIn(context.Background(), DefaultSessionID, "hi", "p", "m", func(d Delta) {
		got = append(got, d.Kind+":"+d.Text)
	})
	if out.Err != nil {
		t.Fatalf("send: %v", out.Err)
	}
	if len(out.Rows) != 2 || out.Rows[1].Role != "assistant" {
		t.Fatalf("rows wrong: %+v", out.Rows)
	}
	if out.Rows[1].Reasoning != "think hard" || out.Rows[1].Content != "Hello world" {
		t.Fatalf("assistant row wrong: %+v", out.Rows[1])
	}
	want := []string{"reasoning:think ", "reasoning:hard", "content:Hello", "content: world"}
	if len(got) != len(want) {
		t.Fatalf("direct deltas %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("direct deltas %v", got)
		}
	}
}

// TestSendStreamHundredDeltasOneRowInvariant: hundreds of volatile deltas
// must not multiply durable events — replay matches the sync equivalent.
func TestSendStreamHundredDeltasOneRowInvariant(t *testing.T) {
	t.Parallel()
	var script []providers.ChatStreamDelta
	for i := 0; i < 300; i++ {
		script = append(script, providers.ChatStreamDelta{Kind: providers.DeltaContent, Text: "x"})
	}
	fc := &fakeStreamCompleter{scripts: [][]providers.ChatStreamDelta{script}}
	svc := newStreamService(t, fc, eventbus.New())

	out := svc.SendStreamIn(context.Background(), DefaultSessionID, "hi", "p", "m", nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("want exactly user+assistant rows, got %d", len(out.Rows))
	}
	if len(out.Rows[1].Content) != 300 {
		t.Fatalf("content assembly wrong: %d bytes", len(out.Rows[1].Content))
	}
	events, err := svc.store.Events(DefaultSessionID, "", 64)
	if err != nil {
		t.Fatal(err)
	}
	// Durable events: exactly the per-row session events (user + assistant).
	if len(events) != 2 {
		t.Fatalf("durable events multiplied: %d (want 2 per-row events)", len(events))
	}
}

// TestSendStreamToolRound: streamed content then a tool round then the
// final answer — rows and order identical to the sync semantics.
func TestSendStreamToolRound(t *testing.T) {
	t.Parallel()
	fc := &fakeStreamCompleter{scripts: [][]providers.ChatStreamDelta{
		{{Kind: providers.DeltaContent, Text: "\x00tool_calls"}},
		{{Kind: providers.DeltaReasoning, Text: "r2"}, {Kind: providers.DeltaContent, Text: "final"}},
	}}
	svc := newStreamService(t, fc, eventbus.New())
	out := svc.SendStreamIn(context.Background(), DefaultSessionID, "hi", "p", "m", nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	roles := []string{}
	for _, r := range out.Rows {
		roles = append(roles, r.Role)
	}
	want := "user assistant tool assistant"
	got := ""
	for _, r := range roles {
		got += r + " "
	}
	if got[:len(got)-1] != want {
		t.Fatalf("row order wrong: %v", roles)
	}
	last := out.Rows[len(out.Rows)-1]
	if last.Reasoning != "r2" || last.Content != "final" {
		t.Fatalf("final row wrong: %+v", last)
	}
}

// TestSendStreamFailureMidStream (F-15 stream leg): a provider failure
// after deltas keeps the user row, appends no assistant row, returns the
// typed error — and a closed bus never kills the exchange.
func TestSendStreamFailureMidStream(t *testing.T) {
	t.Parallel()
	fc := &fakeStreamCompleter{
		scripts: [][]providers.ChatStreamDelta{{{Kind: providers.DeltaContent, Text: "partial"}}},
		errs:    []error{nil},
	}
	// Second call fails: script only has one entry; force an error by
	// pre-seeding errs for call 1.
	fc.errs = append(fc.errs, &providers.Failure{Reason: providers.ReasonUnreachable})
	bus := eventbus.New()
	svc := newStreamService(t, fc, bus)
	bus.Close() // volatile leg must swallow ErrClosed — F-15 family

	out := svc.SendStreamIn(context.Background(), DefaultSessionID, "hi", "p", "m", func(Delta) {})
	if out.Err != nil || len(out.Rows) != 2 {
		t.Fatalf("closed bus must not kill the exchange: err=%v rows=%d", out.Err, len(out.Rows))
	}
	fc2 := &fakeStreamCompleter{
		scripts: [][]providers.ChatStreamDelta{
			{{Kind: providers.DeltaContent, Text: "\x00tool_calls"}},
		},
		errs: []error{nil, &providers.Failure{Reason: providers.ReasonUnreachable}},
	}
	svc2 := newStreamService(t, fc2, eventbus.New())
	out2 := svc2.SendStreamIn(context.Background(), DefaultSessionID, "hi", "p", "m", nil)
	if out2.Err == nil {
		t.Fatal("want typed failure")
	}
	if len(out2.Rows) < 3 || out2.Rows[len(out2.Rows)-1].Role == "assistant" {
		t.Fatalf("failure must keep user+intent+tool rows without a final answer: %+v", out2.Rows)
	}
}

// TestSendStreamBusDeltasCoalesced: the bus leg receives coalesced
// chat_delta events (fewer than fragments), the direct leg receives all.
func TestSendStreamBusDeltasCoalesced(t *testing.T) {
	t.Parallel()
	var script []providers.ChatStreamDelta
	for i := 0; i < 50; i++ {
		script = append(script, providers.ChatStreamDelta{Kind: providers.DeltaContent, Text: "y"})
	}
	fc := &fakeStreamCompleter{scripts: [][]providers.ChatStreamDelta{script}}
	bus := eventbus.New()
	sub := bus.Subscribe(context.Background(), DefaultSessionID)
	svc := newStreamService(t, fc, bus)

	var direct int
	out := svc.SendStreamIn(context.Background(), DefaultSessionID, "hi", "p", "m", func(Delta) { direct++ })
	if out.Err != nil || direct != 50 {
		t.Fatalf("direct leg must see every fragment: %d, err %v", direct, out.Err)
	}
	// Drain the subscription: coalesced bus events exist, are FEWER than
	// the 50 fragments (coalescing), and every text byte arrives via the
	// coalesced payloads (no loss).
	var busDeltas, busBytes int
	deadline := time.After(2 * time.Second)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			select {
			case evt, ok := <-sub.Events():
				if !ok {
					return
				}
				if evt.Kind == "chat_delta" {
					busDeltas++
					busBytes += len(evt.Text)
				}
			case <-time.After(300 * time.Millisecond):
				return // quiet: coalescer flushed and closed
			}
		}
	}()
	select {
	case <-drained:
	case <-deadline:
	}
	if busDeltas == 0 {
		t.Fatal("no coalesced bus delta arrived")
	}
	if busDeltas >= 50 {
		t.Fatalf("coalescing failed: %d bus events for 50 fragments", busDeltas)
	}
	if busBytes != 50 {
		t.Fatalf("coalesced bus leg lost bytes: %d of 50", busBytes)
	}
}

// TestCoalescerCloseVsTickRace: close must win against an in-flight or
// waiting tick — no timer resurrection after close (gate finding #3).
func TestCoalescerCloseVsTickRace(t *testing.T) {
	t.Parallel()
	svc := &fakeStreamCompleter{}
	bus := eventbus.New()
	s := newStreamService(t, svc, bus)
	for i := 0; i < 200; i++ {
		c := newDeltaCoalescer(s, DefaultSessionID, nil)
		c.push(Delta{Kind: "content", Text: "x"})
		// tick and close race every iteration; -race arbitrates.
		go c.tick()
		c.close()
		c.push(Delta{Kind: "content", Text: "y"}) // no-op bus leg after close
	}
}

// TestSendStreamIntentRowKeepsReasoning (gate finding #4): reasoning
// streamed before a tool call persists on the INTENT row too.
func TestSendStreamIntentRowKeepsReasoning(t *testing.T) {
	t.Parallel()
	fc := &fakeStreamCompleter{scripts: [][]providers.ChatStreamDelta{
		{
			{Kind: providers.DeltaReasoning, Text: "need the time"},
			{Kind: providers.DeltaContent, Text: "\x00tool_calls"},
		},
		{{Kind: providers.DeltaContent, Text: "done"}},
	}}
	svc := newStreamService(t, fc, eventbus.New())
	out := svc.SendStreamIn(context.Background(), DefaultSessionID, "time?", "p", "m", nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	var intent store.Message
	for _, r := range out.Rows {
		if r.Role == "assistant" && r.ToolCalls != "" {
			intent = r
		}
	}
	if intent.ID == "" {
		t.Fatal("no intent row")
	}
	if intent.Reasoning != "need the time" {
		t.Fatalf("intent row lost reasoning: %+v", intent)
	}
	// Replay confirms persistence, not just the outcome slice.
	rows, _ := svc.store.Replay(DefaultSessionID, 0, 16)
	found := false
	for _, r := range rows {
		if r.ID == intent.ID && r.Reasoning == "need the time" {
			found = true
		}
	}
	if !found {
		t.Fatal("replay lost intent reasoning")
	}
}

// TestSendStreamNoDeltaAfterCompletion (gate finding #6): across
// consecutive sends, a subscriber must never see a chat_delta after the
// exchange's durable completion event for that send.
func TestSendStreamNoDeltaAfterCompletion(t *testing.T) {
	t.Parallel()
	fc := &fakeStreamCompleter{scripts: [][]providers.ChatStreamDelta{
		{{Kind: providers.DeltaContent, Text: "first"}},
		{{Kind: providers.DeltaContent, Text: "second"}},
	}}
	bus := eventbus.New()
	svc := newStreamService(t, fc, bus)
	sub := bus.Subscribe(context.Background(), DefaultSessionID)

	if out := svc.SendStreamIn(context.Background(), DefaultSessionID, "a", "p", "m", nil); out.Err != nil {
		t.Fatal(out.Err)
	}
	if out := svc.SendStreamIn(context.Background(), DefaultSessionID, "b", "p", "m", nil); out.Err != nil {
		t.Fatal(out.Err)
	}
	// Drain every published event in order.
	var order []string
	deadline := time.After(1 * time.Second)
	for {
		var evt domain.TimelineEvent
		select {
		case evt = <-sub.Events():
			order = append(order, string(evt.Kind))
		case <-deadline:
			goto done
		}
	}
done:
	lastDurable := -1
	for i, k := range order {
		if k == "session" {
			lastDurable = i
		}
	}
	for i := lastDurable + 1; i < len(order); i++ {
		if order[i] == "chat_delta" {
			t.Fatalf("chat_delta after final durable event: %v", order)
		}
	}
}

// TestSendStreamAirtightInvariant (gate finding #7): exact content, exact
// row count, exact durable event kinds — no looseness.
func TestSendStreamAirtightInvariant(t *testing.T) {
	t.Parallel()
	var script []providers.ChatStreamDelta
	for i := 0; i < 300; i++ {
		script = append(script, providers.ChatStreamDelta{Kind: providers.DeltaContent, Text: "x"})
	}
	fc := &fakeStreamCompleter{scripts: [][]providers.ChatStreamDelta{script}}
	svc := newStreamService(t, fc, eventbus.New())
	out := svc.SendStreamIn(context.Background(), DefaultSessionID, "hi", "p", "m", nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("rows: %d", len(out.Rows))
	}
	if want := strings.Repeat("x", 300); out.Rows[1].Content != want {
		t.Fatalf("content assembly: %d bytes", len(out.Rows[1].Content))
	}
	events, _ := svc.store.Events(DefaultSessionID, "", 8)
	if len(events) != 2 || events[0].Kind != "session" || events[1].Kind != "session" {
		t.Fatalf("durable events: %+v", events)
	}
}
