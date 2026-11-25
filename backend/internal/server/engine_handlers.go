package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"zbot/backend/internal/domain"
	apigen "zbot/backend/internal/server/apigen"
	zbotstore "zbot/backend/internal/store"
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

// sessionCounter stamps unique research-session ids.
var sessionCounter atomic.Uint64

// sessionStatus derives the contract status from the row: a session
// with no messages yet is `created` (a fresh shell); anything with a
// transcript is `completed` (live-run liveness arrives with run-state
// surfacing — the Continue endpoint already answers 409 when idle).
func sessionStatus(hasMessages bool) apigen.SessionStatus {
	if hasMessages {
		return apigen.SessionStatusCompleted
	}
	return apigen.SessionStatusCreated
}

func parseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// refreshActiveRuns re-reads the live-run set (call before listing).
func (s *Server) refreshActiveRuns() {
	if s.db == nil {
		return
	}
	runs, err := s.db.ActiveRuns()
	if err == nil {
		s.activeRuns = runs
	}
}

// ListSessions returns bounded, explicitly truncated results — the
// session model's roster (type + ward binding drive the sidebar's
// Chat / Ward Bots / Research groups), most recently active first.
func (s *Server) ListSessions(w http.ResponseWriter, r *http.Request, params apigen.ListSessionsParams) {
	limit := 50
	if params.Limit != nil {
		limit = int(*params.Limit)
	}
	if limit < 1 || limit > 200 {
		writeError(w, http.StatusBadRequest, "invalid_request", "limit must be 1-200")
		return
	}
	offset := 0
	if params.Cursor != nil && *params.Cursor != "" {
		o, err := strconv.Atoi(string(*params.Cursor))
		if err != nil || o < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid cursor")
			return
		}
		offset = o
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "session store not wired")
		return
	}
	s.refreshActiveRuns()
	// Fetch one extra row to detect truncation without a count query.
	infos, err := s.db.ListSessions(offset + limit + 1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	truncated := len(infos) > offset+limit
	if offset+limit < len(infos) {
		infos = infos[:offset+limit]
	}
	if offset > len(infos) {
		infos = nil
	} else {
		infos = infos[offset:]
	}
	out := make([]apigen.SessionSummary, 0, len(infos))
	for _, info := range infos {
		summary := apigen.SessionSummary{
			Id: apigen.Id(info.ID), Type: apigen.SessionSummaryType(info.Type),
			Status: sessionStatus(info.HasMessages),
			Title:  nilStrPtr(info.Title), Ward: nilStrPtr(info.Ward),
			UpdatedAt: parseTime(info.UpdatedAt),
		}
		if s.activeRuns != nil && s.activeRuns[info.ID] {
			r := apigen.SessionSummaryRun("running")
			summary.Run = &r
		}
		out = append(out, summary)
	}
	body := apigen.SessionList{Sessions: out, Truncated: truncated}
	if truncated {
		next := strconv.Itoa(offset + limit)
		body.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, body)
}

