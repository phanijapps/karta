package eino

// The ADK run path (engine-adk-spine, slice 0): the conformance suite
// runs on it (TestContractSuiteADK in adapter_test.go); these pin the
// loop behaviors that must survive the swap — rows/events in the
// legacy's order with usage and attribution, the hallucinated-tool
// refusal, the first-answer nudge as a continuation run, the transient
// progress nudge, the stuck kill, the step bound, and the loop switch.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// scriptedGen is a routingGenerator that records every call's messages.
type scriptedGen struct {
	routingGenerator
	mu    sync.Mutex
	calls [][]*schema.Message
}

func (g *scriptedGen) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	g.mu.Lock()
	g.calls = append(g.calls, append([]*schema.Message(nil), msgs...))
	g.mu.Unlock()
	return g.routingGenerator.Generate(ctx, msgs, opts...)
}

func (g *scriptedGen) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

func newADKDelegating(t *testing.T, gen chatGenerator, snapFn func() registry.Snapshot) *Adapter {
	t.Helper()
	a := newDelegatingAdapter(t, gen, snapFn)
	if err := a.SetLoop(LoopADK); err != nil {
		t.Fatal(err)
	}
	return a
}

func drainEvents(t *testing.T, ch <-chan domain.TimelineEvent) []domain.TimelineEvent {
	t.Helper()
	var out []domain.TimelineEvent
	for evt := range ch {
		out = append(out, evt)
	}
	return out
}

func TestSetLoopRejectsUnknown(t *testing.T) {
	t.Parallel()
	a := New(fakeProviderStore{}, nil, nil, tools.BuiltinProvider{}, nil)
	if a.loop != LoopLegacy {
		t.Fatalf("the transition default must be %q, got %q", LoopLegacy, a.loop)
	}
	if err := a.SetLoop("spaghetti"); err == nil || !strings.Contains(err.Error(), "engine_loop") {
		t.Fatalf("unknown loop must be a typed error naming the knob: %v", err)
	}
	if err := a.SetLoop(LoopADK); err != nil || a.loop != LoopADK {
		t.Fatalf("SetLoop(adk): loop %q err %v", a.loop, err)
	}
}

