package server

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/providers"
	apigen "zbot/backend/internal/server/apigen"
	zbotstore "zbot/backend/internal/store"
)

// scriptedStreamChat scripts SendStream: deltas then an outcome.
type scriptedStreamChat struct {
	deltas  []chat.Delta
	outcome chat.SendOutcome
	onCall  func()
}

func (s *scriptedStreamChat) Send(ctx context.Context, text, providerName, model string) chat.SendOutcome {
	return chat.SendOutcome{Err: errors.New("sync unused on stream tests")}
}
func (s *scriptedStreamChat) SendStream(ctx context.Context, text, providerName, model string, onDelta func(chat.Delta)) chat.SendOutcome {
	if s.onCall != nil {
		s.onCall()
	}
	for _, d := range s.deltas {
		if onDelta != nil {
			onDelta(d)
		}
	}
	return s.outcome
}
func (s *scriptedStreamChat) Sync(afterSeq int64, limit int) ([]zbotstore.Message, int64, bool, error) {
	return nil, afterSeq, false, nil
}

func newStreamDaemon(t *testing.T, svc ChatService) *httptest.Server {
	t.Helper()
	// Chat endpoints need no providers store; nil keeps the test minimal.
	return httptest.NewServer(New(nil, providers.NewClient(), nil, svc, nil, nil, nil, nil, nil, nil, "test").Routes())
}

func readSSE(t *testing.T, url, body string) (string, int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var sb strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		sb.WriteString(sc.Text())
		sb.WriteString("\n")
	}
	ct := resp.Header.Get("Content-Type")
	return sb.String(), resp.StatusCode, ct
}

func TestStreamChatMessageHappyPath(t *testing.T) {
	t.Parallel()
	svc := &scriptedStreamChat{
		deltas: []chat.Delta{
			{Kind: "reasoning", Text: "think"},
			{Kind: "content", Text: "Hi"},
			{Kind: "content", Text: " there"},
		},
		outcome: chat.SendOutcome{
			Rows: []zbotstore.Message{
				{ID: "m1", Seq: 1, Role: "user", Content: "hello"},
				{ID: "m2", Seq: 2, Role: "assistant", Content: "Hi there", Reasoning: "think"},
			},
			EndCursor: 2,
		},
	}
	daemon := newStreamDaemon(t, svc)
	defer daemon.Close()

	body, status, ct := readSSE(t, daemon.URL+"/api/chat/send", `{"text":"hello","model":"p/m"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	if !strings.Contains(body, "event: chat_delta") {
		t.Fatal("no chat_delta events")
	}
	if strings.Count(body, "event: chat_delta") != 3 {
		t.Fatalf("want 3 delta events, got %d", strings.Count(body, "event: chat_delta"))
	}
	if !strings.Contains(body, `"kind":"reasoning"`) || !strings.Contains(body, `"text":"think"`) {
		t.Fatalf("reasoning delta missing: %s", body)
	}
	// Terminal exchange: exactly one, carrying rows + reasoning.
	if strings.Count(body, "event: chat_exchange") != 1 {
		t.Fatalf("want exactly 1 terminal exchange, got %d", strings.Count(body, "event: chat_exchange"))
	}
	if !strings.Contains(body, `"reasoning":"think"`) {
		t.Fatalf("terminal assistant row missing reasoning: %s", body)
	}
	// Order: the last event block is the terminal exchange.
	if last := body[strings.LastIndex(body, "event:"):]; !strings.HasPrefix(last, "event: chat_exchange") {
		t.Fatalf("terminal exchange must be last, got: %q", last)
	}
}

func TestStreamChatMessagePreStreamBadRequests(t *testing.T) {
	t.Parallel()
	svc := &scriptedStreamChat{}
	daemon := newStreamDaemon(t, svc)
	defer daemon.Close()
	for _, body := range []string{`{"text":"  ","model":"p/m"}`, `{"text":"hi","model":"no-slash"}`, `not json`} {
		resp, err := http.Post(daemon.URL+"/api/chat/send", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q: want 400 before any stream, got %d", body, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("body %q: pre-stream errors are typed JSON, got %q", body, ct)
		}
	}
}

func TestStreamChatMessageDisconnectCompletesExchange(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 1)
	completed := make(chan struct{})
	svc := &scriptedStreamChat{
		deltas: []chat.Delta{{Kind: "content", Text: "partial"}},
		outcome: chat.SendOutcome{
			Rows:      []zbotstore.Message{{ID: "m1", Seq: 1, Role: "user", Content: "hi"}},
			EndCursor: 1,
		},
		onCall: func() { started <- struct{}{} },
	}
	// Wrap SendStream completion detection: after outcome returns the
	// handler writes the terminal event; detect via a second outcome field.
	daemon := newStreamDaemon(t, &instrumentedStreamChat{inner: svc, done: completed})
	defer daemon.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, daemon.URL+"/api/chat/send",
		strings.NewReader(`{"text":"hi","model":"p/m"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel() // client disconnects mid-stream
	_ = resp.Body.Close()

	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("exchange did not complete after client disconnect (WithoutCancel broken?)")
	}
}

type instrumentedStreamChat struct {
	inner *scriptedStreamChat
	done  chan struct{}
}

func (i *instrumentedStreamChat) Send(ctx context.Context, text, providerName, model string) chat.SendOutcome {
	return i.inner.Send(ctx, text, providerName, model)
}
func (i *instrumentedStreamChat) SendStream(ctx context.Context, text, providerName, model string, onDelta func(chat.Delta)) chat.SendOutcome {
	out := i.inner.SendStream(ctx, text, providerName, model, onDelta)
	close(i.done)
	return out
}
func (i *instrumentedStreamChat) Sync(afterSeq int64, limit int) ([]zbotstore.Message, int64, bool, error) {
	return nil, afterSeq, false, nil
}

// TestStreamWriterDropsInsteadOfBlocking (gate finding #2): a full buffer
// drops frames — send NEVER blocks the exchange on a stalled reader.
func TestStreamWriterDropsInsteadOfBlocking(t *testing.T) {
	t.Parallel()
	rec := &blockingRecorder{unblock: make(chan struct{})}
	sw := newStreamWriter(rec, rec)
	defer sw.close()
	// Fill the buffer, then keep sending far beyond capacity: every send
	// must return immediately (would deadlock the test if it blocked).
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			sw.send(sseMessage{event: "chat_delta", payload: apigen.ChatDelta{Kind: apigen.Content, Text: "x"}})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send blocked on a stalled writer — exchange would stall holding sendMu")
	}
	close(rec.unblock)
}

