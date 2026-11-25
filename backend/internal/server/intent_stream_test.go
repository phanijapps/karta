package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	zbotstore "zbot/backend/internal/store"
)

// The analysis event's payload survives the durable replay and live SSE
// paths (phase-3 gate F2): complexity, approach, ward, drops, and the
// cost fields reach clients, not just text.
func TestIntentAnalysisPayloadStreams(t *testing.T) {
	db, err := zbotstore.Open(t.TempDir() + "/intent.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	analysis := &domain.IntentAnalysisPayload{
		Status: domain.IntentStatusAnalyzed, PrimaryIntent: "do-research",
		Complexity: domain.ComplexityL, StepBudget: 16, Approach: domain.ApproachGraph,
		WardAction: domain.WardUseExisting, WardName: "stock-analysis",
		Dropped: []string{"phantom-skill"},
		Model:   "zai/glm-5.3-flash", TokensIn: 1200, TokensOut: 180, Attempts: 2, DurationMS: 1400,
	}
	payload, _ := json.Marshal(map[string]any{"text": "Task Analysis: do-research", "intent_analysis": analysis})
	if err := db.AppendEvent(zbotstore.Event{
		ID: "evt-i1", SessionID: "sess-1", Kind: string(domain.EventAnalysisCompleted), Payload: string(payload),
	}); err != nil {
		t.Fatal(err)
	}
	srv := New(nil, nil, nil, nil, nil, eventbus.New(), db, nil, nil, nil, nil, "test")
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()

	// The SSE stream tails forever: bound the read, collect what
	// arrived (the replay event is emitted immediately).
	var sb strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, daemon.URL+"/api/sessions/sess-1/events", nil)
	res, err := daemon.Client().Do(req)
	if err == nil {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			sb.WriteString(sc.Text() + "\n")
		}
	} else if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("GET events: %v", err)
	}
	body := sb.String()
	for _, want := range []string{"event: analysis_completed", "intent_analysis", "do-research", "\"L\"", "16", "stock-analysis", "phantom-skill", "1200", "1400"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %v:\n%s", want, body)
		}
	}
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type: %s", res.Header.Get("Content-Type"))
	}
}

// Pre-upgrade rows carry the single intent_analysis kind; replay maps
// each to the terminal analysis kind its status names (payload intact)
// so old sessions keep their card.
func TestReplayTranslatesLegacyIntentAnalysis(t *testing.T) {
	db, err := zbotstore.Open(t.TempDir() + "/legacy.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	persist := func(id, status string) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"text": "t", "intent_analysis": map[string]any{"status": status, "primary_intent": id}})
		if err := db.AppendEvent(zbotstore.Event{ID: id, SessionID: "sess-l", Kind: "intent_analysis", Payload: string(payload)}); err != nil {
			t.Fatal(err)
		}
	}
	persist("evt-bypassed", "bypassed")
	persist("evt-failed", "failed")
	persist("evt-analyzed", "analyzed")
	// The flat legacy form (the payload IS the analysis) also translates.
	if err := db.AppendEvent(zbotstore.Event{ID: "evt-flat", SessionID: "sess-l", Kind: "intent_analysis", Payload: `{"status":"bypassed","reason":"trivial"}`}); err != nil {
		t.Fatal(err)
	}
	srv := New(nil, nil, nil, nil, nil, eventbus.New(), db, nil, nil, nil, nil, "test")
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()
	var sb strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, daemon.URL+"/api/sessions/sess-l/events", nil)
	res, err := daemon.Client().Do(req)
	if err == nil {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			sb.WriteString(sc.Text() + "\n")
		}
	} else if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("GET events: %v", err)
	}
	body := sb.String()
	for _, want := range []string{"event: analysis_bypassed", "event: analysis_failed", "event: analysis_completed", "evt-analyzed", "evt-flat", "trivial"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %v:\n%s", want, body)
		}
	}
	if strings.Contains(body, "event: intent_analysis") {
		t.Fatalf("legacy kind must not leak to clients:\n%s", body)
	}
}

// run-legibility: every SSE frame names its agent and execution — live
// frames from the bus event, replayed rows from the payload's agent or,
// for rows that predate it, from the execution they belong to; the
// delegation lifecycle payload rides its kinds.
func TestSSEFramesAttributed(t *testing.T) {
	db, err := zbotstore.Open(t.TempDir() + "/attrib.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root, _ := db.EnsureSession("sess-a", "research")
	// A legacy row: no agent in the payload, attributed through the join.
	child, _ := db.CreateChildExecution("sess-a", root, "research-agent")
	legacy, _ := json.Marshal(map[string]any{"text": "child reads", "tool": "read"})
	if err := db.AppendEvent(zbotstore.Event{ID: "evt-legacy", SessionID: "sess-a", ExecutionID: child, Kind: "tool_call", Payload: string(legacy)}); err != nil {
		t.Fatal(err)
	}
	// A current row: the payload's agent wins over the join.
	cur, _ := json.Marshal(map[string]any{"text": "t", "agent": "root"})
	if err := db.AppendEvent(zbotstore.Event{ID: "evt-cur", SessionID: "sess-a", ExecutionID: root, Kind: "agent_step", Payload: string(cur)}); err != nil {
		t.Fatal(err)
	}
	// A delegation lifecycle row with its payload.
	dp, _ := json.Marshal(map[string]any{"text": "research-agent finished", "agent": "research-agent", "delegation": map[string]any{"child_agent": "research-agent", "task": "x", "execution_id": child, "tokens_in": 1102, "tokens_out": 7961, "duration_ms": 84000, "status": "completed"}})
	if err := db.AppendEvent(zbotstore.Event{ID: "evt-done", SessionID: "sess-a", ExecutionID: child, Kind: "delegation_completed", Payload: string(dp)}); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	srv := New(nil, nil, nil, nil, nil, bus, db, nil, nil, nil, nil, "test")
	daemon := httptest.NewServer(srv.Routes())
	defer daemon.Close()
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = bus.Publish(context.Background(), domain.TimelineEvent{ID: "evt-live", SessionID: "sess-a", ExecutionID: root, Kind: domain.EventToolCall, Agent: "root", Text: "shell", ToolCall: &domain.ToolCallPayload{Tool: "shell"}})
		_ = bus.Publish(context.Background(), domain.TimelineEvent{ID: "evt-live-child", SessionID: "sess-a", ExecutionID: child, Kind: domain.EventDelegation, Agent: "research-agent", Text: "read", ToolCall: &domain.ToolCallPayload{Tool: "read"}})
	}()
	var sb strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, daemon.URL+"/api/sessions/sess-a/events", nil)
	res, err := daemon.Client().Do(req)
	if err == nil {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			sb.WriteString(sc.Text() + "\n")
		}
	} else if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("GET events: %v", err)
	}
	body := sb.String()
	for _, want := range []string{
		`"agent":"research-agent"`, `"execution_id":"` + child + `"`, // legacy row via the join
		`"agent":"root"`, `"execution_id":"` + root + `"`, // payload agent + the live root frame
		`"child_agent":"research-agent"`, `"tokens_in":1102`, `"status":"completed"`, // delegation payload
		"research-agent finished",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %v:\n%s", want, body)
		}
	}
}
