// Package eino implements domain.AgentEngine using CloudWeGo Eino's
// OpenAI-compatible chat model. This is the ONLY package that imports
// eino/eino-ext (constitution: engine isolation). The loop is a thin
// iteration over the spike-verified Generate + WithTools mechanics —
// not eino's agent compositions — because the bounded-loop seam needs
// direct control over step counting, cancellation checks, and
// durable-first event emission at each step.
package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/engine"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// MaxSteps bounds one goal run's model calls.
const MaxSteps = 24

// ProviderStore is the providers store seam (atomic lookup+credential).
type ProviderStore interface {
	Lookup(name string) (providers.Provider, error)
	ResolvedLookup(ctx context.Context, name string) (providers.Provider, string, error)
	Secrets() providers.SecretStore
}

// chatGenerator is the slice of eino's model.ChatModel the loop needs.
// Unexported so tests can inject a fake model without a live provider.
type chatGenerator interface {
	Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

// Adapter implements domain.AgentEngine over eino's chat model.
type Adapter struct {
	provStore ProviderStore
	st        *store.Store
	bus       *eventbus.Bus
	toolProv  tools.Provider
	idGen     func() string
	// newModel builds (or fakes, in tests) the chat client on a cache miss.
	newModel func(ctx context.Context, prov providers.Provider, token, modelID string) (chatGenerator, error)

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // sessionID → cancel
	models  map[string]chatGenerator      // cache: provider/model/key → model
}

// New creates the engine adapter.
func New(provStore ProviderStore, st *store.Store, bus *eventbus.Bus, toolProv tools.Provider) *Adapter {
	return &Adapter{
		provStore: provStore, st: st, bus: bus, toolProv: toolProv,
		cancels: map[string]context.CancelFunc{},
		models:  map[string]chatGenerator{},
		idGen:   func() string { return fmt.Sprintf("evt-%d", nextID()) },
		newModel: func(ctx context.Context, prov providers.Provider, token, modelID string) (chatGenerator, error) {
			return einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
				BaseURL: prov.BaseURL,
				APIKey:  token,
				Model:   modelID,
			})
		},
	}
}

var idCounter int64

func nextID() int64 {
	return atomic.AddInt64(&idCounter, 1)
}

// Name identifies the engine.
func (a *Adapter) Name() string { return "eino" }

// RunGoal starts an autonomous run toward req.Goal, streaming TimelineEvents.
// Model resolution is request-only today (F-25): req.Model must name
// provider/model-id; req.Agent and req.Context are accepted but NOT yet
// forwarded into the run (the agent seam lands with the run-ownership
// follow-up).
func (a *Adapter) RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	modelRef := req.Model
	if modelRef == "" {
		return nil, fmt.Errorf("engine: no model selected — pass model or configure the agent's model")
	}
	providerName, modelID, found := strings.Cut(modelRef, "/")
	if !found || providerName == "" || modelID == "" {
		return nil, fmt.Errorf("engine: invalid model reference %q (want provider/model-id)", modelRef)
	}

	// Resolve provider + credential atomically.
	prov, token, err := a.provStore.ResolvedLookup(ctx, providerName)
	if err != nil {
		return nil, fmt.Errorf("engine: provider %q: %w", providerName, err)
	}

	// Create/reuse session + root execution.
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "goal-" + fmt.Sprint(nextID())
	}
	rootExec, err := a.st.EnsureSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Register cancellation.
	runCtx, cancel := context.WithCancel(context.Background()) // ctx-ok: F-23 detach (follow-up backend-engine-run-ownership)
	a.mu.Lock()
	a.cancels[sessionID] = cancel
	a.mu.Unlock()

	events := make(chan domain.TimelineEvent, 64)
	go a.run(runCtx, cancel, events, runInput{
		sessionID: sessionID, execID: rootExec, goal: req.Goal,
		providerName: providerName, modelID: modelID, token: token, prov: prov,
	})
	return events, nil
}

type runInput struct {
	sessionID, execID, goal      string
	providerName, modelID, token string
	prov                         providers.Provider
}