// TestADKToolRoundParity: a root tool round produces the legacy's rows
// and events in the legacy's order — intent row before execution, tool
// row + tool_call event per call, the hallucinated name refused as the
// typed error text (never executed), the final row with its reasoning,
// per-step usage everywhere, "run completed" last.
func TestADKToolRoundParity(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{
		"adk-tools": {
			{toolCall: "clock.now", args: `{}`},
			{toolCall: "ghost_tool", args: `{"x":1}`},
			{content: "final answer", reasoning: "thought about it"},
		},
	}
	a := newADKDelegating(t, gen, nil)
	ch, err := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "adk-tools", Goal: "adk-tools goal", Model: "prov/m1"})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, ch)

	msgs, _ := a.st.Replay("adk-tools", 0, 50)
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m.Role)
		if m.Agent != domain.AgentRoot {
			t.Fatalf("row not attributed to root: %+v", m)
		}
	}
	wantRoles := []string{"user", "assistant", "tool", "assistant", "tool", "assistant"}
	if strings.Join(roles, ",") != strings.Join(wantRoles, ",") {
		t.Fatalf("row order %v, want %v", roles, wantRoles)
	}
	intent, ghost, tool1, tool2, final := msgs[1], msgs[3], msgs[2], msgs[4], msgs[5]
	if !strings.Contains(intent.ToolCalls, `"clock.now"`) || !strings.HasPrefix(intent.ID, "msg-intent-") {
		t.Fatalf("intent row: %+v", intent)
	}
	if !strings.Contains(ghost.ToolCalls, `"ghost_tool"`) {
		t.Fatalf("second intent row: %+v", ghost)
	}
	if tool1.ToolName != "clock.now" || tool1.ToolCallID == "" || strings.HasPrefix(tool1.Content, "error") {
		t.Fatalf("clock_now tool row: %+v", tool1)
	}
	if tool2.ToolName != "ghost_tool" || tool2.Content != unknownToolResult("ghost_tool") {
		t.Fatalf("a hallucinated tool must be refused with the typed error text, got %+v", tool2)
	}
	if final.Content != "final answer" || final.Reasoning != "thought about it" || !strings.HasPrefix(final.ID, "msg-final-") || final.ToolCalls != "" {
		t.Fatalf("final row: %+v", final)
	}
	for _, m := range []store.Message{intent, ghost, final} {
		if !m.PromptTokens.Valid || m.PromptTokens.Int64 != 100 || m.CompletionTokens.Int64 != 10 || m.Provider != "prov" || m.Model != "m1" {
			t.Fatalf("assistant row must carry per-step usage + provenance: %+v", m)
		}
	}
	tokensIn, tokensOut, status, _ := a.st.ExecutionUsage(events[0].ExecutionID)
	if tokensIn != 300 || tokensOut != 30 || status != "completed" {
		t.Fatalf("execution usage %d/%d %s, want 300/30 completed", tokensIn, tokensOut, status)
	}

	// Events: goal → agent_step(intent content) → tool_call → agent_step →
	// tool_call → agent_step(final) → "run completed" last; all attributed.
	var kinds []string
	for _, evt := range events {
		kinds = append(kinds, string(evt.Kind))
		if evt.Agent != domain.AgentRoot || evt.ExecutionID == "" || evt.SessionID != "adk-tools" {
			t.Fatalf("event attribution: %+v", evt)
		}
	}
	if got, want := strings.Join(kinds, ","), "session,agent_step,tool_call,agent_step,tool_call,agent_step,session"; got != want {
		t.Fatalf("event order %s, want %s", got, want)
	}
	if last := events[len(events)-1]; !strings.Contains(last.Text, "run completed") {
		t.Fatalf("the run must declare completion last: %+v", last)
	}
	if events[len(events)-2].Text != "final answer" {
		t.Fatalf("the final agent_step carries the answer: %+v", events[len(events)-2])
	}
	durable, _ := a.st.Events("adk-tools", "", 50)
	if len(durable) != len(events) {
		t.Fatalf("every streamed event is durable: %d streamed, %d stored", len(events), len(durable))
	}
}

// scripts is an empty script set for the scripted generator.
func scripts() map[string][]fakeGenStep { return map[string][]fakeGenStep{} }

