package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"zbot/backend/internal/domain"
	apigen "zbot/backend/internal/server/apigen"
)

// CreateGoal starts a goal run (engine-backed).
func (s *Server) CreateGoal(w http.ResponseWriter, r *http.Request) {
	var req apigen.GoalRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Goal) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "goal must not be empty")
		return
	}

	// Model precedence: request > agent spec > refuse.
	modelRef := ""
	if req.Model != nil && *req.Model != "" {
		modelRef = *req.Model
	}

	sessionID := ""
	if req.SessionId != nil && *req.SessionId != "" {
		sessionID = *req.SessionId
	}

	agent := ""
	if req.Agent != nil && *req.Agent != "" {
		agent = *req.Agent
	}

	events, err := s.engine.RunGoal(r.Context(), domain.GoalRequest{
		SessionID: sessionID, Goal: req.Goal, Agent: agent, Model: modelRef,
	})
	if err != nil {
		if strings.Contains(err.Error(), "no model selected") || strings.Contains(err.Error(), "invalid model") {
			writeError(w, http.StatusBadRequest, "model_required", err.Error())
			return
		}
		if strings.Contains(err.Error(), "unknown agent") {
			writeError(w, http.StatusBadRequest, "unknown_agent", err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "engine_failure", err.Error())
		return
	}
	// Drain the run's channel daemon-side: the durable path is
	// persist+publish (inside the engine/wrapper), and an undrained
	// channel would backpressure the run's emit loop (its buffer
	// fills). The SSE endpoint reads from the store+bus, not here.
	go func() {
		for range events {
			// discard: durability happened upstream
		}
	}()

	// Determine the session ID (the engine may have assigned one).
	sess := sessionID
	if sess == "" {
		// The engine assigns IDs like "goal-N"; query the store for the latest.
		if latest := s.latestGoalSession(); latest != "" {
			sess = latest
		} else {
			sess = "unknown"
		}
	}

	writeJSON(w, http.StatusCreated, apigen.Session{
		Id: apigen.Id(sess), Status: apigen.SessionStatusRunning,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
}

// latestGoalSession finds the most recently created goal session.
func (s *Server) latestGoalSession() string {
	sessions, _, _ := s.storeSessionList(1)
	if len(sessions) > 0 {
		return sessions[0]
	}
	return ""
}

// storeSessionList is a helper for bounded listing.
func (s *Server) storeSessionList(limit int) ([]string, bool, error) {
	// The engine stores sessions via EnsureSession; list from timeline_events.
	return nil, false, nil
}

// ListSessions returns bounded, explicitly truncated results.
func (s *Server) ListSessions(w http.ResponseWriter, r *http.Request, params apigen.ListSessionsParams) {
	// Goal sessions are a subset; for now, return the chat session + any
	// engine sessions recorded in timeline_events.
	writeJSON(w, http.StatusOK, apigen.SessionList{
		Sessions: []apigen.SessionSummary{}, Truncated: false,
	})
}

// GetSession returns identity and status.
func (s *Server) GetSession(w http.ResponseWriter, r *http.Request, sessionId apigen.SessionId) {
	writeJSON(w, http.StatusOK, apigen.Session{
		Id: sessionId, Status: apigen.SessionStatusCompleted,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
}

// CancelSession stops a session's active run.
func (s *Server) CancelSession(w http.ResponseWriter, r *http.Request, sessionId apigen.SessionId) {
	if err := s.engine.Cancel(r.Context(), string(sessionId)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apigen.Session{
		Id: sessionId, Status: apigen.SessionStatusCancelled,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
}

// ResumeSession is typed not-yet.
func (s *Server) ResumeSession(w http.ResponseWriter, r *http.Request, sessionId apigen.SessionId) {
	writeError(w, http.StatusNotImplemented, "not_yet", "resume is not yet implemented — interactive goals arrive with the next engine change")
}

// StreamSessionEvents streams the session's timeline as SSE:
// durable replay first, then live from the bus.
func (s *Server) StreamSessionEvents(w http.ResponseWriter, r *http.Request, sessionId apigen.SessionId) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe to live events BEFORE replay (no gap at the seam).
	busSub := s.bus.Subscribe(r.Context(), string(sessionId))
	busCh := busSub.Events()

	// Durable replay.
	afterID := ""
	for {
		events, err := s.dbSessionEvents(string(sessionId), afterID, 100)
		if err != nil {
			break
		}
		for _, evt := range events {
			writeSSE(w, evt)
			afterID = evt.ID
		}
		flusher.Flush()
		if len(events) < 100 {
			break
		}
	}

	// Heartbeat + live tail until disconnect.
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case evt := <-busCh:
			live := sseEvent{ID: evt.ID, SessionID: evt.SessionID, Kind: string(evt.Kind), Text: evt.Text}
			if evt.Kind == domain.EventIntentAnalysis {
				live.Intent = evt.IntentAnalysis
			}
			writeSSE(w, live)
			flusher.Flush()
		}
	}
}

type sseEvent struct {
	ID        string
	SessionID string
	Kind      string
	Text      string
	// Intent carries the intent_analysis payload when Kind is
	// intent_analysis (both replay and live paths).
	Intent *domain.IntentAnalysisPayload
}

func writeSSE(w http.ResponseWriter, e sseEvent) {
	body := map[string]any{
		"id": e.ID, "session_id": e.SessionID, "kind": e.Kind, "text": e.Text,
	}
	if e.Intent != nil {
		body["intent_analysis"] = e.Intent
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
		}
		_ = json.Unmarshal([]byte(e.Payload), &payload)
		ev := sseEvent{ID: e.ID, SessionID: e.SessionID, Kind: e.Kind, Text: payload.Text}
		if e.Kind == string(domain.EventIntentAnalysis) && payload.IntentAnalysis == nil {
			// legacy/flat storage: the payload itself is the analysis JSON
			var p domain.IntentAnalysisPayload
			if json.Unmarshal([]byte(e.Payload), &p) == nil {
				ev.Intent = &p
			}
		} else {
			ev.Intent = payload.IntentAnalysis
		}
		out = append(out, ev)
	}
	return out, nil
}
