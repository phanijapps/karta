package chat

import (
	"context"
	"fmt"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// SetPersona wires the composed system prompt (static sections only —
// chat is never analyzed and never provisioned skills). An ERROR from
// fn FAILS the exchange before the model call: chat never runs on an
// accidentally empty prompt. Nil restores the pre-composition
// behavior (no persona message).
func (s *Service) SetPersona(fn func() (string, error)) { s.persona = fn }

// SetExtraTools mounts additional tool planes on every exchange (the
// ward plane: wards index + structural + file tools).
func (s *Service) SetExtraTools(fn func() []tools.Tool) { s.extraTools = fn }

// SetActiveWard wires the Active Ward section renderer (composed after
// the persona when the session hosts a ward).
func (s *Service) SetActiveWard(fn func() string) { s.activeWard = fn }

// SetContext wires the memory-recall seam: the lean exchange injects
// the recalled facts as a ## Context system message after the persona
// (the type gates the PIPELINE, never the knowledge — memory runs in
// both session types). Nil disables recall.
func (s *Service) SetContext(fn func(ctx context.Context, text string) []string) { s.recall = fn }

// staticProvider serves a fixed tool list (mounting extra planes like
// the ward tools without a registry).
type staticProvider struct{ list []tools.Tool }

// Tools returns the fixed list.
func (p staticProvider) Tools() []tools.Tool { return p.list }

// SetResearchPath wires the research-session execution route: the full
// engine pipeline (intent analysis, skills, wards, delegation) in the
// named session. Chat sessions always run the lean exchange; research
// sessions never do when this is wired. The persona is the SAME on
// both routes.
func (s *Service) SetResearchPath(fn func(ctx context.Context, sessionID, text, providerName, model string) SendOutcome) {
	s.researchPath = fn
}

// SendIn runs one serialized exchange in the named session (synchronous:
// no deltas). The session's TYPE routes: chat → the lean exchange,
// research → the engine pipeline when wired.
func (s *Service) SendIn(ctx context.Context, sessionID, text, providerName, model string) SendOutcome {
	sess, err := s.resolveSession(sessionID)
	if err != nil {
		return SendOutcome{Err: err}
	}
	if s.isResearch(sess) {
		return s.researchLocked(ctx, sess.ID, text, providerName, model)
	}
	return s.runExchange(ctx, sess, text, providerName, model, nil, nil)
}

// SendStreamIn runs the same serialized exchange, streaming volatile
// deltas (reasoning, then/interleaved content) to onDelta and to the
// bus (coalesced). Persistence semantics are IDENTICAL to SendIn: rows
// append in the same order, the assistant row persists once at
// completion with its reasoning, and durable-before-volatile holds on
// every emit. Research sessions complete as units (the engine pipeline
// is not a token stream): the delta callback never fires for them.
func (s *Service) SendStreamIn(ctx context.Context, sessionID, text, providerName, model string, onDelta func(Delta)) SendOutcome {
	sess, err := s.resolveSession(sessionID)
	if err != nil {
		return SendOutcome{Err: err}
	}
	if s.isResearch(sess) {
		return s.researchLocked(ctx, sess.ID, text, providerName, model)
	}
	sc, ok := s.completer.(StreamingCompleter)
	if !ok {
		// Degenerate but honest: the completer cannot stream.
		return s.runExchange(ctx, sess, text, providerName, model, nil, nil)
	}
	coal := newDeltaCoalescer(s, sess.ID, onDelta)
	defer coal.close()
	complete := func(ctx context.Context, p providers.Provider, token string, messages []providers.ChatMessage, offered []providers.ChatToolDef) (providers.ChatResponse, error) {
		return sc.ChatCompletionStream(ctx, p, model, token, messages, offered, func(d providers.ChatStreamDelta) {
			coal.push(Delta{Kind: string(d.Kind), Text: d.Text})
		})
	}
	return s.runExchange(ctx, sess, text, providerName, model, complete, coal.flush)
}

// SessionBusy reports whether a research run is live in the session
// (the SSE handler probes this before opening a stream, so the typed
// 409 can fire pre-stream; a race window remains — a send that loses
// it surfaces the same error in the terminal exchange).
func (s *Service) SessionBusy(sessionID string) bool {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	return s.busy[sessionID]
}

// researchLocked runs the research path under the session's busy
// guard: ONE live engine run per research session (overlapping sends
// would interleave transcripts and orphan cancellation ownership — the
// engine's documented overlap behavior). Overlaps fail typed
// ErrSessionBusy; the guard releases when the run drains.
func (s *Service) researchLocked(ctx context.Context, sessionID, text, providerName, model string) SendOutcome {
	s.busyMu.Lock()
	if s.busy == nil {
		s.busy = map[string]bool{}
	}
	if s.busy[sessionID] {
		s.busyMu.Unlock()
		return SendOutcome{Err: fmt.Errorf("%w: %s", ErrSessionBusy, sessionID)}
	}
	s.busy[sessionID] = true
	s.busyMu.Unlock()
	defer func() {
		s.busyMu.Lock()
		delete(s.busy, sessionID)
		s.busyMu.Unlock()
	}()
	return s.researchPath(ctx, sessionID, text, providerName, model)
}

// isResearch reports whether the exchange should run the research
// pipeline: the session is typed research AND the path is wired
// (unwired falls back to the lean exchange — wiring-incomplete tests).
func (s *Service) isResearch(sess *store.Session) bool {
	return domain.NormalizeSessionType(sess.Type) == domain.SessionTypeResearch && s.researchPath != nil
}

// resolveSession loads the session row for routing, creating the
// default quick-chat on first use. Unknown ids are typed errors
// (ErrUnknownSession); the server maps those to 404.
func (s *Service) resolveSession(sessionID string) (*store.Session, error) {
	if sessionID == "" {
		sessionID = DefaultSessionID
	}
	sess, err := s.store.Session(sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		if sessionID != DefaultSessionID {
			return nil, fmt.Errorf("%w: %s", ErrUnknownSession, sessionID)
		}
		if _, err := s.store.EnsureSession(sessionID, string(domain.SessionTypeChat)); err != nil {
			return nil, err
		}
		sess, err = s.store.Session(sessionID)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			return nil, fmt.Errorf("chat: default session vanished after ensure")
		}
	}
	return sess, nil
}

// streamCompleteFn bridges the shared exchange loop to a completion call;
// nil means the synchronous completer. Streaming implementations return the
// assembled response (the provider client assembles fragments).
type streamCompleteFn func(ctx context.Context, p providers.Provider, token string, messages []providers.ChatMessage, offered []providers.ChatToolDef) (providers.ChatResponse, error)
