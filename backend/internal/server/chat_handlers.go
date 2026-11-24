package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/providers"
	apigen "zbot/backend/internal/server/apigen"
	"zbot/backend/internal/store"
)

// sessionIDOf dereferences the optional session id (empty = default).
func sessionIDOf(id *apigen.Id) string {
	if id == nil || *id == "" {
		return chat.DefaultSessionID
	}
	return string(*id)
}

// SyncChatMessages pages one session's transcript. next_cursor is the
// last DELIVERED seq (a page cursor, never the database tail).
func (s *Server) SyncChatMessages(w http.ResponseWriter, r *http.Request, params apigen.SyncChatMessagesParams) {
	afterSeq := int64(0)
	if params.AfterSeq != nil {
		afterSeq = int64(*params.AfterSeq)
	}
	limit := 100
	if params.Limit != nil {
		limit = int(*params.Limit)
	}
	if afterSeq < 0 || limit < 1 || limit > 500 {
		writeError(w, http.StatusBadRequest, "invalid_request", "after_seq must be >= 0; limit must be 1-500")
		return
	}
	rows, next, truncated, err := s.chat.SyncIn(sessionIDOf(params.SessionId), afterSeq, limit)
	if err != nil {
		if errors.Is(err, chat.ErrUnknownSession) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]apigen.ChatMessage, 0, len(rows))
	for _, m := range rows {
		out = append(out, chatView(m))
	}
	writeJSON(w, http.StatusOK, apigen.ChatSync{
		Messages: out, NextCursor: int(next), Truncated: truncated,
	})
}

// SendChatMessage runs one serialized exchange and returns every row it
// appended (failures return the persisted user row + cursor).
func (s *Server) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	var req apigen.ChatSend
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "text must not be empty")
		return
	}
	providerName, modelID, found := strings.Cut(req.Model, "/")
	if !found || providerName == "" || modelID == "" {
		writeError(w, http.StatusBadRequest, "model_required", "select a model in the header picker first")
		return
	}

	outcome := s.chat.SendIn(r.Context(), sessionIDOf(req.SessionId), req.Text, providerName, modelID)
	if errors.Is(outcome.Err, chat.ErrUnknownSession) {
		writeError(w, http.StatusNotFound, "not_found", outcome.Err.Error())
		return
	}
	if errors.Is(outcome.Err, chat.ErrSessionBusy) {
		writeError(w, http.StatusConflict, "session_busy", outcome.Err.Error())
		return
	}
	// Persist the model as the default (last-used wins).
	if s.db != nil {
		_ = s.db.SetDefaultModel(req.Model)
	}
	if outcome.Err != nil {
		body := apigen.ChatExchange{
			Messages:  chatViews(outcome.Rows),
			EndCursor: int(outcome.EndCursor),
			Error:     chatErrorView(outcome.Err),
		}
		writeJSON(w, http.StatusBadGateway, body)
		return
	}
	writeJSON(w, http.StatusOK, apigen.ChatExchange{
		Messages: chatViews(outcome.Rows), EndCursor: int(outcome.EndCursor),
	})
}

// StreamChatMessage is the streamed twin of SendChatMessage (contract
// POST /api/chat/send): chat_delta events (reasoning, then/interleaved
// content) stream as the model works, and exactly one terminal
// chat_exchange event — the same shape as the synchronous response —
// closes the stream. Deltas are volatile; the terminal exchange is the
// truth. Invalid bodies/models fail with the synchronous endpoint's typed
// 4xx JSON before any stream opens.
func (s *Server) StreamChatMessage(w http.ResponseWriter, r *http.Request) {
	var req apigen.ChatSend
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "text must not be empty")
		return
	}
	providerName, modelID, found := strings.Cut(req.Model, "/")
	if !found || providerName == "" || modelID == "" {
		writeError(w, http.StatusBadRequest, "model_required", "select a model in the header picker first")
		return
	}

	// Unknown sessions fail with the synchronous endpoint's typed 404
	// before any stream opens (contract: invalid references fail first).
	sessionID := sessionIDOf(req.SessionId)
	if sessionID != chat.DefaultSessionID {
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "session store not wired")
			return
		}
		sess, err := s.db.Session(sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not_found", "no such session: "+sessionID)
			return
		}
	}
	if s.chat.SessionBusy(sessionID) {
		writeError(w, http.StatusConflict, "session_busy", "a research run is already live in this session — extend it with continue instead")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Bounded delivery: a writer goroutine owns ALL response writes under
	// a per-write deadline; the exchange NEVER blocks on the client.
	sw := newStreamWriter(w, flusher)
	defer sw.close()

	// A disconnecting client must not kill the exchange: the run continues
	// under its own context, durable events serve every other client, and
	// writes to the dead connection fail their deadline and drop.
	runCtx := context.WithoutCancel(r.Context())
	// The SERVICE routes by the session's type: chat sessions stream
	// deltas; research sessions complete as units (the engine pipeline
	// is not a token stream — the delta callback never fires).
	outcome := s.chat.SendStreamIn(runCtx, sessionID, req.Text, providerName, modelID, func(d chat.Delta) {
		kind := apigen.Content
		if d.Kind == string(apigen.Reasoning) {
			kind = apigen.Reasoning
		}
		sw.send(sseMessage{event: "chat_delta", payload: apigen.ChatDelta{Kind: kind, Text: d.Text}})
	})
	if s.db != nil {
		_ = s.db.SetDefaultModel(req.Model)
	}

	body := apigen.ChatExchange{
		Messages:  chatViews(outcome.Rows),
		EndCursor: int(outcome.EndCursor),
	}
	if outcome.Err != nil {
		body.Error = chatErrorView(outcome.Err)
	}
	sw.finish(sseMessage{event: "chat_exchange", payload: body, terminal: true})
}

