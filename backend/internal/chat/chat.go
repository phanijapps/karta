// Package chat implements the simple chat exchange: one serialized send
// per conversation that appends the user message, drives a bounded
// completion with the built-in tools against the selected provider
// model, persists every row (intent-before-execution), and emits
// durable-first timeline events. SendStream streams volatile deltas
// (reasoning/content) on the same exchange; Send is that exchange
// without deltas. No agents — the base
// exchange the engine layer later generalizes.
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// SessionID is the eternal conversation's fixed identity.
const SessionID = "default"

// MaxToolRounds bounds tool execution per send.
const MaxToolRounds = 2

// HistoryBound bounds the provider context window (rows).
const HistoryBound = 200

// Delta is one streamed chat fragment (volatile — never persisted).
type Delta struct {
	Kind string // "reasoning" | "content"
	Text string
}

// StreamingCompleter is the optional streaming seam: providers.ChatClient
// implements it; SendStream requires it.
type StreamingCompleter interface {
	ChatCompletionStream(ctx context.Context, p providers.Provider, model, token string, messages []providers.ChatMessage, tools []providers.ChatToolDef, onDelta func(providers.ChatStreamDelta)) (providers.ChatResponse, error)
}

// Completer is the seam over the providers chat client (tests inject
// scripted completions).
type Completer interface {
	ChatCompletion(ctx context.Context, p providers.Provider, model, token string, messages []providers.ChatMessage, toolDefs []providers.ChatToolDef) (providers.ChatResponse, error)
}

// CredentialResolver resolves a provider's request credential.
type CredentialResolver func(ctx context.Context, p providers.Provider) (string, error)

var idCounter uint64

// Service runs the chat exchange.
type Service struct {
	store     *store.Store
	provider  func(name string) (providers.Provider, error)
	resolve   CredentialResolver
	completer Completer
	toolProv  tools.Provider
	bus       *eventbus.Bus

	sendMu sync.Mutex // serializes whole exchanges (never holds a db tx over a provider call)

	idGen func() string
}

// New wires the chat service. idGen is injectable for deterministic ids.
// The provider+credential are resolved atomically when provStore is set.
func New(st *store.Store, providerLookup func(string) (providers.Provider, error), resolve CredentialResolver, completer Completer, toolProv tools.Provider, bus *eventbus.Bus, idGen func() string) *Service {
	if idGen == nil {
		idGen = func() string {
			return fmt.Sprintf("msg-%d-%06d", time.Now().UnixNano(), atomic.AddUint64(&idCounter, 1))
		}
	}
	return &Service{store: st, provider: providerLookup, resolve: resolve, completer: completer, toolProv: toolProv, bus: bus, idGen: idGen}
}

// SendOutcome is a send's result: the appended rows on success, or the
// typed failure with the persisted user row and cursor.
type SendOutcome struct {
	Rows      []store.Message
	EndCursor int64
	Err       error // typed providers failure or chat error
}

// Send runs one serialized exchange (synchronous: no deltas).
func (s *Service) Send(ctx context.Context, text, providerName, model string) SendOutcome {
	return s.runExchange(ctx, text, providerName, model, nil, nil)
}

// SendStream runs the same serialized exchange, streaming volatile deltas
// (reasoning, then/interleaved content) to onDelta and to the bus
// (coalesced). Persistence semantics are IDENTICAL to Send: rows append in
// the same order, the assistant row persists once at completion with its
// reasoning, and durable-before-volatile holds on every emit.
func (s *Service) SendStream(ctx context.Context, text, providerName, model string, onDelta func(Delta)) SendOutcome {
	sc, ok := s.completer.(StreamingCompleter)
	if !ok {
		// Degenerate but honest: the completer cannot stream.
		return s.runExchange(ctx, text, providerName, model, nil, nil)
	}
	coal := newDeltaCoalescer(s, onDelta)
	defer coal.close()
	complete := func(ctx context.Context, p providers.Provider, token string, messages []providers.ChatMessage, offered []providers.ChatToolDef) (providers.ChatResponse, error) {
		return sc.ChatCompletionStream(ctx, p, model, token, messages, offered, func(d providers.ChatStreamDelta) {
			coal.push(Delta{Kind: string(d.Kind), Text: d.Text})
		})
	}
	return s.runExchange(ctx, text, providerName, model, complete, coal.flush)
}

