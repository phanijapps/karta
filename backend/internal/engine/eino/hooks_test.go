package eino

// The hook seams with the REAL runner (internal/hooks) over manifests in a
// temp dir (hook-runner specs: hooks, agent-engine, engine-delegation).

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/compaction"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/hooks"
	"zbot/backend/internal/providers"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// hookedAdapter builds an ADK-path adapter whose hook roster is the given
// manifests (paths in a temp dir, so `sh -c` has a cwd).
func hookedAdapter(t *testing.T, gen chatGenerator, snapFn func() registry.Snapshot, manifests map[string]registry.HookSpec) *Adapter {
	t.Helper()
	a := newBudgetAdapter(t, func(string) chatGenerator { return gen }, snapFn)
	dir := t.TempDir()
	for k, h := range manifests {
		h.Path = dir + "/" + k + ".yaml"
		manifests[k] = h
	}
	a.SetHookRunner(&hooks.Runner{Snapshot: func() registry.Snapshot { return registry.Snapshot{Hooks: manifests} }})
	return a
}

// countingTool records executions.
type countingTool struct {
	name  string
	execs *int32
}

func (c countingTool) Def() tools.Def {
	return tools.Def{Name: c.name, Description: "test " + c.name, ParamsJSON: `{"type":"object","properties":{}}`}
}

func (c countingTool) Exec(context.Context, string) tools.Result {
	atomic.AddInt32(c.execs, 1)
	return tools.Result{Output: "ok"}
}

func toolRows(t *testing.T, a *Adapter, session string) []store.Message {
	t.Helper()
	msgs, _ := a.st.Replay(session, 0, 100)
	var rows []store.Message
	for _, m := range msgs {
		if m.Role == "tool" {
			rows = append(rows, m)
		}
	}
	return rows
}

func hookEvents(events []domain.TimelineEvent, kind domain.TimelineEventKind) []domain.TimelineEvent {
	var out []domain.TimelineEvent
	for _, e := range events {
		if e.Kind == kind && e.Hook != nil {
			out = append(out, e)
		}
	}
	return out
}

func durableHooks(t *testing.T, a *Adapter, session string) []store.Event {
	t.Helper()
	all, _ := a.st.Events(session, "", 200)
	var out []store.Event
	for _, e := range all {
		if e.Kind == string(domain.EventHookDecision) {
			out = append(out, e)
		}
	}
	return out
}

// TestHookPreToolUseBlockNeverExecutes: a matching PreToolUse block stops the
// call before execution; the model sees the reason as a recoverable error;
// the tool_call row/event carry the blocked text; the decision is durable.
func TestHookPreToolUseBlockNeverExecutes(t *testing.T) {
	t.Parallel()
	var execs int32
	big := countingTool{name: "ward_write", execs: &execs}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-hook-pre": {{toolCall: "ward_write", args: `{"path":"x"}`}, {content: "done"}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{
		"pre-block": {Event: domain.HookPreToolUse, Matcher: regexp.MustCompile(`^ward_`), Command: "echo 'no ward writes' >&2; exit 2"},
	})
	a.toolProv = &liveProvider{ts: []tools.Tool{big}}
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-pre", Goal: "adk-hook-pre goal", Model: "prov/m1"}))
	if atomic.LoadInt32(&execs) != 0 {
		t.Fatalf("a blocked call must never execute: %d executions", execs)
	}
	res := lastToolResult(gen.calls[1])
	if res == nil || res.Content != "error: blocked by hook: pre-block: no ward writes" {
		t.Fatalf("the model must see the hook's reason as a recoverable error: %q", res.Content)
	}
	if rows := toolRows(t, a, "adk-hook-pre"); len(rows) != 1 || rows[0].Content != "error: blocked by hook: pre-block: no ward writes" {
		t.Fatalf("the tool_call row records the blocked text: %+v", rows)
	}
	var block *domain.TimelineEvent
	for i := range events {
		if strings.HasPrefix(events[i].Text, "hook PreToolUse ward_write: block") {
			block = &events[i]
		}
	}
	if block == nil || block.Agent != domain.AgentRoot || !strings.Contains(block.Text, "pre-block: no ward writes") {
		t.Fatalf("the block decision is on the stream: %+v", block)
	}
	if keys, _ := block.Hook.Context["hooks"].([]string); strings.Join(keys, ",") != "pre-block" {
		t.Fatalf("context names the hook: %v", block.Hook.Context)
	}
	var blockOnRecord bool
	for _, e := range durableHooks(t, a, "adk-hook-pre") {
		if strings.Contains(e.Payload, `"action":"block"`) && strings.Contains(e.Payload, "no ward writes") {
			blockOnRecord = true
		}
	}
	if !blockOnRecord {
		t.Fatalf("the block is durable: %+v", durableHooks(t, a, "adk-hook-pre"))
	}
}

