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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
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
	// runAttr remembers the run currently executing per session (agent +
	// execution) — attribution for host-level events raised without a
	// runInput, i.e. ward transitions (run-legibility).
	runAttr map[string]runAttribution
	// wardBinder observes ward enters per session (session-row binding;
	// nil = no binding updates).
	wardBinder func(sessionID, ward string)
	// mcpTools supplies the live MCP tool plane (the retrieval plane;
	// nil = none mounted).
	mcpTools func() []tools.Tool
	// wardVault builds the ward tool plane for a session (nil = wards
	// off, e.g. no vault configured). Sticky per session.
	wardVault func() *wards.Vault
	wardHosts map[string]*wardtools.Host // sessionID → sticky ward host
	models    map[string]chatGenerator   // cache: provider/model/key → model
	// loop selects the run path (engine-adk-spine): LoopLegacy (the
	// hand-rolled runLoop) or LoopADK (runAgent). Both pass the
	// conformance suite during the transition; config flips it.
	loop string
	// opts are the daemon-applied context-budget knobs (slice 1).
	opts RunOptions
	// compactorFor binds the kernel compaction policy to a run's model
	// (nil = no in-run compaction). The adapter never holds the policy —
	// the backend constitution's Compaction Rule.
	compactorFor func(providerName, modelID string) domain.Compactor
	// effortOpt is the option the usage wrapper appends for an agent's
	// reasoning_effort (DefaultEffortOption; tests substitute a recorder).
	effortOpt EffortOption
	// hooks raises lifecycle events (internal/hooks by default; nil = no
	// hooks). The seams live in hooks.go.
	hooks domain.HookRunner
}

// SetCompactorFactory wires the in-run compaction policy
// (internal/compaction) — the bridge (mw_compaction.go) asks it before
// every model call once the context is over its budget.
func (a *Adapter) SetCompactorFactory(f func(providerName, modelID string) domain.Compactor) {
	a.compactorFor = f
}

// runChild runs a delegated child on the engine's run path: the ADK spine
// when it is the loop — children get the spine's middlewares (caps,
// skills, compaction, effort; slice 2) — the legacy loop otherwise.
func (a *Adapter) runChild(ctx context.Context, events chan domain.TimelineEvent, in runInput) (string, error) {
	if a.loop == LoopADK {
		return a.runAgent(ctx, events, in)
	}
	return a.runLoop(ctx, events, in)
}

// Engine run paths (`engine_loop`): the hand-rolled loop and the ADK spine.
const (
	LoopLegacy = "legacy"
	LoopADK    = "adk"
)

// SetLoop selects the run path (config `engine_loop`). Unknown values are
// a typed error; the daemon applies it at construction.
func (a *Adapter) SetLoop(loop string) error {
	switch loop {
	case LoopLegacy, LoopADK:
		a.loop = loop
		return nil
	}
	return fmt.Errorf("engine: unknown engine_loop %q (want %q or %q)", loop, LoopADK, LoopLegacy)
}

// DefaultDepthCap bounds delegation depth until the daemon applies the
// configured `delegation.max_depth` (SetDepthCap); it mirrors the config
// default — the operator's hold on nesting — so a second constructor
// could never silently enable it.
const DefaultDepthCap = 1

// New creates the engine adapter. snapFn may be nil.
func New(provStore ProviderStore, st *store.Store, bus *eventbus.Bus, toolProv tools.Provider, snapFn func() registry.Snapshot) *Adapter {
	return &Adapter{
		provStore: provStore, st: st, bus: bus, toolProv: toolProv,
		snapFn: snapFn, depthCap: DefaultDepthCap, loop: LoopADK, effortOpt: DefaultEffortOption,
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
		in.requireToolUse = spec.RequireToolUse
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
		in.reasoningEffort = spec.ReasoningEffort
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
	rootExec, err := a.st.EnsureSession(sessionID, string(domain.SessionTypeResearch))
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
		// A hosted root run executes as the ward's agent: the ward's
		// own constitution + layout packet joins the prompt (sticky
		// binding pre-load included — EnsureWard ran before RunGoal).
		if section := activeWardSection(in); section != "" {
			in.systemPrompt = strings.TrimSpace(in.systemPrompt + "\n\n" + section)
		}
	}
	in.skills = req.Skills // progressive skills: the analysis's recommendations (D3)
	in.sessionID, in.execID, in.goal, in.token, in.prov = sessionID, rootExec, req.Goal, token, prov
	events := make(chan domain.TimelineEvent, 64)
	go a.run(runCtx, cancel, events, in)
	return events, nil
}