// streamCompleteFn bridges the shared exchange loop to a completion call;
// nil means the synchronous completer. Streaming implementations return the
// assembled response (the provider client assembles fragments).
type streamCompleteFn func(ctx context.Context, p providers.Provider, token string, messages []providers.ChatMessage, offered []providers.ChatToolDef) (providers.ChatResponse, error)

// runExchange is the one exchange implementation shared by Send and
// SendStream (no drift between the paths; both are pinned by tests).
func (s *Service) runExchange(ctx context.Context, text, providerName, model string, stream streamCompleteFn, onBoundary func()) SendOutcome {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	root, err := s.store.EnsureSession(SessionID)
	if err != nil {
		return SendOutcome{Err: err}
	}

	// 1. Persist the user message before any network call.
	user, err := s.store.Append(store.Message{ID: s.idGen(), SessionID: SessionID, ExecutionID: root, Role: "user", Content: text})
	if err != nil {
		return SendOutcome{Err: err}
	}
	s.emitEvent(user, root, "")
	outcome := SendOutcome{Rows: []store.Message{user}}
	fail := func(err error) SendOutcome {
		outcome.Err = err
		outcome.EndCursor = user.Seq
		return outcome
	}

	p, err := s.provider(providerName)
	if err != nil {
		return fail(err)
	}
	token, err := s.resolve(ctx, p)
	if err != nil {
		return fail(err)
	}

	// 2. Compaction-aware, exchange-complete history window.
	hw, err := s.store.HistoryWindow(SessionID, "", HistoryBound)
	if err != nil {
		return fail(err)
	}
	messages := windowToChatMessages(hw)
	var usage providers.ChatUsage

	complete := func(ctx context.Context, messages []providers.ChatMessage, offered []providers.ChatToolDef) (providers.ChatResponse, error) {
		if stream != nil {
			res, err := stream(ctx, p, token, messages, offered)
			// Exchange boundary: flush volatile deltas BEFORE the durable
			// completion event publishes and sendMu releases, so observers
			// never see typing after completion (nor stale fragments
			// interleaved into the next send).
			if onBoundary != nil {
				onBoundary()
			}
			return res, err
		}
		return s.completer.ChatCompletion(ctx, p, model, token, messages, offered)
	}

	// 3. Bounded tool rounds.
	toolDefs := toolChatDefs(s.toolProv)
	for round := 0; round <= MaxToolRounds; round++ {
		offerTools := round < MaxToolRounds // final round: no tools, force an answer
		offered := toolDefs
		if !offerTools {
			offered = nil
		}
		res, err := complete(ctx, messages, offered)
		if err != nil {
			return fail(err)
		}
		usage.PromptTokens += res.Usage.PromptTokens
		usage.CompletionTokens += res.Usage.CompletionTokens

		if len(res.Message.ToolCalls) == 0 || res.FinishReason != "tool_calls" {
			// Final answer row (provenance + request usage + reasoning).
			assistant, err := s.store.Append(store.Message{
				ID: s.idGen(), SessionID: SessionID, ExecutionID: root, Role: "assistant",
				Content: res.Message.Content, Reasoning: res.Message.Reasoning,
				Provider: providerName, Model: model,
				PromptTokens: nullInt(usage.PromptTokens), CompletionTokens: nullInt(usage.CompletionTokens),
			})
			if err != nil {
				return fail(err)
			}
			s.emitEvent(assistant, root, "")
			outcome.Rows = append(outcome.Rows, assistant)
			outcome.EndCursor = assistant.Seq
			return outcome
		}

		// Persist the assistant tool-call INTENT before executing.
		intentJSON, _ := json.Marshal(res.Message.ToolCalls)
		intent, err := s.store.Append(store.Message{
			ID: s.idGen(), SessionID: SessionID, ExecutionID: root, Role: "assistant",
			Content: res.Message.Content, Reasoning: res.Message.Reasoning,
			ToolCalls: string(intentJSON),
			Provider:  providerName, Model: model,
		})
		if err != nil {
			return fail(err)
		}
		s.emitEvent(intent, root, "")
		outcome.Rows = append(outcome.Rows, intent)
		messages = append(messages, chatToProvider(res.Message))

		// Execute each requested tool; persist result rows.
		for _, call := range res.Message.ToolCalls {
			result := tools.Execute(ctx, s.toolProv, call.Function.Name, call.Function.Arguments)
			s.emitToolEvent(root, call.Function.Name, result)
			content := result.Output
			if result.Err != nil {
				content = "error: " + result.Err.Error() // model-visible failure
			}
			toolRow, err := s.store.Append(store.Message{
				ID: s.idGen(), SessionID: SessionID, ExecutionID: root, Role: "tool",
				Content: content, ToolCallID: call.ID, ToolName: call.Function.Name,
			})
			if err != nil {
				return fail(err)
			}
			s.emitEvent(toolRow, root, call.Function.Name)
			outcome.Rows = append(outcome.Rows, toolRow)
			messages = append(messages, providers.ChatMessage{Role: "tool", Content: content, ToolCallID: call.ID, Name: call.Function.Name})
		}
	}
	// Unreachable: the final round offers no tools, so the loop always
	// returns inside it; keep a typed guard anyway.
	return fail(fmt.Errorf("chat: tool rounds exhausted"))
}

