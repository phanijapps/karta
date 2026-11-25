package server

// The session SSE stream's frame helpers: the event shape, the
// durable-replay row → frame mapping (including the legacy
// intent_analysis → analysis_* kind translation), and the writer.
// Extracted from engine_handlers.go (LOC budget).

import (
	"encoding/json"
	"fmt"
	"net/http"

	"zbot/backend/internal/domain"
)

// sseEvent is one frame of the session stream.
type sseEvent struct {
	ID        string
	SessionID string
	Kind      string
	Text      string
	// Intent carries the analysis payload for the analysis_* kinds (both
	// replay and live paths).
	Intent *domain.IntentAnalysisPayload
	// Ward carries the ward transition payload for ward_entered /
	// ward_created events.
	Ward *domain.WardEventPayload
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

// analysisKindFor maps a persisted event's kind to the streamed kind: a
// legacy `intent_analysis` row becomes the terminal analysis kind its
// recorded status names, so pre-upgrade sessions keep rendering their
// analysis card.

func writeSSE(w http.ResponseWriter, e sseEvent) {
	body := map[string]any{
		"id": e.ID, "session_id": e.SessionID, "kind": e.Kind, "text": e.Text,
	}
	if e.Intent != nil {
		body["intent_analysis"] = e.Intent
	}
	if e.Ward != nil {
		body["ward"] = e.Ward
	}
	data, _ := json.Marshal(body)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, data)
}

func (s *Server) dbSessionEvents(sessionID, afterID string, limit int) ([]sseEvent, error) {
	storeEvents, err := s.db.Events(sessionID, afterID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]sseEvent, 0, len(storeEvents))
	for _, e := range storeEvents {
		var payload struct {
			Text           string                        `json:"text"`
			IntentAnalysis *domain.IntentAnalysisPayload `json:"intent_analysis"`
			Ward           *domain.WardEventPayload      `json:"ward"`
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
		ev := sseEvent{ID: e.ID, SessionID: e.SessionID, Kind: kind, Text: payload.Text, Ward: payload.Ward}
		if isAnalysisKind(domain.TimelineEventKind(kind)) {
			ev.Intent = payload.IntentAnalysis
		}
		out = append(out, ev)
	}
	return out, nil
}