// sseMessage is one SSE frame to write; terminal marks the final frame.
type sseMessage struct {
	event    string
	payload  any
	terminal bool
}

// streamWriter decouples exchange execution from client write speed.
// Delivery and lifetime contract:
//
//   - PLATFORM BOUNDARY (documented, not fixable at this layer): if a
//     write actually EXCEEDS its deadline, net/http permanently poisons
//     the response (the bufio.Writer under http.response caches the
//     error; see go/src/net/http/server.go response.write and
//     responsecontroller.go "a write deadline exceeded ... will not
//     extend"). No SSE implementation on net/http can deliver through a
//     response after a timed-out write. The application-level mitigation
//     is deadline GENEROSITY: 10s per write, so transient client stalls
//     recover within the deadline (no failed write, no poisoning); the
//     strike system then handles sustained slowness within the budget.
//

//		goroutine owns ALL writes. Each write gets its own fresh 10s
//		deadline — so a SLOW GENERATION never consumes budget (idle gaps
//		cost nothing; only actual write time counts), while a STALLED
//		client accrues penalties: three writes slower than 1s declare the
//		connection dead, after which the goroutine only drains. Deadlines
//		bound every write, so close() JOINS the goroutine strictly — the
//		ResponseWriter is never used after the handler returns. Delta
//		frames are volatile (send is non-blocking, drops on full); the
//		terminal frame is a post-exchange blocking enqueue that always
//		lands (a healthy writer drains the queue; a dead one drains
//		without writing).
//	  - Sync mode (ResponseController without deadline support — wrapped
//	    writers only; real server connections always support it): no
//	    goroutine at all. Delta frames are dropped (an unbounded goroutine
//	    write cannot be made lifetime-safe without deadlines); the
//	    terminal frame is written synchronously from the handler's own
//	    goroutine after the exchange (blocking is safe: the exchange is
//	    done and the handler still owns the ResponseWriter). Lifetime is
//	    strict in both modes.
type streamWriter struct {
	w          http.ResponseWriter
	flusher    http.Flusher
	rc         *http.ResponseController
	ch         chan sseMessage
	done       chan struct{}
	dead       atomic.Bool
	strikes    int
	prevStrike time.Time // strikes decay: only sustained slowness kills
	deadlineOK bool
}

const (
	streamWriterBuffer  = 64
	slowWriteThreshold  = time.Second
	slowWritePenaltyCap = 3
	slowStrikeWindow    = 10 * time.Second
)

// streamWriteDeadline is the per-write recovery window (transient client
// stalls recover within it; see the PLATFORM BOUNDARY note above). A var,
// not a const, solely so the gap test can prove disarm-across-idle
// behavior with a short window instead of sleeping 10s.
var streamWriteDeadline = 10 * time.Second

func newStreamWriter(w http.ResponseWriter, f http.Flusher) *streamWriter {
	sw := &streamWriter{
		w: w, flusher: f, rc: http.NewResponseController(w),
		ch: make(chan sseMessage, streamWriterBuffer), done: make(chan struct{}),
	}
	// Probe deadline support with a FUTURE deadline, then clear it — an
	// expired deadline is never armed. Writes arm fresh and disarm after
	// (see write), so no deadline is ever armed across idle gaps.
	sw.deadlineOK = sw.rc.SetWriteDeadline(time.Now().Add(time.Hour)) == nil
	if sw.deadlineOK && sw.rc.SetWriteDeadline(time.Time{}) != nil {
		sw.deadlineOK = false // cannot disarm the probe: refuse deadline mode
	}
	if sw.deadlineOK {
		go sw.loop()
	}
	return sw
}