// deltaCoalescer batches volatile chat deltas for the bus (~100ms or
// kind-change flush) so bounded mailboxes never thrash on token rates,
// while the caller's onDelta receives every fragment directly.
type deltaCoalescer struct {
	s       *Service
	onDelta func(Delta)
	mu      sync.Mutex
	closed  bool
	bufKind string
	buf     strings.Builder
	timer   *time.Timer
	stamp   time.Time
}

const deltaFlushInterval = 100 * time.Millisecond

func newDeltaCoalescer(s *Service, onDelta func(Delta)) *deltaCoalescer {
	c := &deltaCoalescer{s: s, onDelta: onDelta}
	c.mu.Lock()
	c.timer = time.AfterFunc(deltaFlushInterval, c.tick)
	c.mu.Unlock()
	return c
}

func (c *deltaCoalescer) push(d Delta) {
	if c.onDelta != nil {
		c.onDelta(d) // direct leg: every fragment, no batching
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if d.Kind != c.bufKind || time.Since(c.lastFlush()) >= deltaFlushInterval {
		c.flushLocked()
		c.bufKind = d.Kind
	}
	c.buf.WriteString(d.Text)
}

// lastFlush reads the flush timestamp; caller holds c.mu.
func (c *deltaCoalescer) lastFlush() time.Time { return c.stamp }

func (c *deltaCoalescer) tick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.flushLocked()
	c.timer.Reset(deltaFlushInterval)
}

// flush publishes the buffered fragment as ONE volatile bus event.
// ctx-ok: F-15-family fire-and-forget volatile emit (drop-oldest bus).
func (c *deltaCoalescer) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked()
}

// flushLocked publishes the buffer; caller holds c.mu.
func (c *deltaCoalescer) flushLocked() {
	if c.buf.Len() == 0 || c.s.bus == nil {
		c.buf.Reset()
		return
	}
	_ = c.s.bus.Publish(context.Background(), domain.TimelineEvent{ // ctx-ok: F-15-family volatile fire-and-forget emit (coalescer goroutine)
		ID: c.s.idGen(), SessionID: SessionID, Kind: domain.EventChatDelta, Text: c.buf.String(),
	})
	c.buf.Reset()
	c.stamp = time.Now()
}

// close stops the timer and flushes the tail. After close, tick cannot
// resurrect the timer (closed flag under the same mutex) and push is a
// no-op on the bus leg.
func (c *deltaCoalescer) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.flushLocked()
}

// Sync pages the transcript for clients.
func (s *Service) Sync(afterSeq int64, limit int) ([]store.Message, int64, bool, error) {
	rows, err := s.store.Replay(SessionID, afterSeq, limit)
	if err != nil {
		return nil, 0, false, err
	}
	next := afterSeq
	for _, r := range rows {
		if r.Seq > next {
			next = r.Seq
		}
	}
	latest, err := s.store.LatestSeq(SessionID)
	if err != nil {
		return nil, 0, false, err
	}
	return rows, next, latest > next, nil
}