// runLoop is the LEGACY bounded loop for roots and children (the
// hand-rolled ReAct path kept selectable by `engine_loop: legacy` for one
// soak cycle; the ADK twin is runAgent): returns the final answer text
// ("" on failure/cancellation). Children reuse it via the delegate tool
// with their own execution record and wiring.
func (a *Adapter) runLoop(ctx context.Context, events chan domain.TimelineEvent, in runInput) (string, error) {
	s, err := a.prepareRun(ctx, events, in)
	if err != nil {
		return "", err
	}
	emit, chatModel := s.emit, s.chatModel

	// Progress-aware bounded loop (the operator's agentzero shape):
	// soft budget nudges, auto-extension for productive work, early
	// kill ONLY when the progress tracker says the agent is stuck.
	var tokensIn, tokensOut int64
	var childMsgs []*schema.Message // child's retained conversation (P1-2)
	// What the MODEL saw per tool-call occurrence (call id → contents in
	// order): the legacy root re-reads durable rows, which keep the REAL
	// outputs, so a PostToolUse block's replacement is overlaid from here.
	// Keyed by occurrence because a model may reuse a call id within a run.
	modelFacing := map[string][]string{}
	pendingNudge := "" // the require_tool_use nudge (first completion only) rides the next step
	softBudget, hardBudget, steps := budgetFor(in.maxSteps)
	tracker := progress.New() // per-run scoring
	for step := 0; step < steps; step++ {
		if ctx.Err() != nil {
			emit(domain.EventSession, "run cancelled")
			a.finishRun(ctx, events, in, "cancelled", "", tokensIn, tokensOut)
			return "", fmt.Errorf("engine: cancelled")
		}

		// Progress-aware steering (the agentzero shape):
		// 1. Kill only when genuinely stuck (warned + sinking score).
		if tracker.ShouldStop() {
			emit(domain.EventAgentStep, "engine: stopping — agent stuck (progress score "+fmt.Sprint(tracker.Score())+")")
			a.finishRun(ctx, events, in, "failed", "stuck: no progress", tokensIn, tokensOut)
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
		msgs, childMsgs, err = a.buildMessages(in, childMsgs, modelFacing)
		if err != nil {
			emit(domain.EventAgentStep, "engine: "+err.Error())
			a.finishRun(ctx, events, in, "failed", err.Error(), tokensIn, tokensOut)
			return "", err
		}
		// The require_tool_use nudge rides one user message (advisory
		// steering, like the progress nudges) and persists in a child's
		// retained conversation so later steps keep the instruction.
		if pendingNudge != "" {
			msgs = append(msgs, schema.UserMessage(pendingNudge))
			if in.isChild {
				childMsgs = append(childMsgs, schema.UserMessage(pendingNudge))
			}
			pendingNudge = ""
		}
		// Offer tools on all but the final step: the agent's allowlist
		// plus the delegate tool when this run's depth grant allows. The
		// offered set is ALSO the execution inventory (an allowlist binds
		// execution, not just the menu; the delegate tool is executable).
		var opts []model.Option
		var offered []tools.Tool
		if step < steps-1 && step < hardBudget-1 { // suppress tools only near the absolute ceiling
			offered = a.offeredTools(ctx, events, in)
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
		if err == nil && ctx.Err() != nil {
			// Cancelled while the model answered: never record that
			// answer as the run's success.
			emit(domain.EventSession, "run cancelled")
			a.finishRun(ctx, events, in, "cancelled", "", tokensIn, tokensOut)
			return "", fmt.Errorf("engine: cancelled: %w", ctx.Err())
		}
		if err != nil {
			if ctx.Err() != nil {
				emit(domain.EventSession, "run cancelled")
				a.finishRun(ctx, events, in, "cancelled", "", tokensIn, tokensOut)
				return "", fmt.Errorf("engine: cancelled: %w", ctx.Err())
			}
			emit(domain.EventAgentStep, "engine: model error: "+err.Error())
			a.finishRun(ctx, events, in, "failed", err.Error(), tokensIn, tokensOut)
			return "", fmt.Errorf("engine: model error: %w", err)
		}
		if out.ResponseMeta != nil && out.ResponseMeta.Usage != nil {
			tokensIn += int64(out.ResponseMeta.Usage.PromptTokens)
			tokensOut += int64(out.ResponseMeta.Usage.CompletionTokens)
		}

		if len(out.ToolCalls) == 0 {
			// The guard against narrated tool use (delegation-truthfulness):
			// an agent that declared require_tool_use and answered its
			// FIRST step without tools gets exactly one nudge — the answer
			// stays in the conversation, the instruction rides a user
			// message, and whatever comes next is final.
			if in.requireToolUse && step == 0 && ctx.Err() == nil {
				// Only the FIRST completion: an agent that did tool work
				// and then answers is finishing, not narrating.
				// The nudged answer stays in the transcript as an intent
				// row (ToolCalls "[]") — deliberately NOT a final-answer
				// row: surfaces render tool-free assistant rows as answers,
				// and a narrated non-answer must never pose as one.
				_, _ = a.st.Append(store.Message{
					ID: fmt.Sprintf("msg-nudge-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
					ExecutionID: in.execID, Role: "assistant", Agent: in.agent(), Content: out.Content, Reasoning: out.ReasoningContent,
					ToolCalls: "[]", Provider: in.providerName, Model: in.modelID,
					PromptTokens: usageOrNull(out.ResponseMeta, true), CompletionTokens: usageOrNull(out.ResponseMeta, false),
				})
				emit(domain.EventAgentStep, "engine: "+in.agent()+" answered without using any tools — nudged once (require_tool_use)")
				if in.isChild {
					childMsgs = append(childMsgs, out) // the child sees its own answer before the nudge
				}
				pendingNudge = toolUseNudge
				continue
			}
			// Final answer (with the step's reasoning — the model's
			// thinking for this step, as the chat path stores it).
			_, _ = a.st.Append(store.Message{
				ID: fmt.Sprintf("msg-final-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
				ExecutionID: in.execID, Role: "assistant", Agent: in.agent(), Content: out.Content, Reasoning: out.ReasoningContent,
				Provider: in.providerName, Model: in.modelID,
				PromptTokens: usageOrNull(out.ResponseMeta, true), CompletionTokens: usageOrNull(out.ResponseMeta, false),
			})
			emit(domain.EventAgentStep, out.Content)
			emit(domain.EventSession, "run completed")
			a.finishRun(ctx, events, in, "completed", "", tokensIn, tokensOut)
			return out.Content, nil
		}

		// Tool round: persist intent, execute, persist results.
		intentJSON, _ := json.Marshal(out.ToolCalls)
		_, _ = a.st.Append(store.Message{
			ID: fmt.Sprintf("msg-intent-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
			ExecutionID: in.execID, Role: "assistant", Agent: in.agent(), Content: out.Content, Reasoning: out.ReasoningContent,
			ToolCalls: string(intentJSON), Provider: in.providerName, Model: in.modelID,
			PromptTokens: usageOrNull(out.ResponseMeta, true), CompletionTokens: usageOrNull(out.ResponseMeta, false),
		})
		emit(domain.EventAgentStep, out.Content)
		if in.isChild {
			childMsgs = append(childMsgs, out)
		}

		for _, tc := range out.ToolCalls {
			// The shared tool-call seam: PreToolUse → the call → record + row
			// → PostToolUse (a configured block protects this loop too).
			content := a.executeTool(ctx, events, in, tracker, tc.Function.Name, tc.Function.Arguments, tc.ID, func() tools.Result {
				return execOffered(ctx, offered, tc.Function.Name, tc.Function.Arguments)
			})
			if !in.isChild {
				modelFacing[tc.ID] = append(modelFacing[tc.ID], content)
			}
			if in.isChild {
				childMsgs = append(childMsgs, &schema.Message{
					Role: schema.Tool, Content: content, ToolCallID: tc.ID, ToolName: tc.Function.Name,
				})
			}
		}
	}

	// Step bound reached.
	emit(domain.EventSession, "run stopped: step bound reached")
	a.finishRun(ctx, events, in, "failed", "step bound reached", tokensIn, tokensOut)
	return "", fmt.Errorf("engine: step bound reached")
}