// TestHookPostToolUseBlockHidesOutputOnly: the tool runs, the model sees the
// hook's reason, the durable row keeps the real output.
func TestHookPostToolUseBlockHidesOutputOnly(t *testing.T) {
	t.Parallel()
	var execs int32
	probe := countingTool{name: "probe", execs: &execs}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-hook-post": {{toolCall: "probe", args: `{}`}, {content: "done"}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{
		"post-block": {Event: domain.HookPostToolUse, Command: "echo 'redacted by policy' >&2; exit 2"},
	})
	a.toolProv = &liveProvider{ts: []tools.Tool{probe}}
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-post", Goal: "adk-hook-post goal", Model: "prov/m1"}))
	if atomic.LoadInt32(&execs) != 1 {
		t.Fatalf("a post-hook block runs after execution: %d executions", execs)
	}
	if res := lastToolResult(gen.calls[1]); res == nil || res.Content != "error: blocked by hook: post-block: redacted by policy" {
		t.Fatalf("the model must see the hook's reason: %q", res.Content)
	}
	if rows := toolRows(t, a, "adk-hook-post"); len(rows) != 1 || rows[0].Content != "ok" {
		t.Fatalf("the durable row keeps the real output: %+v", rows)
	}
}

// TestHookSessionStartAndEnd: SessionStart fires once at the root run's
// start (an allow is on the record), SessionEnd once at the terminal with
// the status; a non-zero, non-2 exit warns.
func TestHookSessionStartAndEnd(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-hook-session": {{content: "done"}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{
		"start":  {Event: domain.HookSessionStart, Command: "exit 0"},
		"end":    {Event: domain.HookSessionEnd, Command: "echo 'audit lag' >&2; exit 3"},
		"ignore": {Event: domain.HookPreCompact, Matcher: regexp.MustCompile(`^never-matches`), Command: "exit 2"},
	})
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-session", Goal: "adk-hook-session goal", Model: "prov/m1"}))
	decisions := hookEvents(events, domain.EventHookDecision)
	if len(decisions) != 2 {
		t.Fatalf("SessionStart + SessionEnd decisions: %+v", decisions)
	}
	if d := decisions[0]; d.Hook.Action != domain.HookAllow || !strings.HasPrefix(d.Text, "hook SessionStart: allow") {
		t.Fatalf("the allow is on the record: %+v", d)
	}
	if d := decisions[1]; d.Hook.Action != domain.HookWarn || !strings.Contains(d.Text, "hook SessionEnd: warn — end: audit lag") {
		t.Fatalf("the end warns: %+v", d)
	}
	var goalSeen, statusSeen bool
	for _, e := range durableHooks(t, a, "adk-hook-session") {
		if strings.Contains(e.Payload, `"goal":"adk-hook-session goal"`) {
			goalSeen = true
		}
		if strings.Contains(e.Payload, `"status":"completed"`) {
			statusSeen = true
		}
	}
	if !goalSeen || !statusSeen {
		t.Fatalf("payloads carry the goal and the status: %+v", durableHooks(t, a, "adk-hook-session"))
	}
	// SessionStart/End fire once per ROOT run even when it fails.
	gen2 := &gateGen{entered: make(chan struct{}), gate: make(chan struct{})}
	a2 := hookedAdapter(t, gen2, nil, map[string]registry.HookSpec{"end": {Event: domain.HookSessionEnd, Command: "exit 0"}})
	ch := mustRun(t, a2, domain.GoalRequest{SessionID: "adk-hook-cancel", Goal: "adk-hook-cancel goal", Model: "prov/m1"})
	<-gen2.entered
	_ = a2.Cancel(context.Background(), "adk-hook-cancel")
	close(gen2.gate)
	drainEvents(t, ch)
	var ends int
	for _, e := range durableHooks(t, a2, "adk-hook-cancel") {
		if strings.Contains(e.Payload, `"status":"cancelled"`) {
			ends++
		}
	}
	if ends != 1 {
		t.Fatalf("SessionEnd fires exactly once at the cancelled terminal: %d", ends)
	}
}