// TestADKRootNudgeOnce: an agent with require_tool_use answering its
// FIRST completion without tools is nudged exactly once — the answer
// row keeps ToolCalls "[]", the nudge rides a continuation run whose
// model input is [history, answer, nudge], and the next answer is final.
func TestADKRootNudgeOnce(t *testing.T) {
	t.Parallel()
	snap := agentSnap()
	snap.Agents["builder"] = registry.AgentSpec{Name: "builder", SystemPrompt: "Build.", Description: "d", MaxDepth: 0, RequireToolUse: true}
	snap.Agents["writer"] = registry.AgentSpec{Name: "writer", SystemPrompt: "Write.", Description: "d", MaxDepth: 0}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{
		"adk-nudge": {
			{content: "I'll start by reading the codebase…"}, // narrated, no tools
			{content: "Built it."},
		},
	}
	a := newADKDelegating(t, gen, func() registry.Snapshot { return snap })
	ch, err := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "adk-nudge", Goal: "adk-nudge goal", Agent: "builder", Model: "prov/m1"})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, ch)
	var nudges int
	for _, evt := range events {
		if strings.Contains(evt.Text, "nudged once") {
			nudges++
			if evt.Agent != "builder" {
				t.Fatalf("the nudge is attributed to the agent: %+v", evt)
			}
		}
	}
	if nudges != 1 || !strings.Contains(events[len(events)-1].Text, "run completed") {
		t.Fatalf("one nudge, then completion: nudges %d, last %+v", nudges, events[len(events)-1])
	}
	if n := gen.callCount(); n != 2 {
		t.Fatalf("narrated → nudged → accepted = 2 model calls, got %d", n)
	}
	// The continuation run's input: the goal, the narrated answer, the nudge last.
	last := gen.calls[1]
	if len(last) < 3 || last[len(last)-1].Role != schema.User || !strings.Contains(last[len(last)-1].Content, toolUseNudge) ||
		last[len(last)-2].Role != schema.Assistant || last[len(last)-2].Content != "I'll start by reading the codebase…" {
		t.Fatalf("continuation input must be [history, answer, nudge]: %+v", last)
	}
	if last[0].Role != schema.System || last[0].Content != "Build." {
		t.Fatalf("the agent's instruction leads the model input: %+v", last[0])
	}
	msgs, _ := a.st.Replay("adk-nudge", 0, 50)
	var nudgeRows, finalRows int
	for _, m := range msgs {
		switch {
		case strings.HasPrefix(m.ID, "msg-nudge-"):
			nudgeRows++
			if m.ToolCalls != "[]" || m.Content != "I'll start by reading the codebase…" {
				t.Fatalf("the nudged non-answer keeps ToolCalls []: %+v", m)
			}
		case strings.HasPrefix(m.ID, "msg-final-"):
			finalRows++
			if m.Content != "Built it." {
				t.Fatalf("final row: %+v", m)
			}
		}
	}
	if nudgeRows != 1 || finalRows != 1 {
		t.Fatalf("rows: %d nudge, %d final", nudgeRows, finalRows)
	}

	// Without require_tool_use the same narration is accepted at once.
	gen2 := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen2.scripts = map[string][]fakeGenStep{"adk-plain": {{content: "narrated answer"}}}
	a2 := newADKDelegating(t, gen2, func() registry.Snapshot { return snap })
	events2 := drainEvents(t, mustRun(t, a2, domain.GoalRequest{SessionID: "adk-plain", Goal: "adk-plain goal", Agent: "writer", Model: "prov/m1"}))
	if gen2.callCount() != 1 || events2[len(events2)-2].Text != "narrated answer" {
		t.Fatalf("a synthesis task is not penalized: calls %d, %+v", gen2.callCount(), events2)
	}
}

