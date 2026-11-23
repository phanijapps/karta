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

// The intent_analysis event's payload survives the durable replay and
// live SSE paths (phase-3 gate F2): complexity, approach, ward, and
// drops reach clients, not just text.
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
	}
	payload, _ := json.Marshal(map[string]any{"text": "Task Analysis: do-research", "intent_analysis": analysis})
	if err := db.AppendEvent(zbotstore.Event{
		ID: "evt-i1", SessionID: "sess-1", Kind: string(domain.EventIntentAnalysis), Payload: string(payload),
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
	for _, want := range []string{"intent_analysis", "do-research", "\"L\"", "16", "stock-analysis", "phantom-skill"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %v:\n%s", want, body)
		}
	}
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type: %s", res.Header.Get("Content-Type"))
	}
}