// TestHookSubagentStopAdvisory: a SubagentStop warning is appended to the
// fold-back so the root reads it beside the report; the decision is
// attributed to the child and surfaces as a delegation-kind event.
func TestHookSubagentStopAdvisory(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{
		"adk-hook-stop": {{toolCall: "delegate_researcher", args: `{"task":"child-task stop"}`}, {content: "root done"}},
		"child-task":    {{content: "child report"}},
	}
	a := hookedAdapter(t, gen, agentSnap, map[string]registry.HookSpec{
		"stop-research": {Event: domain.HookSubagentStop, Matcher: regexp.MustCompile(`^research`), Command: "echo 'verify the numbers' >&2; exit 2"},
	})
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-stop", Goal: "adk-hook-stop goal", Model: "prov/m1"}))
	res := lastToolResult(gen.calls[len(gen.calls)-1])
	if res == nil || !strings.HasPrefix(res.Content, "child report") || !strings.Contains(res.Content, "Hook — stop-research: verify the numbers") {
		t.Fatalf("the fold-back carries the hook's reason after the report: %q", res.Content)
	}
	var childDecision *domain.TimelineEvent
	for i := range events {
		if events[i].Hook != nil && strings.HasPrefix(events[i].Text, "hook SubagentStop") {
			childDecision = &events[i]
		}
	}
	var childExec string
	for _, evt := range events {
		if evt.Kind == domain.EventDelegationStarted && evt.Delegation != nil {
			childExec = evt.Delegation.ExecutionID
		}
	}
	if childDecision == nil || childDecision.Kind != domain.EventDelegation || childDecision.Agent != "researcher" || childExec == "" || childDecision.ExecutionID != childExec {
		t.Fatalf("the child's decision surfaces as a delegation event attributed to the child's execution: %+v (child %s)", childDecision, childExec)
	}
	var onRecord bool
	for _, e := range durableHooks(t, a, "adk-hook-stop") {
		if e.ExecutionID == childDecision.ExecutionID && strings.Contains(e.Payload, `"action":"block"`) {
			onRecord = true
		}
	}
	if !onRecord {
		t.Fatal("the durable hook_decision row sits under the child's execution")
	}
}

// TestHookPostToolUseSeesTheOutput: the post hook's stdin carries the tool's
// output under payload.output (gate F9).
func TestHookPostToolUseSeesTheOutput(t *testing.T) {
	t.Parallel()
	var execs int32
	probe := countingTool{name: "probe", execs: &execs}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-hook-payload": {{toolCall: "probe", args: `{}`}, {content: "done"}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{
		"saw-ok": {Event: domain.HookPostToolUse, Command: `grep -q '"output":"ok"' && { echo 'saw the output' >&2; exit 3; }; exit 0`},
	})
	a.toolProv = &liveProvider{ts: []tools.Tool{probe}}
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-payload", Goal: "adk-hook-payload goal", Model: "prov/m1"}))
	var warned bool
	for _, e := range hookEvents(events, domain.EventHookDecision) {
		if e.Hook.Action == domain.HookWarn && strings.Contains(e.Hook.Reason, "saw the output") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("the post hook reads payload.output: %+v", hookEvents(events, domain.EventHookDecision))
	}
}