// windowToChatMessages maps a HistoryWindow to provider messages; the
// summary becomes a system message.
func windowToChatMessages(hw store.HistoryWindow) []providers.ChatMessage {
	var out []providers.ChatMessage
	if hw.Summary != nil {
		out = append(out, providers.ChatMessage{Role: "system", Content: hw.Summary.Content})
	}
	for _, m := range hw.Rows {
		cm := providers.ChatMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, Name: m.ToolName}
		if m.ToolCalls != "" {
			_ = json.Unmarshal([]byte(m.ToolCalls), &cm.ToolCalls)
		}
		out = append(out, cm)
	}
	return out
}

func chatToProvider(m providers.ChatMessage) providers.ChatMessage { return m }

// toolChatDefs maps the provider's tools to OpenAI defs.
func toolChatDefs(p tools.Provider) []providers.ChatToolDef {
	var defs []providers.ChatToolDef
	for _, tool := range p.Tools() {
		d := tool.Def()
		var params any
		_ = json.Unmarshal([]byte(d.ParamsJSON), &params)
		def := providers.ChatToolDef{Type: "function"}
		def.Function.Name = d.Name
		def.Function.Description = d.Description
		def.Function.Parameters = params
		defs = append(defs, def)
	}
	return defs
}

// emitEvent persists a session timeline event BEFORE volatile publish.
// emitEvent persists a session timeline event and (F-15, pinned by
// characterization tests) publishes ONLY on successful persistence — the
// OPPOSITE of the engine adapter's emit, which publishes despite append
// failure. Bus publish errors (e.g. ErrClosed on a closed bus) are
// swallowed; durability is never lost. Policy follow-up:
// backend-engine-run-ownership.
func (s *Service) emitEvent(m store.Message, execID, toolName string) {
	kind := "session"
	payload := map[string]any{"role": m.Role, "content": m.Content, "message_id": m.ID, "seq": m.Seq}
	if toolName != "" {
		payload["tool"] = toolName
	}
	payloadJSON, _ := json.Marshal(payload)
	evt := store.Event{ID: s.idGen(), SessionID: SessionID, ExecutionID: execID, Kind: kind, Payload: string(payloadJSON)}
	if err := s.store.AppendEvent(evt); err == nil && s.bus != nil {
		_ = s.bus.Publish(context.Background(), domain.TimelineEvent{SessionID: SessionID, Kind: domain.TimelineEventKind(kind), Text: m.Content}) // ctx-ok: F-15 fire-and-forget emit
	}
}

// emitToolEvent is emitEvent for tool-call events (same F-15 semantics).
func (s *Service) emitToolEvent(execID, name string, result tools.Result) {
	payload, _ := json.Marshal(map[string]any{"tool": name, "output": result.Output, "error": errString(result.Err)})
	evt := store.Event{ID: s.idGen(), SessionID: SessionID, ExecutionID: execID, Kind: "tool_call", Payload: string(payload)}
	if err := s.store.AppendEvent(evt); err == nil && s.bus != nil {
		_ = s.bus.Publish(context.Background(), domain.TimelineEvent{SessionID: SessionID, Kind: domain.EventToolCall, Text: name}) // ctx-ok: F-15 fire-and-forget emit
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func nullInt(v int64) sqlNull { return sqlNull{Valid: v > 0, Int64: v} }

type sqlNull = sql.NullInt64

// ProviderStore is the subset of the providers Store the chat needs
// (atomic provider+credential resolution — the same seam discovery uses).
type ProviderStore interface {
	Lookup(name string) (providers.Provider, error)
	ResolvedLookup(ctx context.Context, name string) (providers.Provider, string, error)
	Secrets() providers.SecretStore
}

// NewStoreBacked wires the chat over the providers store's atomic
// lookup+credential seam, avoiding the update-race where a provider's
// new key could be sent to its old URL.
func NewStoreBacked(db *store.Store, provStore ProviderStore, completer Completer, toolProv tools.Provider, bus *eventbus.Bus, idGen func() string) *Service {
	svc := New(db,
		provStore.Lookup,
		func(ctx context.Context, p providers.Provider) (string, error) {
			return providers.ResolveCredential(ctx, p, provStore.Secrets())
		},
		completer, toolProv, bus, idGen)
	return svc
}