func mustRun(t *testing.T, a *Adapter, req domain.GoalRequest) <-chan domain.TimelineEvent {
	t.Helper()
	ch, err := a.RunGoal(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// TestADKProgressNudgeIsTransient: the turn-budget nudge rides ONE model
// call as its last message and is gone on the next — never persisted,
// never sticky (the legacy posture, kept on the spine).
func TestADKProgressNudgeIsTransient(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{
		"adk-prog": {{toolCall: "clock.now", args: `{"n":1}`}, {toolCall: "clock.now", args: `{"n":2}`}, {toolCall: "clock.now", args: `{"n":3}`}, {content: "done"}},
	}
	a := newADKDelegating(t, gen, nil)
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-prog", Goal: "adk-prog goal", Model: "prov/m1", MaxSteps: 2})) // soft 2: the nudge fires at step 2
	if n := gen.callCount(); n != 4 {
		t.Fatalf("4 model calls, got %d", n)
	}
	has := func(msgs []*schema.Message) bool {
		for _, m := range msgs {
			if m.Role == schema.User && strings.HasPrefix(m.Content, "[SYSTEM: You have used 2 of 4") {
				return true
			}
		}
		return false
	}
	if !has(gen.calls[2]) || gen.calls[2][len(gen.calls[2])-1].Content == "" || !strings.HasPrefix(gen.calls[2][len(gen.calls[2])-1].Content, "[SYSTEM: You have used 2 of 4") {
		t.Fatalf("the nudge rides step 2 as the last message: %+v", gen.calls[2])
	}
	if has(gen.calls[3]) {
		t.Fatalf("the nudge must be stripped after the model saw it: %+v", gen.calls[3])
	}
	if gen.calls[3][len(gen.calls[3])-1].Role != schema.Tool {
		t.Fatalf("step 3 follows the tool result: %+v", gen.calls[3][len(gen.calls[3])-1])
	}
	msgs, _ := a.st.Replay("adk-prog", 0, 50)
	for _, m := range msgs {
		if strings.Contains(m.Content, "[SYSTEM:") {
			t.Fatalf("advisory nudges are never persisted: %+v", m)
		}
	}
	for _, evt := range events {
		if strings.Contains(evt.Text, "[SYSTEM:") {
			t.Fatalf("advisory nudges are never emitted: %+v", evt)
		}
	}
}

// TestADKStuckKill: eleven identical failing calls trip the tracker —
// the stuck warning nudge at step 10, the kill at step 11 with the
// legacy's terminal text and a failed execution.
func TestADKStuckKill(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	var steps []fakeGenStep
	for i := 0; i < 20; i++ {
		steps = append(steps, fakeGenStep{toolCall: "ghost_tool", args: `{"x":1}`})
	}
	gen.scripts = map[string][]fakeGenStep{"adk-stuck": steps}
	a := newADKDelegating(t, gen, nil)
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-stuck", Goal: "adk-stuck goal", Model: "prov/m1"}))
	if n := gen.callCount(); n != 11 {
		t.Fatalf("the kill lands on the 11th call, got %d", n)
	}
	var stuckEvent bool
	for _, evt := range events {
		if evt.Kind == domain.EventAgentStep && strings.HasPrefix(evt.Text, "engine: stopping — agent stuck (progress score ") {
			stuckEvent = true
		}
	}
	if !stuckEvent {
		t.Fatalf("the stuck kill must be described on the stream: %+v", events)
	}
	_, _, status, _ := a.st.ExecutionUsage(events[0].ExecutionID)
	if status != "failed" {
		t.Fatalf("status %s, want failed", status)
	}
	// The stuck warning (a [SYSTEM: …] nudge) rode step 10 — the kill
	// lands before a 12th model call exists (calls 0..10).
	if !strings.Contains(gen.calls[10][len(gen.calls[10])-1].Content, "repeating similar actions") {
		t.Fatalf("the stuck warning rides step 10 as the last message: %+v", gen.calls[10][len(gen.calls[10])-1])
	}
}

// TestADKStepBoundReached: an endless tool script ends at the iteration
// ceiling with the recorded outcome — never a silent loop.
func TestADKStepBoundReached(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-bound": {{toolCall: "clock.now", args: `{"n":1}`}}}
	a := newADKDelegating(t, gen, nil)
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-bound", Goal: "adk-bound goal", Model: "prov/m1", MaxSteps: 1})) // ceiling = 2 + 3 = 5
	if n := gen.callCount(); n != 5 {
		t.Fatalf("the ceiling is 5 model calls, got %d", n)
	}
	if last := events[len(events)-1]; !strings.Contains(last.Text, "step bound reached") {
		t.Fatalf("the step bound must be the last event: %+v", last)
	}
	// Tools are withdrawn from the second model call on (hard budget 2):
	// step 0's call executes, every later call is refused — never
	// executed — and the model sees the typed text on the following step.
	if last := gen.calls[1][len(gen.calls[1])-1]; last.Role != schema.Tool || strings.HasPrefix(last.Content, "error") {
		t.Fatalf("step 0's call executes: %+v", last)
	}
	// Legacy parity (review F5): nudges fire only while tools are offered —
	// none at or beyond the suppression boundary, even though turn 1 ≥ soft.
	for i, call := range gen.calls {
		for _, m := range call {
			if m.Role == schema.User && strings.HasPrefix(m.Content, "[SYSTEM:") {
				t.Fatalf("call %d: no nudge once tools are withdrawn: %+v", i, m)
			}
		}
	}
	for i := 2; i < 5; i++ {
		var refused bool
		for _, m := range gen.calls[i] {
			if m.Role == schema.Tool && m.Content == unknownToolResult("clock.now") {
				refused = true
			}
		}
		if !refused {
			t.Fatalf("call %d must see the refused tool result: %+v", i, gen.calls[i])
		}
	}
	_, _, status, _ := a.st.ExecutionUsage(events[0].ExecutionID)
	if status != "failed" {
		t.Fatalf("status %s, want failed", status)
	}
}

// TestADKWrapperRefusesSuppressedCalls: near the ceiling the wrapper
// refuses a call before execution — the typed error text, nothing run.
func TestADKWrapperRefusesSuppressedCalls(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t, &modelFactory{})
	ctx := context.Background()
	events := make(chan domain.TimelineEvent, 16)
	st := newRunState(0)
	st.suppress = true
	executed := false
	mw := &persistenceMW{a: a, ctx: ctx, events: events, in: runInput{sessionID: "adk-sup", execID: "exec-1"}, st: st, emit: func(domain.TimelineEventKind, string) {}}
	endpoint, err := mw.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
		executed = true
		return "ran", nil
	}, &adk.ToolContext{Name: "clock.now", CallID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := endpoint(ctx, `{}`)
	if executed || err != nil || out != unknownToolResult("clock.now") {
		t.Fatalf("suppressed call: executed %v, out %q, err %v", executed, out, err)
	}
	if st.tracker.Iterations() != 1 {
		t.Fatalf("the refusal is recorded with the tracker: iterations %d", st.tracker.Iterations())
	}
}

// gateGen blocks in Generate until released and then answers SUCCESSFULLY
// regardless of the context — the "cancelled while the model answered"
// shape (review F1).
type gateGen struct {
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (g *gateGen) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	g.once.Do(func() { close(g.entered) })
	<-g.gate
	return &schema.Message{Role: schema.Assistant, Content: "late answer"}, nil
}

// TestADKCancelDuringModelCallNeverCompletes: a successful answer that
// lands after Cancel is never recorded as the run's success — the
// legacy post-Generate check, on the spine.
func TestADKCancelDuringModelCallNeverCompletes(t *testing.T) {
	t.Parallel()
	gen := &gateGen{entered: make(chan struct{}), gate: make(chan struct{})}
	a := newADKDelegating(t, gen, nil)
	ch, err := a.RunGoal(context.Background(), domain.GoalRequest{SessionID: "adk-cancel", Goal: "adk-cancel goal", Model: "prov/m1"})
	if err != nil {
		t.Fatal(err)
	}
	<-gen.entered // the run is provably inside its model call
	if err := a.Cancel(context.Background(), "adk-cancel"); err != nil {
		t.Fatal(err)
	}
	close(gen.gate) // the model answers anyway
	events := drainEvents(t, ch)
	for _, evt := range events {
		if strings.Contains(evt.Text, "run completed") || evt.Text == "late answer" {
			t.Fatalf("a cancelled run must not complete or surface the late answer: %+v", evt)
		}
	}
	if _, _, status, _ := a.st.ExecutionUsage(events[0].ExecutionID); status != "cancelled" {
		t.Fatalf("status must be cancelled, got %s", status)
	}
	// Durable-first: the cancellation is on the record even though a
	// cancelled context may skip its streamed copy (the emit's select).
	durable, _ := a.st.Events("adk-cancel", "", 50)
	var cancelledOnRecord bool
	for _, e := range durable {
		if strings.Contains(e.Payload, "run cancelled") {
			cancelledOnRecord = true
		}
	}
	if !cancelledOnRecord {
		t.Fatalf("the cancellation must be described durably: %+v", durable)
	}
	msgs, _ := a.st.Replay("adk-cancel", 0, 50)
	for _, m := range msgs {
		if m.Role == "assistant" {
			t.Fatalf("no assistant row may land after cancellation: %+v", m)
		}
	}
}

// TestADKNudgedRunSharesTheCeiling: the continuation run inherits the
// REMAINING iteration budget — a nudged run that then loops on tools
// dies at the same model-call ceiling the legacy loop enforces (review F2).
func TestADKNudgedRunSharesTheCeiling(t *testing.T) {
	t.Parallel()
	snap := agentSnap()
	snap.Agents["builder"] = registry.AgentSpec{Name: "builder", SystemPrompt: "Build.", Description: "d", MaxDepth: 0, RequireToolUse: true}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-ceiling": {
		{content: "narrated first answer"}, // nudged
		{toolCall: "clock.now", args: `{"n":1}`},
	}}
	a := newADKDelegating(t, gen, func() registry.Snapshot { return snap })
	events := drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-ceiling", Goal: "adk-ceiling goal", Agent: "builder", Model: "prov/m1", MaxSteps: 1})) // ceiling 5 for the whole run
	if n := gen.callCount(); n != 5 {
		t.Fatalf("nudge continuation shares the ceiling: 5 model calls total, got %d", n)
	}
	if last := events[len(events)-1]; !strings.Contains(last.Text, "step bound reached") {
		t.Fatalf("the ceiling ends the run: %+v", last)
	}
	if _, _, status, _ := a.st.ExecutionUsage(events[0].ExecutionID); status != "failed" {
		t.Fatalf("status %s, want failed", status)
	}
}