// TestHookBlockedDelegationNeverStartsAChild: a PreToolUse block on a
// delegation tool creates no child execution (gate F9).
func TestHookBlockedDelegationNeverStartsAChild(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{
		"adk-hook-deleg": {{toolCall: "delegate_researcher", args: `{"task":"child-task never"}`}, {content: "root done"}},
		"child-task":     {{content: "should not run"}},
	}
	a := hookedAdapter(t, gen, agentSnap, map[string]registry.HookSpec{
		"no-delegation": {Event: domain.HookPreToolUse, Matcher: regexp.MustCompile(`^delegate_`), Command: "echo 'delegation is off today' >&2; exit 2"},
	})
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-deleg", Goal: "adk-hook-deleg goal", Model: "prov/m1"}))
	for _, evt := range events {
		if evt.Kind == domain.EventDelegationStarted {
			t.Fatalf("a blocked delegation must not start a child: %+v", evt)
		}
	}
	if res := lastToolResult(gen.calls[1]); res == nil || res.Content != "error: blocked by hook: no-delegation: delegation is off today" {
		t.Fatalf("the root sees the hook's reason: %q", res.Content)
	}
	if children, _ := store.QueryChildExecutions(a.st, "adk-hook-deleg"); len(children) != 0 {
		t.Fatalf("no child execution: %v", children)
	}
}

// TestHookLegacyLoopSessionEnd: the legacy loop ends through finishRun too.
func TestHookLegacyLoopSessionEnd(t *testing.T) {
	t.Parallel()
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{"adk-legacy-hook": {{content: "done"}}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{"end": {Event: domain.HookSessionEnd, Command: "exit 0"}})
	if err := a.SetLoop(LoopLegacy); err != nil {
		t.Fatal(err)
	}
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-legacy-hook", Goal: "adk-legacy-hook goal", Model: "prov/m1"}))
	var ends int
	for _, e := range durableHooks(t, a, "adk-legacy-hook") {
		if strings.Contains(e.Payload, `"status":"completed"`) {
			ends++
		}
	}
	if ends != 1 {
		t.Fatalf("the legacy loop raises SessionEnd once: %d", ends)
	}
}

// TestHookPreCompactDecisionIsEmitted: the bridge raises PreCompact through
// runHook — a blocking manifest is a durable, attributed decision and the
// pass is skipped (gate F1).
func TestHookPreCompactDecisionIsEmitted(t *testing.T) {
	t.Parallel()
	big := namedTool{name: "big", out: strings.Repeat("word ", 300)}
	policy := &compaction.Service{Summarizer: &fakeSummarizer{}, KeepRecent: 2}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-hook-compact": {{toolCall: "big", args: `{"n":1}`}, {toolCall: "big", args: `{"n":2}`}, {toolCall: "big", args: `{"n":3}`}, {content: "done"}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{"no-compact": {Event: domain.HookPreCompact, Command: "echo 'not now' >&2; exit 2"}})
	a.toolProv = &liveProvider{ts: []tools.Tool{big}}
	a.SetRunOptions(RunOptions{SummarizeAt: 500})
	a.SetCompactorFactory(func(p, m string) domain.Compactor { return policy.For(compaction.ModelRef{Provider: p, Model: m}) })
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-compact", Goal: "adk-hook-compact goal", Model: "prov/m1"}))
	var block *domain.TimelineEvent
	var skipped bool
	for i := range events {
		if events[i].Hook != nil && strings.HasPrefix(events[i].Text, "hook PreCompact: block") {
			block = &events[i]
		}
		if strings.Contains(events[i].Text, "blocked by a PreCompact hook — no-compact: not now") {
			skipped = true
		}
	}
	if block == nil || block.Agent != domain.AgentRoot || !skipped {
		t.Fatalf("the PreCompact block is an attributed decision and skips the pass: %+v skipped=%v", block, skipped)
	}
	var onRecord bool
	for _, e := range durableHooks(t, a, "adk-hook-compact") {
		if strings.Contains(e.Payload, `"action":"block"`) && strings.Contains(e.Payload, `"PreCompact"`) {
			onRecord = true
		}
	}
	if !onRecord {
		t.Fatal("the PreCompact decision is durable")
	}
}