// CreateSession creates a research session — a shell (type research,
// no ward binding) ready for its first exchange.
func (s *Server) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req apigen.CreateSessionRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			return
		}
	}
	title := ""
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		if len(title) > 256 {
			writeError(w, http.StatusBadRequest, "invalid_request", "title must be at most 256 characters")
			return
		}
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "session store not wired")
		return
	}
	id := fmt.Sprintf("research-%d-%d", time.Now().UnixNano(), sessionCounter.Add(1))
	if _, err := s.db.EnsureSession(id, string(domain.SessionTypeResearch)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if title != "" {
		if err := s.db.SetSessionTitle(id, title); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	sess, err := s.db.Session(id)
	if err != nil || sess == nil {
		writeError(w, http.StatusInternalServerError, "internal", "session vanished after create")
		return
	}
	writeJSON(w, http.StatusCreated, s.sessionView(sess, false, time.Time{}))
}

// sessionView maps a store row to the contract Session shape.
// updatedAt carries the session's recency hint (latest message time,
// falling back to creation) when the caller has one.
func (s *Server) sessionView(sess *zbotstore.Session, hasMessages bool, updatedAt time.Time) apigen.Session {
	if updatedAt.IsZero() {
		updatedAt = sess.CreatedAt
	}
	view := apigen.Session{
		Id: apigen.Id(sess.ID), Type: apigen.SessionType(sess.Type),
		Status: sessionStatus(hasMessages),
		Title:  nilStrPtr(sess.Title), Ward: nilStrPtr(sess.Ward),
		CreatedAt: sess.CreatedAt, UpdatedAt: updatedAt,
	}
	if s.activeRuns != nil && s.activeRuns[sess.ID] {
		r := apigen.SessionRun("running")
		view.Run = &r
	}
	return view
}

// GetSession returns identity and status.
func (s *Server) GetSession(w http.ResponseWriter, r *http.Request, sessionId apigen.SessionId) {
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "session store not wired")
		return
	}
	sess, err := s.db.Session(string(sessionId))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such session")
		return
	}
	latest, err := s.db.LatestSeq(string(sessionId))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	updatedRaw, err := s.db.SessionUpdated(string(sessionId))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.sessionView(sess, latest > 0, parseTime(updatedRaw)))
}

// LeaveWard clears the session's ward binding (the session becomes a
// research shell again; the engine host drops the ward).
func (s *Server) LeaveWard(w http.ResponseWriter, r *http.Request, sessionId apigen.SessionId) {
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "session store not wired")
		return
	}
	sid := string(sessionId)
	if sid == "default" {
		writeError(w, http.StatusConflict, "not_bound", "the quick chat has no ward binding")
		return
	}
	sess, err := s.db.Session(sid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such session")
		return
	}
	if err := s.db.SetSessionWard(sid, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if s.wardDropper != nil {
		s.wardDropper(sid)
	}
	w.WriteHeader(http.StatusNoContent)
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
			live := sseEvent{ID: evt.ID, SessionID: evt.SessionID, Kind: string(evt.Kind), Text: evt.Text, Ward: evt.Ward}
			if isAnalysisKind(evt.Kind) {
				live.Intent = evt.IntentAnalysis
			}
			writeSSE(w, live)
			flusher.Flush()
		}
	}
}

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

// legacyAnalysisKind picks the terminal analysis kind for a legacy
// intent_analysis row from its status (a missing status reads as
// completed — the row existed, so an analysis was recorded).
func legacyAnalysisKind(p *domain.IntentAnalysisPayload) string {
	if p == nil {
		return string(domain.EventAnalysisCompleted)
	}
	switch p.Status {
	case domain.IntentStatusBypassed:
		return string(domain.EventAnalysisBypassed)
	case domain.IntentStatusFailed:
		return string(domain.EventAnalysisFailed)
	default:
		return string(domain.EventAnalysisCompleted)
	}
}

// GetSessionState reads the session row plus its LATEST intent
// analysis (the state surface: the UI's Task Analysis card and the
// context panel render from this — never transcript parsing).
func (s *Server) GetSessionState(w http.ResponseWriter, r *http.Request, sessionId apigen.SessionId) {
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "session store not wired")
		return
	}
	sid := string(sessionId)
	sess, err := s.db.Session(sid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such session")
		return
	}
	latest, err := s.db.LatestSeq(sid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	state := apigen.SessionState{Session: s.sessionView(sess, latest > 0, time.Time{})}
	// The three terminal kinds plus the legacy pre-upgrade kind: an
	// upgraded home's last pre-upgrade analysis still reads as state.
	if ev, err := s.db.LatestEventOfKind(sid, string(domain.EventAnalysisCompleted), string(domain.EventAnalysisBypassed), string(domain.EventAnalysisFailed), legacyIntentAnalysisKind); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	} else if ev != nil {
		var persisted struct {
			IntentAnalysis *apigen.IntentAnalysisPayload `json:"intent_analysis"`
		}
		if err := json.Unmarshal([]byte(ev.Payload), &persisted); err == nil && persisted.IntentAnalysis != nil {
			state.IntentAnalysis = persisted.IntentAnalysis
		}
	}
	writeJSON(w, http.StatusOK, state)
}