// TestADKNudgeRidesOneCallOnARoot: the require_tool_use nudge is gone
// after the continuation's first model call (review F6 — the legacy root
// never persisted it).
func TestADKNudgeRidesOneCallOnARoot(t *testing.T) {
	t.Parallel()
	snap := agentSnap()
	snap.Agents["builder"] = registry.AgentSpec{Name: "builder", SystemPrompt: "Build.", Description: "d", MaxDepth: 0, RequireToolUse: true}
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-once": {
		{content: "narrated first answer"},
		{toolCall: "clock.now", args: `{"n":1}`},
		{content: "built"},
	}}
	a := newADKDelegating(t, gen, func() registry.Snapshot { return snap })
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-once", Goal: "adk-once goal", Agent: "builder", Model: "prov/m1"}))
	hasNudge := func(msgs []*schema.Message) bool {
		for _, m := range msgs {
			if m.Role == schema.User && m.Content == toolUseNudge {
				return true
			}
		}
		return false
	}
	if n := gen.callCount(); n != 3 || !hasNudge(gen.calls[1]) || hasNudge(gen.calls[2]) {
		t.Fatalf("the nudge rides call 1 only: calls %d, call1 %v, call2 %v", gen.callCount(), hasNudge(gen.calls[1]), hasNudge(gen.calls[2]))
	}
}