// TestHookTerminalHookRunsOnCancellation (gate F2): a SessionEnd command
// with a side effect runs when the run was cancelled.
func TestHookTerminalHookRunsOnCancellation(t *testing.T) {
	t.Parallel()
	marker := t.TempDir() + "/audit-ran"
	gen := &gateGen{entered: make(chan struct{}), gate: make(chan struct{})}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{"audit": {Event: domain.HookSessionEnd, Command: "echo ended > " + marker}})
	ch := mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-audit", Goal: "adk-hook-audit goal", Model: "prov/m1"})
	<-gen.entered
	_ = a.Cancel(context.Background(), "adk-hook-audit")
	close(gen.gate)
	drainEvents(t, ch)
	if raw, err := os.ReadFile(marker); err != nil || strings.TrimSpace(string(raw)) != "ended" {
		t.Fatalf("the terminal hook must run despite cancellation: %q %v", raw, err)
	}
}

// TestHookFailedChildsAdvisoryReachesTheParent (gate R5): when the child
// fails, the SubagentStop reason still reaches the root — on the error.
func TestHookFailedChildsAdvisoryReachesTheParent(t *testing.T) {
	t.Parallel()
	gen := &stepGen{}
	gen.next = func(call int, msgs []*schema.Message) *schema.Message {
		if call == 0 {
			return toolCallMsg("delegate_researcher", `{"task":"child-task doom"}`)
		}
		return &schema.Message{Content: "root done"}
	}
	// A generator that errors for the child: wrap stepGen with a failing child.
	failing := &failingChildGen{stepGen: gen}
	a := hookedAdapter(t, failing, agentSnap, map[string]registry.HookSpec{
		"stop-research": {Event: domain.HookSubagentStop, Matcher: regexp.MustCompile(`^research`), Command: "echo 'the child failed — check the report' >&2; exit 3"},
	})
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-fail", Goal: "adk-hook-fail goal", Model: "prov/m1"}))
	for _, evt := range events {
		if evt.Kind == domain.EventToolCall && evt.ToolCall != nil && evt.ToolCall.Tool == "delegate_researcher" {
			if !strings.Contains(evt.ToolCall.Output, "hook — stop-research: the child failed") {
				t.Fatalf("the failed child's advisory reaches the root on the error: %q", evt.ToolCall.Output)
			}
			return
		}
	}
	t.Fatalf("no delegation tool result: %+v", events)
}

// failingChildGen errors on the second model call (the child's first).
type failingChildGen struct{ *stepGen }

func (f *failingChildGen) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.mu.Lock()
	call := len(f.calls)
	f.mu.Unlock()
	if call == 1 {
		return nil, errors.New("fake: child model error")
	}
	return f.stepGen.Generate(ctx, msgs, opts...)
}

// TestHookSessionStartThenFailedEnd (gate R6): when the model factory
// fails, the run still has a SessionStart and a failed SessionEnd.
func TestHookSessionStartThenFailedEnd(t *testing.T) {
	t.Parallel()
	gen := &stepGen{}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{
		"start": {Event: domain.HookSessionStart, Command: "exit 0"},
		"end":   {Event: domain.HookSessionEnd, Command: "exit 0"},
	})
	a.newModel = func(context.Context, providers.Provider, string, string) (chatGenerator, error) {
		return nil, errors.New("fake: no model")
	}
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-hook-nomodel", Goal: "adk-hook-nomodel goal", Model: "prov/m-fail"}))
	durable := durableHooks(t, a, "adk-hook-nomodel")
	var order []string
	for _, e := range durable {
		if strings.Contains(e.Payload, `"SessionStart"`) {
			order = append(order, "start")
		}
		if strings.Contains(e.Payload, `"status":"failed"`) {
			order = append(order, "end")
		}
	}
	if strings.Join(order, ",") != "start,end" {
		t.Fatalf("one start THEN one failed end, in that order: %v (%+v)", order, durable)
	}
}