func (sw *streamWriter) write(msg sseMessage) {
	if sw.dead.Load() && !msg.terminal {
		return // stalled stream: deltas stop, the terminal still gets one attempt
	}
	// Arm a fresh deadline immediately before the write, and DISARM after
	// — no deadline is ever armed across an idle generation gap, and the
	// flush counts toward the write's stall accounting. A failed arm or
	// disarm voids the bounded-write guarantee: declare the stream dead
	// (writes stop; the goroutine stays joinable).
	if sw.rc != nil && sw.deadlineOK {
		if sw.rc.SetWriteDeadline(time.Now().Add(streamWriteDeadline)) != nil {
			sw.dead.Store(true)
			return
		}
	}
	start := time.Now()
	data, err := json.Marshal(msg.payload)
	if err == nil {
		_, err = fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", msg.event, data)
	}
	if err == nil {
		sw.flusher.Flush() // inside the timed section: slow flushes strike too
	}
	if sw.rc != nil && sw.deadlineOK {
		if sw.rc.SetWriteDeadline(time.Time{}) != nil {
			sw.dead.Store(true) // cannot disarm: next write may spuriously fail
			return
		}
	}
	took := time.Since(start)
	if err != nil {
		sw.dead.Store(true)
		return
	}
	if took > slowWriteThreshold {
		// Strikes DECAY: an isolated slow write (GC pause, wifi hiccup) is
		// forgiven — only sustained slowness (cap strikes inside the
		// window) declares the client stalled.
		now := time.Now()
		if now.Sub(sw.prevStrike) > slowStrikeWindow {
			sw.strikes = 0
		}
		sw.prevStrike = now
		sw.strikes++
		if sw.strikes >= slowWritePenaltyCap {
			sw.dead.Store(true)
		}
	}
}

func (sw *streamWriter) loop() {
	defer close(sw.done)
	for msg := range sw.ch {
		sw.write(msg)
	}
}

// send enqueues one VOLATILE frame, dropping when the writer cannot keep
// up; sync mode drops by design (see the contract comment). It never
// blocks the caller — the exchange must not stall on the client.
func (sw *streamWriter) send(msg sseMessage) {
	if !sw.deadlineOK {
		return
	}
	select {
	case sw.ch <- msg:
	default:
	}
}

// finish delivers the TERMINAL frame after the exchange completed.
// Deadline mode: blocking enqueue (safe post-exchange) — it lands when
// the writer drains the queue, or immediately once dead-and-draining.
// Sync mode: the handler's own goroutine writes it directly.
func (sw *streamWriter) finish(msg sseMessage) {
	if !sw.deadlineOK {
		sw.write(msg)
		return
	}
	select {
	case sw.ch <- msg:
	case <-sw.done:
	}
}

// close joins the writer goroutine (deadline mode: strict — every write
// is deadline-bounded; sync mode: nothing to join).
func (sw *streamWriter) close() {
	if !sw.deadlineOK {
		return
	}
	close(sw.ch)
	<-sw.done
}

func chatViews(rows []store.Message) []apigen.ChatMessage {
	out := make([]apigen.ChatMessage, 0, len(rows))
	for _, m := range rows {
		out = append(out, chatView(m))
	}
	return out
}

func chatView(m store.Message) apigen.ChatMessage {
	view := apigen.ChatMessage{
		Id: m.ID, Seq: int(m.Seq), Role: apigen.ChatMessageRole(m.Role),
		Content: m.Content, CreatedAt: m.CreatedAt,
		ToolCallId: nilStrPtr(m.ToolCallID), ToolName: nilStrPtr(m.ToolName),
		Provider: nilStrPtr(m.Provider), Model: nilStrPtr(m.Model),
	}
	if m.Reasoning != "" {
		view.Reasoning = &m.Reasoning
	}
	if m.ToolCalls != "" {
		view.ToolCalls = &m.ToolCalls
	}
	if m.PromptTokens.Valid {
		v := int(m.PromptTokens.Int64)
		view.PromptTokens = &v
	}
	if m.CompletionTokens.Valid {
		v := int(m.CompletionTokens.Int64)
		view.CompletionTokens = &v
	}
	return view
}

func chatErrorView(err error) *apigen.ProviderError {
	reason := apigen.ProviderFailureReason(providers.ReasonOf(err))
	if reason == "" {
		if errors.Is(err, providers.ErrKeyringUnavailable) {
			reason = apigen.ProviderFailureReason("credential_unavailable")
		} else {
			reason = apigen.ProviderFailureReason("unreachable")
		}
	}
	return &apigen.ProviderError{Code: "provider_failure", Message: err.Error(), Reason: reason}
}

func nilStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GetDefaultModel returns the stored default model for the chat.
func (s *Server) GetDefaultModel(w http.ResponseWriter, r *http.Request) {
	model, err := s.db.DefaultModel()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
}

// ClearChat wipes one session's transcript (user-initiated,
// destructive). The session row itself survives — the quick chat is
// never deleted.
func (s *Server) ClearChat(w http.ResponseWriter, r *http.Request, params apigen.ClearChatParams) {
	sessionID := sessionIDOf(params.SessionId)
	if sessionID != chat.DefaultSessionID {
		sess, err := s.db.Session(sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "not_found", "no such session")
			return
		}
	}
	if err := s.db.ClearSession(sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
