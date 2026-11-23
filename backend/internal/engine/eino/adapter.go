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
	"time"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/engine"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/progress"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wards"
	"zbot/backend/internal/wardtools"
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
	// snapFn supplies registry snapshots (agents for delegation); nil =
	// no agents (delegation off, agent-named runs refuse with the empty
	// list).
	snapFn func() registry.Snapshot
	// depthCap bounds the delegation tree regardless of agent grants.
	depthCap int
	idGen    func() string
	// newModel builds (or fakes, in tests) the chat client on a cache miss.
	newModel func(ctx context.Context, prov providers.Provider, token, modelID string) (chatGenerator, error)

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // sessionID → cancel
	// wardVault builds the ward tool plane for a session (nil = wards
	// off, e.g. no vault configured). Sticky per session.
	wardVault func() *wards.Vault
	wardHosts map[string]*wardtools.Host // sessionID → sticky ward host
	models    map[string]chatGenerator   // cache: provider/model/key → model
}

// DefaultDepthCap bounds delegation depth unless configured lower.
const DefaultDepthCap = 2

// New creates the engine adapter. snapFn may be nil.
func New(provStore ProviderStore, st *store.Store, bus *eventbus.Bus, toolProv tools.Provider, snapFn func() registry.Snapshot) *Adapter {
	return &Adapter{
		provStore: provStore, st: st, bus: bus, toolProv: toolProv,
		snapFn: snapFn, depthCap: DefaultDepthCap,
		cancels: map[string]context.CancelFunc{},
		models:  map[string]chatGenerator{},
		idGen:   func() string { return fmt.Sprintf("evt-%d-%d", time.Now().UnixNano(), nextID()) },
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
// Model precedence: the request's model, else the named agent's, else a
// typed refusal. req.Agent shapes the run (system prompt, tool allowlist,
// delegation grant); req.Context remains unimplemented (run-ownership
// follow-up).
func (a *Adapter) RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	var snap registry.Snapshot
	if a.snapFn != nil {
		snap = a.snapFn()
	}
	in := runInput{systemPrompt: systemPrompt, grant: a.depthCap, maxSteps: req.MaxSteps}
	if req.SystemPrompt != "" && req.Agent == "" {
		in.systemPrompt = req.SystemPrompt // daemon-composed prompt (GoalRequest.SystemPrompt, json:"-")
	}
	if req.Agent != "" {
		spec, ok := snap.Agents[req.Agent]
		if !ok {
			return nil, fmt.Errorf("%w %q (configured: %s)", ErrUnknownAgent, req.Agent, agentList(snap))
		}
		in.agentName = spec.Name
		in.systemPrompt = spec.SystemPrompt
		if in.systemPrompt == "" && spec.Prompt != "" {
			in.systemPrompt = "(prompt template " + spec.Prompt + ")"
		}
		if req.SystemPrompt != "" {
			// The middleware's composed DYNAMIC sections (Task Analysis,
			// Skills, Context) append to the agent's body — the agent's
			// identity is never replaced.
			in.systemPrompt = strings.TrimSpace(in.systemPrompt) + "\n\n" + req.SystemPrompt
		}
		in.toolAllow = spec.Tools
		in.grant = spec.MaxDepth
		if req.Model == "" && spec.Model != "" {
			req.Model = spec.Model
		}
	}
	modelRef := req.Model
	if modelRef == "" {
		return nil, fmt.Errorf("engine: no model selected — pass model or configure the agent's model")
	}
	providerName, modelID, found := strings.Cut(modelRef, "/")
	if !found || providerName == "" || modelID == "" {
		return nil, fmt.Errorf("engine: invalid model reference %q (want provider/model-id)", modelRef)
	}
	in.providerName, in.modelID = providerName, modelID

	// Resolve provider + credential atomically.
	prov, token, err := a.provStore.ResolvedLookup(ctx, providerName)
	if err != nil {
		return nil, fmt.Errorf("engine: provider %q: %w", providerName, err)
	}

	// Create/reuse session + root execution.
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("goal-%d-%d", time.Now().UnixNano(), nextID())
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

	if a.wardVault != nil && !in.isChild {
		in.wardHost = a.wardHost(sessionID)
	}
	in.sessionID, in.execID, in.goal, in.token, in.prov = sessionID, rootExec, req.Goal, token, prov
	events := make(chan domain.TimelineEvent, 64)
	go a.run(runCtx, cancel, events, in)
	return events, nil
}

type runInput struct {
	sessionID, execID, goal      string
	providerName, modelID, token string
	prov                         providers.Provider
	maxSteps                     int // per-run bound (clamped to MaxSteps; 0 = default)

	// Delegation wiring: agent-shaped runs and children.
	systemPrompt string   // resolved system prompt (agent body or default)
	toolAllow    []string // agent allowlist (nil = all tools)
	agentName    string   // "" for root default wiring
	depth        int      // 0 = root
	grant        int      // may delegate while depth < grant (agent max_depth; root = cap)
	isChild      bool     // children surface steps as delegation events

	// Ward hosting (slice 3): the session's sticky host; nil = wards
	// off. Root runs get the ward tools; children get the file tools +
	// the Active Ward packet on their prompt (read + declared roles).
	wardHost *wardtools.Host
}

// run is the ROOT wrapper: cleanup, cancellation registration, channel
// close. Children bypass it (no cancels-map entry, no channel close).
func (a *Adapter) run(ctx context.Context, cancel context.CancelFunc, events chan domain.TimelineEvent, in runInput) {
	defer func() {
		cancel()
		a.mu.Lock()
		delete(a.cancels, in.sessionID)
		a.mu.Unlock()
		close(events)
	}()
	// Root run: the answer was already emitted as the final agent step;
	// runLoop's error variants emit their own failure events (the caller
	// observes the stream, not the return value).
	if _, err := a.runLoop(ctx, events, in); err != nil {
		// Nothing further to do at the root: the failure event and
		// execution status were already recorded inside the loop.
		_ = err
	}
}

// runLoop is the ONE bounded loop for roots and children: returns the
// final answer text ("" on failure/cancellation). Children reuse it via
// the delegate tool with their own execution record and wiring.
func (a *Adapter) runLoop(ctx context.Context, events chan domain.TimelineEvent, in runInput) (string, error) {

	emit := func(kind domain.TimelineEventKind, text string) {
		chanKind, chanAgent := kind, in.providerName+"/"+in.modelID
		if in.isChild {
			// Child steps surface in the parent's stream as delegation
			// events (the Timeline Tree keeps the original kinds under
			// the child's execution).
			chanKind, chanAgent = domain.EventDelegation, in.agentName
		} else if in.agentName != "" {
			// Named root runs attribute their events to the agent.
			chanAgent = in.agentName
		}
		evt := domain.TimelineEvent{
			ID: a.idGen(), SessionID: in.sessionID, Kind: chanKind,
			Text: text, Agent: chanAgent,
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
			return "", fmt.Errorf("engine: chat model: %w", err)
		}
		a.mu.Lock()
		a.models[modelKey] = chatModel
		a.mu.Unlock()
	}

	// Append the goal as the opening user message.
	var err error
	_, err = a.st.Append(store.Message{
		ID: fmt.Sprintf("msg-goal-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
		ExecutionID: in.execID, Role: "user", Content: in.goal,
	})
	if err != nil {
		emit(domain.EventAgentStep, "engine: failed to persist goal: "+err.Error())
		_ = a.st.FinishExecution(in.execID, "failed", err.Error(), 0, 0)
		return "", fmt.Errorf("engine: persist goal: %w", err)
	}
	emit(domain.EventSession, fmt.Sprintf("goal: %s", in.goal))

	// Progress-aware bounded loop (the operator's agentzero shape):
	// soft budget nudges, auto-extension for productive work, early
	// kill ONLY when the progress tracker says the agent is stuck.
	var tokensIn, tokensOut int64
	var childMsgs []*schema.Message // child's retained conversation (P1-2)
	softBudget := in.maxSteps       // from the intent complexity (0 = default)
	if softBudget <= 0 || softBudget > MaxSteps {
		softBudget = MaxSteps
	}
	hardBudget := softBudget * 2                   // hard stop at 2x soft
	tracker := progress.New()                      // per-run scoring
	maxExtensions := 3                             // productive agents can earn up to 3 windows
	steps := hardBudget + maxExtensions*softBudget // absolute ceiling
	for step := 0; step < steps; step++ {
		if ctx.Err() != nil {
			emit(domain.EventSession, "run cancelled")
			_ = a.st.FinishExecution(in.execID, "cancelled", "", tokensIn, tokensOut)
			return "", fmt.Errorf("engine: cancelled")
		}

		// Progress-aware steering (the agentzero shape):
		// 1. Kill only when genuinely stuck (warned + sinking score).
		if tracker.ShouldStop() {
			emit(domain.EventAgentStep, "engine: stopping — agent stuck (progress score "+fmt.Sprint(tracker.Score())+")")
			_ = a.st.FinishExecution(in.execID, "failed", "stuck: no progress", tokensIn, tokensOut)
			return "", fmt.Errorf("engine: stuck (score %d after %d iterations)", tracker.Score(), tracker.Iterations())
		}
		// 2. At the soft budget: extend if productive, nudge otherwise.
		if step > 0 && step%softBudget == 0 {
			if tracker.ShouldExtend(maxExtensions) {
				tracker.Extend()
				emit(domain.EventAgentStep, fmt.Sprintf("engine: extending run (productive, score %d, extension %d)", tracker.Score(), tracker.Extensions()))
			}
		}
		tracker.Tick()

		// Build messages. Root: compaction-aware session history. Child:
		// a FRESH context — system prompt + the delegated task only (the
		// parent embeds needed context in the task; inheriting the whole
		// session transcript pollutes the child and bills the parent's
		// history twice).
		var msgs []*schema.Message
		msgs, childMsgs, err = a.buildMessages(in, childMsgs)
		if err != nil {
			emit(domain.EventAgentStep, "engine: "+err.Error())
			_ = a.st.FinishExecution(in.execID, "failed", err.Error(), tokensIn, tokensOut)
			return "", err
		}
		// Offer tools on all but the final step: the agent's allowlist
		// plus the delegate tool when this run's depth grant allows. The
		// offered set is ALSO the execution inventory (an allowlist binds
		// execution, not just the menu; the delegate tool is executable).
		var opts []model.Option
		var offered []tools.Tool
		if step < steps-1 && step < hardBudget-1 { // suppress tools only near the absolute ceiling
			if in.toolAllow != nil && len(in.toolAllow) == 0 {
				offered = nil // tools: [] is an EXPLICIT no-tools inventory
			} else {
				offered = engine.FilterTools(a.toolProv, in.toolAllow)
			}
			if in.depth < in.grant && in.depth < a.depthCap && a.snapFn != nil {
				offered = append(offered, a.delegateTool(ctx, events, in))
			}
			offered = append(offered, wardToolSet(in)...)
			// Inject progress nudges (advisory user messages, not system
			// prompts — the model sees them as steering, not overrides).
			if nudge := tracker.Nudge(step, softBudget, hardBudget); nudge != "" {
				msgs = append(msgs, schema.UserMessage(nudge))
			}
			var toolInfos []*schema.ToolInfo
			for _, t := range offered {
				d := t.Def()
				ti := &schema.ToolInfo{Name: d.Name, Desc: d.Description,
					ParamsOneOf: paramsOneOfFromJSON(d.ParamsJSON)}
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
				return "", fmt.Errorf("engine: cancelled: %w", ctx.Err())
			}
			emit(domain.EventAgentStep, "engine: model error: "+err.Error())
			_ = a.st.FinishExecution(in.execID, "failed", err.Error(), tokensIn, tokensOut)
			return "", fmt.Errorf("engine: model error: %w", err)
		}
		if out.ResponseMeta != nil && out.ResponseMeta.Usage != nil {
			tokensIn += int64(out.ResponseMeta.Usage.PromptTokens)
			tokensOut += int64(out.ResponseMeta.Usage.CompletionTokens)
		}

		if len(out.ToolCalls) == 0 {
			// Final answer.
			_, _ = a.st.Append(store.Message{
				ID: fmt.Sprintf("msg-final-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
				ExecutionID: in.execID, Role: "assistant", Content: out.Content,
				Provider: in.providerName, Model: in.modelID,
			})
			emit(domain.EventAgentStep, out.Content)
			emit(domain.EventSession, "run completed")
			_ = a.st.FinishExecution(in.execID, "completed", "", tokensIn, tokensOut)
			return out.Content, nil
		}

		// Tool round: persist intent, execute, persist results.
		intentJSON, _ := json.Marshal(out.ToolCalls)
		_, _ = a.st.Append(store.Message{
			ID: fmt.Sprintf("msg-intent-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
			ExecutionID: in.execID, Role: "assistant", Content: out.Content,
			ToolCalls: string(intentJSON), Provider: in.providerName, Model: in.modelID,
		})
		emit(domain.EventAgentStep, out.Content)
		if in.isChild {
			childMsgs = append(childMsgs, out)
		}

		for _, tc := range out.ToolCalls {
			result := execOffered(ctx, offered, tc.Function.Name, tc.Function.Arguments)
			content := result.Output
			errText := ""
			if result.Err != nil {
				content = "error: " + result.Err.Error()
				errText = result.Err.Error()
			}
			tracker.Record(tc.Function.Name, tc.Function.Arguments, errText)
			// Tool event.
			payload, _ := json.Marshal(map[string]any{"tool": tc.Function.Name, "output": content})
			_ = a.st.AppendEvent(store.Event{
				ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID,
				Kind: "tool_call", Payload: string(payload),
			})
			toolEvt := domain.TimelineEvent{
				ID: a.idGen(), SessionID: in.sessionID, Kind: domain.EventToolCall,
				ToolCall: &domain.ToolCallPayload{Tool: tc.Function.Name, Output: content},
			}
			if in.isChild || in.agentName != "" {
				// Tool events carry the agent attribution (child runs
				// surface them as delegation events like every step).
				toolEvt.Agent = in.agentName
				if in.isChild {
					toolEvt.Kind = domain.EventDelegation
				}
			}
			if a.bus != nil {
				_ = a.bus.Publish(context.Background(), toolEvt) // ctx-ok: F-15
			}
			select {
			case events <- toolEvt:
			case <-ctx.Done():
			}
			_, _ = a.st.Append(store.Message{
				ID: fmt.Sprintf("msg-tool-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
				ExecutionID: in.execID, Role: "tool", Content: content,
				ToolCallID: tc.ID, ToolName: tc.Function.Name,
			})
			if in.isChild {
				childMsgs = append(childMsgs, &schema.Message{
					Role: schema.Tool, Content: content, ToolCallID: tc.ID, ToolName: tc.Function.Name,
				})
			}
		}
	}

	// Step bound reached.
	emit(domain.EventSession, "run stopped: step bound reached")
	_ = a.st.FinishExecution(in.execID, "failed", "step bound reached", tokensIn, tokensOut)
	return "", fmt.Errorf("engine: step bound reached")
}

// hashToken produces a short stable hash for cache keying (never the
// token itself).
func hashToken(token string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return fmt.Sprint(h.Sum32())
}

// execOffered executes one tool call against the step's offered inventory
// (allowlist + delegate). Unknown or non-offered names are typed errors the
// model can see — never silent execution outside the inventory.
func execOffered(ctx context.Context, offered []tools.Tool, name, argsJSON string) tools.Result {
	for _, t := range offered {
		if t.Def().Name == name {
			return t.Exec(ctx, argsJSON)
		}
	}
	return tools.Result{Err: fmt.Errorf("%w: %q", ErrToolNotOffered, name)}
}