// TestHookBlockedLegacyCall (gate R8): the legacy loop's tool calls pass
// through the seam too — a configured block protects both paths.
func TestHookBlockedLegacyCall(t *testing.T) {
	t.Parallel()
	var execs int32
	probe := countingTool{name: "probe", execs: &execs}
	gen := &routingGenerator{scripts: map[string][]fakeGenStep{"adk-legacy-block": {{toolCall: "probe", args: `{}`}, {content: "done"}}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{"no-probe": {Event: domain.HookPreToolUse, Matcher: regexp.MustCompile(`^probe$`), Command: "echo 'probe is retired' >&2; exit 2"}})
	a.toolProv = &liveProvider{ts: []tools.Tool{probe}}
	if err := a.SetLoop(LoopLegacy); err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-legacy-block", Goal: "adk-legacy-block goal", Model: "prov/m1"}))
	if atomic.LoadInt32(&execs) != 0 {
		t.Fatalf("a blocked legacy call never executes: %d", execs)
	}
	if rows := toolRows(t, a, "adk-legacy-block"); len(rows) != 1 || rows[0].Content != "error: blocked by hook: no-probe: probe is retired" {
		t.Fatalf("the legacy tool row records the blocked text: %+v", rows)
	}
	if len(hookEvents(events, domain.EventHookDecision)) < 2 {
		t.Fatalf("decisions on the stream: %+v", events)
	}
}

// TestHookPostBlockOnTheLegacyRoot (gate R12): the legacy root re-reads
// durable rows — a PostToolUse block is overlaid on the next request while
// the row keeps the real output.
func TestHookPostBlockOnTheLegacyRoot(t *testing.T) {
	t.Parallel()
	var execs int32
	probe := countingTool{name: "probe", execs: &execs}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-legacy-post": {{toolCall: "probe", args: `{}`}, {content: "done"}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{"post-block": {Event: domain.HookPostToolUse, Command: "echo 'redacted' >&2; exit 2"}})
	a.toolProv = &liveProvider{ts: []tools.Tool{probe}}
	if err := a.SetLoop(LoopLegacy); err != nil {
		t.Fatal(err)
	}
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-legacy-post", Goal: "adk-legacy-post goal", Model: "prov/m1"}))
	if res := lastToolResult(gen.calls[1]); res == nil || res.Content != "error: blocked by hook: post-block: redacted" {
		t.Fatalf("the legacy root's next request must carry the hook's verdict: %q", res.Content)
	}
	if rows := toolRows(t, a, "adk-legacy-post"); len(rows) != 1 || rows[0].Content != "ok" {
		t.Fatalf("the durable row keeps the real output: %+v", rows)
	}
}

// TestHookOverlayIsKeyedByOccurrence (gate G5): a model reusing a call id
// within a legacy root run gets each occurrence's own verdict — an allowed
// result never inherits an earlier block, nor the reverse.
func TestHookOverlayIsKeyedByOccurrence(t *testing.T) {
	t.Parallel()
	var execs int32
	probe := countingTool{name: "probe", execs: &execs}
	dir := t.TempDir()
	counter := dir + "/n"
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-legacy-occ": {{toolCall: "probe", args: `{"n":1}`}, {toolCall: "probe", args: `{"n":2}`}, {content: "done"}}}
	a := hookedAdapter(t, gen, nil, map[string]registry.HookSpec{"second-blocks": {
		Event: domain.HookPostToolUse, Path: dir + "/second-blocks.yaml",
		Command: `n=$(cat ` + counter + ` 2>/dev/null || echo 0); n=$((n+1)); echo $n > ` + counter + `; [ "$n" -eq 2 ] && { echo 'second call blocked' >&2; exit 2; }; exit 0`,
	}})
	a.toolProv = &liveProvider{ts: []tools.Tool{probe}}
	if err := a.SetLoop(LoopLegacy); err != nil {
		t.Fatal(err)
	}
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-legacy-occ", Goal: "adk-legacy-occ goal", Model: "prov/m1"}))
	// The final call's input holds BOTH tool messages, in order: the first
	// allowed (the real "ok"), the second replaced by the hook's verdict.
	var toolMsgs []string
	for _, m := range gen.calls[len(gen.calls)-1] {
		if m.Role == schema.Tool {
			toolMsgs = append(toolMsgs, m.Content)
		}
	}
	if strings.Join(toolMsgs, "|") != "ok|error: blocked by hook: second-blocks: second call blocked" {
		t.Fatalf("each occurrence carries its own verdict: %v", toolMsgs)
	}
	if rows := toolRows(t, a, "adk-legacy-occ"); len(rows) != 2 || rows[0].Content != "ok" || rows[1].Content != "ok" {
		t.Fatalf("both durable rows keep the real output: %+v", rows)
	}
}