// TestADKRefusalsPairWithTheirOwnCallID: two hallucinated calls to the
// SAME name in one response each pair with their own call ID (review F3).
func TestADKRefusalsPairWithTheirOwnCallID(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-pair": {
		{toolCall: "ghost_tool", args: `{"x":1}`, extra: []fakeGenStep{{toolCall: "ghost_tool", args: `{"x":2}`}}},
		{content: "done"},
	}}
	a := newADKDelegating(t, gen, nil)
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-pair", Goal: "adk-pair goal", Model: "prov/m1"}))
	msgs, _ := a.st.Replay("adk-pair", 0, 50)
	var intent store.Message
	var toolIDs []string
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.ToolCalls, "ghost_tool") {
			intent = m
		}
		if m.Role == "tool" && m.ToolName == "ghost_tool" {
			toolIDs = append(toolIDs, m.ToolCallID)
		}
	}
	var calls []schema.ToolCall
	_ = json.Unmarshal([]byte(intent.ToolCalls), &calls)
	if len(calls) != 2 || len(toolIDs) != 2 || toolIDs[0] != calls[0].ID || toolIDs[1] != calls[1].ID || toolIDs[0] == toolIDs[1] {
		t.Fatalf("each refusal pairs with its own call: calls %+v, tool rows %v", calls, toolIDs)
	}
}

