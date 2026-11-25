package server

// The session SSE stream's frame helpers: the event shape, the
// durable-replay row → frame mapping (including the legacy
// intent_analysis → analysis_* kind translation), and the writer.
// Extracted from engine_handlers.go (LOC budget).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"zbot/backend/internal/domain"
)

// sseEvent is one frame of the session stream.
type sseEvent struct {
	ID        string
	SessionID string
	Kind      string
	Text      string
	// Timestamp is the event's time (the row's created_at on replay,
	// the event's own time live) — the contract requires it.
	Timestamp time.Time
	// Agent + ExecutionID attribute every frame (replayed or live) to the
	// acting agent and its execution — run-legibility.
	Agent       string
	ExecutionID string
	// Intent carries the analysis payload for the analysis_* kinds (both
	// replay and live paths).
	Intent *domain.IntentAnalysisPayload
	// Ward carries the ward transition payload for ward_entered /
	// ward_created events.
	Ward *domain.WardEventPayload
	// Delegation carries the lifecycle payload for delegation_started /
	// delegation_completed.
	Delegation *domain.DelegationPayload
}

// legacyIntentAnalysisKind is the pre-intent-analyst-agent event kind;
// durable rows still carry it.
const legacyIntentAnalysisKind = "intent_analysis"

// isAnalysisKind reports whether a timeline kind belongs to the
// analysis trail (started + the three terminal kinds).
func isAnalysisKind(kind domain.TimelineEventKind) bool {
	switch kind {
	case domain.EventAnalysisStarted, domain.EventAnalysisCompleted, domain.EventAnalysisBypassed, domain.EventAnalysisFailed:
		return true
	}
	return false
}

// writeSSE emits one SSE frame (the kind is the event name).
func writeSSE(w http.ResponseWriter, e sseEvent) {
	body := map[string]any{
		"id": e.ID, "session_id": e.SessionID, "kind": e.Kind, "text": e.Text,
	}
	if !e.Timestamp.IsZero() {
		body["timestamp"] = e.Timestamp
	}
	if e.Agent != "" {
		body["agent"] = e.Agent
	}
	if e.ExecutionID != "" {
		body["execution_id"] = e.ExecutionID
	}
	if e.Intent != nil {
		body["intent_analysis"] = e.Intent
	}
	if e.Delegation != nil {
		body["delegation"] = e.Delegation
	}
	if e.Ward != nil {
		body["ward"] = e.Ward
	}
	data, _ := json.Marshal(body)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, data)
}

// dbSessionEvents maps persisted rows to frames: a legacy
// `intent_analysis` row becomes the terminal analysis kind its recorded
// status names, so pre-upgrade sessions keep rendering their analysis
// card.
func (s *Server) dbSessionEvents(sessionID, afterID string, limit int) ([]sseEvent, error) {
	storeEvents, err := s.db.Events(sessionID, afterID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]sseEvent, 0, len(storeEvents))
	for _, e := range storeEvents {
		var payload struct {
			Text           string                        `json:"text"`
			Agent          string                        `json:"agent"`
			IntentAnalysis *domain.IntentAnalysisPayload `json:"intent_analysis"`
			Ward           *domain.WardEventPayload      `json:"ward"`
			Delegation     *domain.DelegationPayload     `json:"delegation"`
		}
		_ = json.Unmarshal([]byte(e.Payload), &payload)
		kind := e.Kind
		if e.Kind == legacyIntentAnalysisKind {
			// legacy/flat storage: the payload itself may be the analysis JSON
			if payload.IntentAnalysis == nil {
				var p domain.IntentAnalysisPayload
				if json.Unmarshal([]byte(e.Payload), &p) == nil {
					payload.IntentAnalysis = &p
				}
			}
			kind = legacyAnalysisKind(payload.IntentAnalysis)
		}
		// Attribution: the payload's agent when it carries one, else the
		// execution's agent from the join (rows that predate it).
		agent := payload.Agent
		if agent == "" {
			agent = e.Agent
		}
		ts, _ := time.Parse(time.RFC3339Nano, e.CreatedAt)
		ev := sseEvent{ID: e.ID, SessionID: e.SessionID, Kind: kind, Text: payload.Text, Ward: payload.Ward, Agent: agent, ExecutionID: e.ExecutionID, Delegation: payload.Delegation, Timestamp: ts}
		if isAnalysisKind(domain.TimelineEventKind(kind)) {
			ev.Intent = payload.IntentAnalysis
		}
		out = append(out, ev)
	}
	return out, nil
}