// blockingRecorder is a ResponseWriter whose writes block until unblocked
// (a stalled reader).
type blockingRecorder struct {
	unblock chan struct{}
}

func (b *blockingRecorder) Header() http.Header       { return http.Header{} }
func (b *blockingRecorder) Write([]byte) (int, error) { <-b.unblock; return 0, nil }
func (b *blockingRecorder) WriteHeader(int)           {}
func (b *blockingRecorder) Flush()                    {}

// TestStreamIdleGapDoesNotConsumeBudget proves the slow-generation
// contract on a REAL TCP connection (deadline mode): two frames
// separated by a gap longer than the write deadline both arrive —
// deadlines are armed only around writes, never across idle gaps.
func TestStreamIdleGapDoesNotConsumeBudget(t *testing.T) {
	// NOT parallel: it mutates the package-level streamWriteDeadline var.
	// Short window for the test (restored): the gap still exceeds the
	// per-write deadline, proving deadlines are disarmed across idles.
	orig := streamWriteDeadline
	streamWriteDeadline = 250 * time.Millisecond
	t.Cleanup(func() { streamWriteDeadline = orig })
	gap := streamWriteDeadline + 450*time.Millisecond
	svc := &scriptedStreamChat{
		deltas: []chat.Delta{
			{Kind: "content", Text: "before-gap"},
			{Kind: "content", Text: "after-gap"},
		},
		outcome: chat.SendOutcome{
			Rows: []zbotstore.Message{
				{ID: "m1", Seq: 1, Role: "user", Content: "hi"},
			},
			EndCursor: 1,
		},
	}
	daemon := newStreamDaemon(t, &gappedStreamChat{inner: svc, gap: gap})
	defer daemon.Close()

	start := time.Now()
	body, status, _ := readSSE(t, daemon.URL+"/api/chat/send", `{"text":"hi","model":"p/m"}`)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if !strings.Contains(body, "before-gap") || !strings.Contains(body, "after-gap") {
		t.Fatalf("idle gap consumed write budget — frames lost: %s", body)
	}
	if !strings.Contains(body, "event: chat_exchange") {
		t.Fatal("terminal lost across the gap")
	}
	if elapsed := time.Since(start); elapsed < gap {
		t.Fatalf("test bug: gap not actually exercised (%s < %s)", elapsed, gap)
	}
}

// gappedStreamChat inserts a real delay between deltas.
type gappedStreamChat struct {
	inner *scriptedStreamChat
	gap   time.Duration
}

func (g *gappedStreamChat) Send(ctx context.Context, text, providerName, model string) chat.SendOutcome {
	return g.inner.Send(ctx, text, providerName, model)
}
func (g *gappedStreamChat) SendStream(ctx context.Context, text, providerName, model string, onDelta func(chat.Delta)) chat.SendOutcome {
	if onDelta != nil && len(g.inner.deltas) > 0 {
		onDelta(g.inner.deltas[0])
		time.Sleep(g.gap) // idle generation gap longer than the write deadline
		for _, d := range g.inner.deltas[1:] {
			onDelta(d)
		}
	}
	return g.inner.outcome
}
func (g *gappedStreamChat) Sync(afterSeq int64, limit int) ([]zbotstore.Message, int64, bool, error) {
	return nil, afterSeq, false, nil
}