// TestADKDelegateToolReadsTheLiveRegistry: built once per run, the
// delegate tool still refuses an agent the registry dropped mid-run
// (review F4).
func TestADKDelegateToolReadsTheLiveRegistry(t *testing.T) {
	t.Parallel()
	var snap registry.Snapshot
	a := newADKDelegating(t, &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}, func() registry.Snapshot { return snap })
	snap = agentSnap()
	d := a.delegateTool(context.Background(), make(chan domain.TimelineEvent, 4), runInput{})
	if !strings.Contains(d.Def().Description, "researcher") {
		t.Fatalf("the routing table lists the researcher: %q", d.Def().Description)
	}
	snap = registry.Snapshot{Agents: map[string]registry.AgentSpec{}} // hot-reload drops every agent
	if res := d.Exec(context.Background(), `{"agent":"researcher","task":"x"}`); res.Err == nil || !strings.Contains(res.Err.Error(), "unknown agent") {
		t.Fatalf("a dropped agent must be refused typed, got %+v", res)
	}
	// (The tool value's own description is a snapshot, like the legacy
	// loop's per-step tool value was; the spine re-derives it per call —
	// TestADKInventoryFollowsTheRegistry.)
}

// toolsGen records the tool infos each model call was offered; before,
// when set, runs ahead of each call with the call's index.
type toolsGen struct {
	routingGenerator
	mu     sync.Mutex
	tools  [][]*schema.ToolInfo
	before func(call int)
}

func (g *toolsGen) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	g.mu.Lock()
	call := len(g.tools)
	g.mu.Unlock()
	if g.before != nil {
		g.before(call)
	}
	o := model.GetCommonOptions(nil, opts...)
	g.mu.Lock()
	g.tools = append(g.tools, o.Tools)
	g.mu.Unlock()
	return g.routingGenerator.Generate(ctx, msgs, opts...)
}

// TestADKInventoryFollowsTheRegistry: the model-facing inventory is
// re-derived every call — an agent added to the registry mid-run shows
// up in the delegate tool's routing table on the next call (review F4).
func TestADKInventoryFollowsTheRegistry(t *testing.T) {
	t.Parallel()
	snap := agentSnap()
	delete(snap.Agents, "planner")
	gen := &toolsGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-live": {{toolCall: "clock.now", args: `{}`}, {content: "done"}}}
	a := newADKDelegating(t, gen, func() registry.Snapshot { return snap })
	gen.before = func(call int) {
		if call == 0 {
			snap = agentSnap() // hot-reload adds the planner DURING call 0 (call 0's schema is already fixed)
		}
	}
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-live", Goal: "adk-live goal", Model: "prov/m1"}))
	delegateDesc := func(call int) string {
		for _, ti := range gen.tools[call] {
			if ti.Name == "delegate_agent" {
				return ti.Desc
			}
		}
		return ""
	}
	if d := delegateDesc(0); d == "" || strings.Contains(d, "planner") {
		t.Fatalf("call 0 offers the delegate tool without the planner: %q", d)
	}
	if d := delegateDesc(1); !strings.Contains(d, "planner") {
		t.Fatalf("call 1 must advertise the planner: %q", d)
	}
}

// liveProvider is a mutable tool provider (a mid-run hot-reload of the
// tool plane — review R2-2).
type liveProvider struct {
	mu sync.Mutex
	ts []tools.Tool
}

func (p *liveProvider) Tools() []tools.Tool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]tools.Tool(nil), p.ts...)
}

func (p *liveProvider) set(ts ...tools.Tool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ts = ts
}

// namedTool is a constant-output tool with a chosen name.
type namedTool struct{ name, out string }

func (t namedTool) Def() tools.Def {
	return tools.Def{Name: t.name, Description: "test tool " + t.name, ParamsJSON: `{"type":"object","properties":{}}`}
}

func (t namedTool) Exec(context.Context, string) tools.Result { return tools.Result{Output: t.out} }

