package eino

// Delegation machinery (add-engine-delegation): the depth-granted
// delegate_agent tool, the agent routing table, and the offered-inventory
// executor. Semantics live in adapter.go's runLoop; this file is the
// tooling it offers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wardtools"
)

// agentList names configured agents (error guidance).
// Delegation errors: distinguishable by callers (errors.Is).
var (
	// ErrUnknownAgent: the named agent is not configured.
	ErrUnknownAgent = errors.New("engine: unknown agent")
	// ErrNotDelegatable: the agent exists but has no description and is
	// therefore not offered for delegation.
	ErrNotDelegatable = errors.New("engine: agent not delegatable")
	// ErrToolNotOffered: the model named a tool outside this run's inventory.
	ErrToolNotOffered = errors.New("engine: tool not offered in this run")
	// ErrInvalidAgentModel: the agent's configured model reference is malformed.
	ErrInvalidAgentModel = errors.New("engine: invalid agent model reference")
)

func agentList(snap registry.Snapshot) string {
	if len(snap.Agents) == 0 {
		return "none configured"
	}
	names := make([]string, 0, len(snap.Agents))
	for n := range snap.Agents {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// delegateDescription builds the delegate tool's description: the routing
// table of delegatable agents (description required to be offered;
// system-role agents are host-run steps and never listed).
func delegateDescription(snap registry.Snapshot) string {
	names := delegatableNames(snap)
	var b strings.Builder
	b.WriteString("Delegate a self-contained task to a specialist agent and receive its final answer. Agents: ")
	for i, n := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s — %s", n, snap.Agents[n].Description)
	}
	b.WriteString(". Arguments: {\"agent\": \"name\", \"task\": \"what to do\"}. The child cannot be interrupted mid-answer; give it complete context.")
	return b.String()
}

// delegateTool builds this run's delegate_agent tool. It closes over the
// run context (cancellation propagates to children), the parent's input,
// and the live registry snapshot's routing table.
func (a *Adapter) delegateTool(ctx context.Context, events chan domain.TimelineEvent, parent runInput) tools.Tool {
	snap := a.snapFn()
	desc := delegateDescription(snap)
	return delegateTool{
		adapter: a, ctx: ctx, events: events, parent: parent, snap: snap, desc: desc,
	}
}

type delegateTool struct {
	adapter *Adapter
	ctx     context.Context
	events  chan domain.TimelineEvent
	parent  runInput
	snap    registry.Snapshot
	desc    string
	// target names the agent this tool delegates to — the per-agent task
	// tool of the ADK spine (decision D1: `delegate_<agent>`, args {task}).
	// Empty = the legacy dispatch tool (`delegate_agent`, args {agent,
	// task}): the legacy loop's surface, and on the spine still CALLABLE
	// for prompts that name it (routed through the unknown-tool handler).
	target string
}

// dispatchDelegateTool is the legacy dispatch tool's name.
const dispatchDelegateTool = "delegate_agent"

// delegatableNames lists the agents offered for delegation, sorted: a
// description is the eligibility contract; system-role agents are host-run
// steps and never workers.
func delegatableNames(snap registry.Snapshot) []string {
	names := make([]string, 0, len(snap.Agents))
	for n, spec := range snap.Agents {
		if spec.Description != "" && !registry.IsSystemAgent(n) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// delegateToolName is the per-agent tool's wire name: `delegate_` + the
// agent name VERBATIM. The shared prefix is what the UI recognizes as a
// delegation (legacy rows say `delegate_agent`), and the suffix is what
// it shows as the agent — so a name with characters outside
// [A-Za-z0-9_-] (`a.b`), or literally "agent" (the dispatch name), gets
// NO tool of its own (""; perAgentDelegationTools reports it): the UI
// would otherwise attribute the child's rows to a rewritten name. Such
// agents stay reachable through the dispatch alias.
func delegateToolName(agent string) string {
	if agent == "agent" || agent == "" {
		return ""
	}
	for _, r := range agent {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return ""
		}
	}
	return "delegate_" + agent
}

// perAgentDelegationTools builds one task tool per delegatable agent
// (ADK spine, decision D1). An agent with no usable wire name — a name
// that is not verbatim-safe, "agent", or one that collides with an earlier
// agent's — is skipped and reported in the second list (the driver names
// the drops on the stream); it stays reachable through the dispatch alias.
func (a *Adapter) perAgentDelegationTools(ctx context.Context, events chan domain.TimelineEvent, parent runInput) (tools_ []tools.Tool, collided []string) {
	snap := a.snapFn()
	taken := map[string]bool{dispatchDelegateTool: true}
	var out []tools.Tool
	for _, name := range delegatableNames(snap) {
		wire := delegateToolName(name)
		if wire == "" || taken[wire] {
			collided = append(collided, name)
			continue
		}
		taken[wire] = true
		out = append(out, delegateTool{adapter: a, ctx: ctx, events: events, parent: parent, snap: snap, target: name})
	}
	return out, collided
}

// refreshed re-reads the registry snapshot: on the ADK path the tool is
// built once per run (the legacy loop rebuilt it every step), and a
// hot-reload during a long run must not leave a removed or changed agent
// invocable. The routing table (description) is refreshed with it.
func (d delegateTool) refreshed() delegateTool {
	if d.adapter.snapFn != nil {
		d.snap = d.adapter.snapFn()
		d.desc = delegateDescription(d.snap)
	}
	return d
}

func (d delegateTool) Def() tools.Def {
	name, desc, params := dispatchDelegateTool, d.refreshed().desc, `{"type":"object","properties":{"agent":{"type":"string","description":"agent name"},"task":{"type":"string","description":"complete, self-contained task"}},"required":["agent","task"]}`
	if d.target != "" {
		if spec, ok := d.refreshed().snap.Agents[d.target]; ok {
			desc = spec.Description
		}
		name = delegateToolName(d.target)
		params = `{"type":"object","properties":{"task":{"type":"string","description":"complete, self-contained task"}},"required":["task"]}`
	}
	if d.parent.wardHost != nil && d.parent.wardHost.Active() != nil {
		// The ward guard: children act INSIDE the ward through the file
		// and shell tools; entering/switching/creating wards is the
		// root's structural call (children don't get ward_use/create).
		desc += "\n\nWard: this run is hosted in a ward. Delegated agents work inside it (read/write/list/shell); they cannot enter, switch, or create wards — that is the root's call. State the ward and the task's file targets in the task text."
	}
	return tools.Def{Name: name, Description: desc + "\n\nDelegate a self-contained task to this agent and receive its final answer. The child cannot be interrupted mid-answer; give it complete context.", ParamsJSON: params}
}

// Exec runs the named agent as a CHILD of the parent's execution: its own
// record (parent linkage), its agent wiring, depth+1, steps surfaced as
// delegation events, final answer folded back as the tool result.
func (d delegateTool) Exec(_ context.Context, argsJSON string) tools.Result {
	d = d.refreshed()
	var args struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Task == "" {
		return tools.Result{Err: fmt.Errorf("%s: arguments must be {%s, task}: %w", d.Def().Name, map[bool]string{true: "task", false: "agent"}[d.target != ""], err)}
	}
	if d.target != "" {
		args.Agent = d.target
	} else if args.Agent == "" {
		return tools.Result{Err: fmt.Errorf("%s: arguments must be {agent, task}", dispatchDelegateTool)}
	}
	spec, ok := d.snap.Agents[args.Agent]
	if !ok {
		return tools.Result{Err: fmt.Errorf("delegate_agent: %w %q (configured: %s)", ErrUnknownAgent, args.Agent, agentList(d.snap))}
	}
	if registry.IsSystemAgent(args.Agent) {
		// A system-role agent (the intent analyst) is a host-run step,
		// never a worker — whatever its description says.
		return tools.Result{Err: fmt.Errorf("delegate_agent: %w %q (system agent; run by the host, not delegatable)", ErrNotDelegatable, args.Agent)}
	}
	if spec.Description == "" {
		// Not offered for delegation: the description IS the eligibility
		// contract (routing table and invocation agree).
		return tools.Result{Err: fmt.Errorf("delegate_agent: %w %q (no description; usable via goal requests only)", ErrNotDelegatable, args.Agent)}
	}
	child := runInput{
		sessionID: d.parent.sessionID, goal: args.Task,
		providerName: d.parent.providerName, modelID: d.parent.modelID,
		token: d.parent.token, prov: d.parent.prov,
		systemPrompt: spec.SystemPrompt, toolAllow: spec.Tools,
		agentName: spec.Name, depth: d.parent.depth + 1,
		grant: spec.MaxDepth, isChild: true, requireToolUse: spec.RequireToolUse,
		reasoningEffort: spec.ReasoningEffort, skills: d.parent.skills,
		wardHost: d.parent.wardHost, // read-only inheritance: children act inside the ward (file tools), structural stays root-only
	}
	// A hosted child executes as the ward's agent too: the same
	// ## Active Ward section the root composes joins its prompt (the
	// NVDA run's children had to `read` AGENTS.md manually before this).
	if section := activeWardSection(child); section != "" {
		child.systemPrompt = strings.TrimSpace(child.systemPrompt + "\n\n" + section)
	}
	if spec.Model != "" {
		p, m, found := strings.Cut(spec.Model, "/")
		if !found || p == "" || m == "" {
			return tools.Result{Err: fmt.Errorf("delegate_agent: agent %q: %w %q", args.Agent, ErrInvalidAgentModel, spec.Model)}
		}
		prov, token, err := d.adapter.provStore.ResolvedLookup(d.ctx, p)
		if err != nil {
			return tools.Result{Err: fmt.Errorf("delegate_agent: agent %q model %q: %w", args.Agent, spec.Model, err)}
		}
		child.providerName, child.modelID, child.token, child.prov = p, m, token, prov
	}
	childExec, err := d.adapter.st.CreateChildExecution(d.parent.sessionID, d.parent.execID, spec.Name)
	if err != nil {
		return tools.Result{Err: fmt.Errorf("delegate_agent: child execution: %w", err)}
	}
	child.execID = childExec
	// The delegation is a Timeline node: started when the child's
	// execution exists, completed with its cost when the run ends.
	started := time.Now()
	d.adapter.emitDelegation(d.ctx, d.events, d.parent, domain.EventDelegationStarted,
		&domain.DelegationPayload{ChildAgent: spec.Name, Task: args.Task, ExecutionID: childExec},
		fmt.Sprintf("Delegated to %s: %s", spec.Name, firstLine(args.Task)))
	answer, err := d.adapter.runChild(d.ctx, d.events, child)
	// The stored terminal status wins (a cancelled child reads
	// "cancelled"); "failed" only when no reliable status exists.
	tokensIn, tokensOut, status, uerr := d.adapter.st.ExecutionUsage(childExec)
	if uerr != nil || status == "running" {
		status = "failed"
	}
	// SubagentStop fires when the child returns (advisory): a block or warn
	// appends its reason to the fold-back so the root reads it beside the
	// report; the decision is attributed to the child.
	stop := d.adapter.runHook(d.ctx, d.events, child, domain.HookEvent{Event: domain.HookSubagentStop, SessionID: d.parent.sessionID,
		Payload: map[string]any{"agent": spec.Name, "status": status, "execution_id": childExec}})
	advisory := stop.Action != domain.HookAllow && stop.Reason != ""
	if advisory {
		answer += "\n\nHook — " + stop.Reason // the reason already names its hooks
	}
	done := &domain.DelegationPayload{ChildAgent: spec.Name, Task: args.Task, ExecutionID: childExec,
		TokensIn: tokensIn, TokensOut: tokensOut, DurationMS: time.Since(started).Milliseconds(), Status: status}
	d.adapter.emitDelegation(d.ctx, d.events, d.parent, domain.EventDelegationCompleted, done,
		fmt.Sprintf("%s finished (%s) · %d tokens in / %d out · %d ms", spec.Name, status, tokensIn, tokensOut, done.DurationMS))
	// The parent resumes: host-level events (a ward transition the root
	// makes next) must be attributed to the parent again, not the child.
	d.adapter.setRunAttribution(d.parent.sessionID, d.parent)
	if err != nil {
		if advisory { // a failed child's advisory still reaches the parent
			return tools.Result{Err: fmt.Errorf("delegate_agent: %s: %w (hook — %s)", args.Agent, err, stop.Reason)}
		}
		return tools.Result{Err: fmt.Errorf("delegate_agent: %s: %w", args.Agent, err)}
	}
	return tools.Result{Output: answer + deliverableReceipts(d.parent, args.Task)}
}

// deliverablePathPattern matches the shape a task's OUTPUT section
// names — "sources/x.md", "outputs/brief.md", or a bare "report.md" —
// with an alphabetic extension (so version strings like "glm-5.3" stay
// out). URLs are stripped from the task before matching.
var deliverablePathPattern = regexp.MustCompile(`\b([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*\.[A-Za-z]{2,8})\b`)

// urlPattern removes URLs from the task text before receipt matching.
var urlPattern = regexp.MustCompile(`(?:https?|ftp)://\S+`)

// deliverableReceipts appends host-verified lines for every path the
// task named: present inside the active ward (✓) or not (✗) — so the
// root reads the truth next to the child's claims instead of spending
// steps on it. An EXISTENCE check at fold-back, not proof the child
// created the file (a task-named input that already exists reads ✓).
// Only inside a hosted ward; nothing to check otherwise.
func deliverableReceipts(parent runInput, task string) string {
	if parent.wardHost == nil || parent.wardHost.Active() == nil {
		return ""
	}
	files := wardtools.FileTools{Host: parent.wardHost}
	seen := map[string]bool{}
	var lines []string
	for _, p := range deliverablePathPattern.FindAllString(urlPattern.ReplaceAllString(task, ""), -1) {
		if seen[p] {
			continue
		}
		seen[p] = true
		mark := "✗ not on disk"
		if ok, err := files.Exists(p); err == nil && ok {
			mark = "✓ on disk"
		}
		lines = append(lines, "- "+p+" "+mark)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\nDeliverables check (host; existence at fold-back, not proof of creation):\n" + strings.Join(lines, "\n")
}

// firstLine bounds the task text on the started event's line.
func firstLine(task string) string {
	line, _, _ := strings.Cut(task, "\n")
	line = strings.TrimSpace(line)
	if len(line) > 120 {
		line = line[:120] + "…"
	}
	if line == "" {
		line = "(task)"
	}
	return line
}

// emitDelegation persists, publishes and streams one delegation
// lifecycle event into the PARENT's stream (attempted-durable-first,
// the engine's posture). The event's execution is the CHILD's.
func (a *Adapter) emitDelegation(ctx context.Context, events chan domain.TimelineEvent, parent runInput, kind domain.TimelineEventKind, p *domain.DelegationPayload, text string) {
	evt := domain.TimelineEvent{ID: a.idGen(), SessionID: parent.sessionID, ExecutionID: p.ExecutionID, Kind: kind, Agent: p.ChildAgent, Text: text, Delegation: p}
	payload, _ := json.Marshal(map[string]any{"text": text, "agent": p.ChildAgent, "delegation": p})
	_ = a.st.AppendEvent(store.Event{ //nolint:errcheck // attempted-durable-first (F-15 posture)
		ID: evt.ID, SessionID: parent.sessionID, ExecutionID: p.ExecutionID, Kind: string(kind), Payload: string(payload),
	})
	if a.bus != nil {
		_ = a.bus.Publish(context.Background(), evt) // ctx-ok: F-15 fire-and-forget emit
	}
	select {
	case events <- evt:
	case <-ctx.Done():
	}
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

// paramsOneOfFromJSON converts a tool's JSON-Schema parameters object
// into eino's parameter shape, preserving required flags, types, and
// descriptions. An empty/invalid schema degrades to no parameters (the
// pre-delegation behavior) — tool defs are internal constants.
func paramsOneOfFromJSON(paramsJSON string) *schema.ParamsOneOf {
	var raw struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal([]byte(paramsJSON), &raw); err != nil || len(raw.Properties) == 0 {
		return schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})
	}
	req := map[string]bool{}
	for _, r := range raw.Required {
		req[r] = true
	}
	out := map[string]*schema.ParameterInfo{}
	for name, rawParam := range raw.Properties {
		if pi := paramInfoFromJSON(rawParam, req[name]); pi != nil {
			out[name] = pi
		}
	}
	return schema.NewParamsOneOfByParams(out)
}

// paramInfoFromJSON converts one JSON-Schema property (including nested
// object sub-parameters and number typing) into eino's parameter shape.
func paramInfoFromJSON(raw json.RawMessage, required bool) *schema.ParameterInfo {
	var p struct {
		Type        string                     `json:"type"`
		Description string                     `json:"description"`
		Properties  map[string]json.RawMessage `json:"properties"`
		Required    []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Type == "" {
		return nil
	}
	pi := &schema.ParameterInfo{Desc: p.Description, Required: required}
	switch p.Type {
	case "integer":
		pi.Type = schema.Integer
	case "number":
		pi.Type = schema.Number
	case "boolean":
		pi.Type = schema.Boolean
	case "object":
		pi.Type = schema.Object
		subReq := map[string]bool{}
		for _, r := range p.Required {
			subReq[r] = true
		}
		for subName, subRaw := range p.Properties {
			if sub := paramInfoFromJSON(subRaw, subReq[subName]); sub != nil {
				if pi.SubParams == nil {
					pi.SubParams = map[string]*schema.ParameterInfo{}
				}
				pi.SubParams[subName] = sub
			}
		}
	default:
		pi.Type = schema.String
	}
	return pi
}