func (a *Adapter) run(ctx context.Context, cancel context.CancelFunc, events chan domain.TimelineEvent, in runInput) {
	defer func() {
		cancel()
		a.mu.Lock()
		delete(a.cancels, in.sessionID)
		a.mu.Unlock()
		close(events)
	}()

	emit := func(kind domain.TimelineEventKind, text string) {
		evt := domain.TimelineEvent{
			ID: a.idGen(), SessionID: in.sessionID, Kind: kind,
			Text: text, Agent: in.providerName + "/" + in.modelID,
		}
		// ATTEMPTED-durable-first (F-15, pinned by characterization tests):
		// the append error is IGNORED — publish and stream happen anyway, so
		// clients may observe events that failed to persist. The chat site
		// does the OPPOSITE (skips publish on append failure). The
		// retry/log/publish-anyway policy is owned by follow-up
		// backend-engine-run-ownership.
		payload, _ := json.Marshal(map[string]any{"text": text})
		_ = a.st.AppendEvent(store.Event{
			ID: evt.ID, SessionID: in.sessionID, ExecutionID: in.execID,
			Kind: string(kind), Payload: string(payload),
		})
		if a.bus != nil {
			_ = a.bus.Publish(context.Background(), evt) // ctx-ok: F-15 fire-and-forget emit
		}
		select {
		case events <- evt:
		case <-ctx.Done():
		}
	}

	// Build or reuse the chat model (cached per provider+model+key to
	// avoid re-initializing the HTTP client on every run).
	modelKey := in.providerName + "/" + in.modelID + "/" + hashToken(in.token)
	a.mu.Lock()
	chatModel, ok := a.models[modelKey]
	a.mu.Unlock()
	if !ok {
		var err error
		chatModel, err = a.newModel(ctx, in.prov, in.token, in.modelID)
		if err != nil {
			emit(domain.EventAgentStep, "engine: failed to create chat model: "+err.Error())
			_ = a.st.FinishExecution(in.execID, "failed", err.Error(), 0, 0)
			return
		}
		a.mu.Lock()
		a.models[modelKey] = chatModel
		a.mu.Unlock()
	}

	// Append the goal as the opening user message.
	var err error
	_, err = a.st.Append(store.Message{
		ID: fmt.Sprintf("msg-goal-%d", nextID()), SessionID: in.sessionID,
		ExecutionID: in.execID, Role: "user", Content: in.goal,
	})
	if err != nil {
		emit(domain.EventAgentStep, "engine: failed to persist goal: "+err.Error())
		_ = a.st.FinishExecution(in.execID, "failed", err.Error(), 0, 0)
		return
	}
	emit(domain.EventSession, fmt.Sprintf("goal: %s", in.goal))

	// Bounded loop over the spike-verified Generate + WithTools.
	var tokensIn, tokensOut int64
	for step := 0; step < MaxSteps; step++ {
		if ctx.Err() != nil {
			emit(domain.EventSession, "run cancelled")
			_ = a.st.FinishExecution(in.execID, "cancelled", "", tokensIn, tokensOut)
			return
		}

		// Build messages: goal + history.
		hw, err := a.st.HistoryWindow(in.sessionID, "", 200)
		if err != nil {
			emit(domain.EventAgentStep, "engine: history error: "+err.Error())
			break
		}
		var msgs []*schema.Message
		msgs = append(msgs, schema.SystemMessage(systemPrompt))
		if hw.Summary != nil {
			msgs = append(msgs, schema.SystemMessage(hw.Summary.Content))
		}
		for _, m := range hw.Rows {
			switch m.Role {
			case "user":
				msgs = append(msgs, schema.UserMessage(m.Content))
			case "assistant":
				am := &schema.Message{Role: schema.Assistant, Content: m.Content}
				if m.ToolCalls != "" {
					_ = json.Unmarshal([]byte(m.ToolCalls), &am.ToolCalls)
				}
				msgs = append(msgs, am)
			case "tool":
				msgs = append(msgs, &schema.Message{Role: schema.Tool, Content: m.Content, ToolCallID: m.ToolCallID, ToolName: m.ToolName})
			}
		}

		// Offer tools on all but the final step.
		var opts []model.Option
		if step < MaxSteps-1 {
			var toolInfos []*schema.ToolInfo
			for _, t := range engine.FilterTools(a.toolProv, nil) {
				d := t.Def()
				ti := &schema.ToolInfo{Name: d.Name, Desc: d.Description,
					ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}
				toolInfos = append(toolInfos, ti)
			}
			if len(toolInfos) > 0 {
				opts = append(opts, model.WithTools(toolInfos))
			}
		}

		out, err := chatModel.Generate(ctx, msgs, opts...)
		if err != nil {
			if ctx.Err() != nil {
				emit(domain.EventSession, "run cancelled")
				_ = a.st.FinishExecution(in.execID, "cancelled", "", tokensIn, tokensOut)
				return
			}
			emit(domain.EventAgentStep, "engine: model error: "+err.Error())
			_ = a.st.FinishExecution(in.execID, "failed", err.Error(), tokensIn, tokensOut)
			return
		}
		if out.ResponseMeta != nil && out.ResponseMeta.Usage != nil {
			tokensIn += int64(out.ResponseMeta.Usage.PromptTokens)
			tokensOut += int64(out.ResponseMeta.Usage.CompletionTokens)
		}

		if len(out.ToolCalls) == 0 {
			// Final answer.
			_, _ = a.st.Append(store.Message{
				ID: fmt.Sprintf("msg-final-%d", nextID()), SessionID: in.sessionID,
				ExecutionID: in.execID, Role: "assistant", Content: out.Content,
				Provider: in.providerName, Model: in.modelID,
			})
			emit(domain.EventAgentStep, out.Content)
			emit(domain.EventSession, "run completed")
			_ = a.st.FinishExecution(in.execID, "completed", "", tokensIn, tokensOut)
			return
		}

		// Tool round: persist intent, execute, persist results.
		intentJSON, _ := json.Marshal(out.ToolCalls)
		_, _ = a.st.Append(store.Message{
			ID: fmt.Sprintf("msg-intent-%d", nextID()), SessionID: in.sessionID,
			ExecutionID: in.execID, Role: "assistant", Content: out.Content,
			ToolCalls: string(intentJSON), Provider: in.providerName, Model: in.modelID,
		})
		emit(domain.EventAgentStep, out.Content)

		for _, tc := range out.ToolCalls {
			result := engine.ExecuteTool(ctx, a.toolProv, tc.Function.Name, tc.Function.Arguments)
			content := result.Output
			if result.Err != nil {
				content = "error: " + result.Err.Error()
			}
			// Tool event.
			payload, _ := json.Marshal(map[string]any{"tool": tc.Function.Name, "output": content})
			_ = a.st.AppendEvent(store.Event{
				ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID,
				Kind: "tool_call", Payload: string(payload),
			})
			if a.bus != nil {
				_ = a.bus.Publish(context.Background(), domain.TimelineEvent{ // ctx-ok: F-15
					ID: a.idGen(), SessionID: in.sessionID, Kind: domain.EventToolCall,
					ToolCall: &domain.ToolCallPayload{Tool: tc.Function.Name, Output: content},
				})
			}
			_, _ = a.st.Append(store.Message{
				ID: fmt.Sprintf("msg-tool-%d", nextID()), SessionID: in.sessionID,
				ExecutionID: in.execID, Role: "tool", Content: content,
				ToolCallID: tc.ID, ToolName: tc.Function.Name,
			})
		}
	}

	// Step bound reached.
	emit(domain.EventSession, "run stopped: step bound reached")
	_ = a.st.FinishExecution(in.execID, "failed", "step bound reached", tokensIn, tokensOut)
}

// Cancel stops a session's active run.
func (a *Adapter) Cancel(ctx context.Context, sessionID string) error {
	a.mu.Lock()
	cancel, ok := a.cancels[sessionID]
	a.mu.Unlock()
	if ok {
		cancel()
	}
	return nil // idempotent on idle sessions
}

// Resume is not yet implemented (typed not-yet).
func (a *Adapter) Resume(ctx context.Context, sessionID string, input domain.UserInput) (<-chan domain.TimelineEvent, error) {
	return nil, fmt.Errorf("engine: resume not yet implemented")
}

const systemPrompt = "You are Karta, a local-first assistant. Work toward the user's goal using the available tools. Be direct and useful."

// hashToken produces a short stable hash for cache keying (never the
// token itself).
func hashToken(token string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return fmt.Sprint(h.Sum32())
}