// TestADKExecutionFollowsTheLiveInventory: the compiled node's tools are
// name slots only — a tool removed, replaced or added mid-run is
// refused / the replacement runs / runs, exactly like the legacy loop's
// per-step inventory (review R2-2).
func TestADKExecutionFollowsTheLiveInventory(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		initial        []tools.Tool
		midrun         []tools.Tool
		called         string
		wantAdvertised bool
		wantRow        string
	}{
		{name: "removed", initial: []tools.Tool{namedTool{"live_tool", "v1"}}, midrun: nil, called: "live_tool", wantAdvertised: false, wantRow: unknownToolResult("live_tool")},
		{name: "replaced", initial: []tools.Tool{namedTool{"live_tool", "v1"}}, midrun: []tools.Tool{namedTool{"live_tool", "v2"}}, called: "live_tool", wantAdvertised: true, wantRow: "v2"},
		{name: "added", initial: nil, midrun: []tools.Tool{namedTool{"live_tool", "new"}}, called: "live_tool", wantAdvertised: true, wantRow: "new"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			live := &liveProvider{ts: append([]tools.Tool{}, tc.initial...)}
			gen := &toolsGen{routingGenerator: routingGenerator{scripts: scripts()}}
			session := "adk-live-" + tc.name
			gen.scripts = map[string][]fakeGenStep{session: {{toolCall: "clock.now", args: `{}`}, {toolCall: "live_tool", args: `{}`}, {content: "done"}}}
			gen.before = func(call int) {
				if call == 0 {
					live.set(tc.midrun...) // the tool plane changes during call 0
				}
			}
			a := newADKDelegating(t, gen, nil)
			a.toolProv = live
			drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: session, Goal: session + " goal", Model: "prov/m1"}))
			var advertised bool
			for _, ti := range gen.tools[1] {
				if ti.Name == "live_tool" {
					advertised = true
				}
			}
			if advertised != tc.wantAdvertised {
				t.Fatalf("call 1 advertised live_tool=%v, want %v", advertised, tc.wantAdvertised)
			}
			msgs, _ := a.st.Replay(session, 0, 50)
			var row string
			for _, m := range msgs {
				if m.Role == "tool" && m.ToolName == "live_tool" {
					row = m.Content
				}
			}
			if row != tc.wantRow {
				t.Fatalf("live_tool row %q, want %q", row, tc.wantRow)
			}
		})
	}
}

// TestADKEventsAreDurableBeforePublication: at bus delivery time, every
// event's durable row already exists — persist precedes publish precedes
// stream, by observation rather than by construction (review R2-4).
func TestADKEventsAreDurableBeforePublication(t *testing.T) {
	t.Parallel()
	gen := &scriptedGen{routingGenerator: routingGenerator{scripts: scripts()}}
	gen.scripts = map[string][]fakeGenStep{"adk-durable": {{toolCall: "clock.now", args: `{}`}, {content: "done"}}}
	a := newADKDelegating(t, gen, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := a.bus.Subscribe(ctx, "adk-durable")
	checked := make(chan int, 1)
	go func() {
		n := 0
		for evt := range sub.Events() {
			durable, _ := a.st.Events("adk-durable", "", 200)
			found := false
			for _, e := range durable {
				if e.ID == evt.ID {
					found = true
					break
				}
			}
			if !found {
				close(checked)
				t.Errorf("event %s (%s) was published before its durable row existed", evt.ID, evt.Kind)
				return
			}
			n++
		}
		checked <- n
	}()
	drainEvents(t, mustRun(t, a, domain.GoalRequest{SessionID: "adk-durable", Goal: "adk-durable goal", Model: "prov/m1"}))
	cancel()
	select {
	case n, ok := <-checked:
		if !ok {
			t.Fatal("a published event had no durable row at delivery time")
		}
		if n < 5 {
			t.Fatalf("the subscription saw %d events, want the whole run", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the bus subscription never drained")
	}
}
