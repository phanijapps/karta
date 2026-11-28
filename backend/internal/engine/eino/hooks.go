// The hook seams (hook-runner): where the engine raises lifecycle hooks and
// how every decision reaches the Timeline. The POLICY is the runner's
// (internal/hooks, the exit-code contract); this file only calls it at the
// right moments and emits — constitution rule 4: no silent decisions.
//
//   PreToolUse / PostToolUse  — the tool wrapper (mw_persistence.go), every
//                               executed call on the spine
//   SessionStart / SessionEnd — prepareRun and finishRun, root runs only
//                               (advisory: a block reads as warn)
//   SubagentStop              — the delegation fold-back (delegation.go)
//   PreCompact                — the compaction policy (internal/compaction)

package eino

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/progress"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

// SetHookRunner wires the hook runner (nil = no hooks: every decision allows).
func (a *Adapter) SetHookRunner(r domain.HookRunner) { a.hooks = r }

// runHook raises one event through the runner and emits the decision. A
// runner error becomes a warn (visible), never a run failure.
func (a *Adapter) runHook(ctx context.Context, events chan domain.TimelineEvent, in runInput, ev domain.HookEvent) domain.HookDecision {
	if a.hooks == nil {
		return domain.HookDecision{Action: domain.HookAllow}
	}
	// EXECUTION ignores the run's cancellation (a terminal audit hook must
	// run) — each hook is bounded by its OWN manifest timeout, so a slow
	// earlier hook can never downgrade a later blocking one to a warning;
	// STREAMING keeps the cancellable ctx, so a vanished consumer can never
	// wedge the run on a full channel.
	d, err := a.hooks.Run(context.WithoutCancel(ctx), ev)
	if err != nil {
		d = domain.HookDecision{Action: domain.HookWarn, Reason: "hook runner: " + err.Error()}
	}
	// The record carries what the hook was asked (the goal, the status, the
	// child) — the decision's context is the contract's free map.
	if len(ev.Payload) > 0 {
		if d.Context == nil {
			d.Context = map[string]any{}
		}
		d.Context["payload"] = ev.Payload
	}
	a.emitHook(ctx, events, in, ev, d)
	return d
}

// hookText renders the decision line the Timeline carries.
func hookText(ev domain.HookEvent, d domain.HookDecision) string {
	subject := ev.ToolName
	if ev.Event == domain.HookSubagentStop {
		subject, _ = ev.Payload["agent"].(string)
	}
	text := fmt.Sprintf("hook %s", ev.Event)
	if subject != "" {
		text += " " + subject
	}
	text += ": " + string(d.Action)
	if d.Reason != "" {
		text += " — " + d.Reason
	}
	return text
}

// emitHook persists, publishes and streams one hook_decision event
// (attempted-durable-first, the run's attribution; a child's decisions
// surface in the parent's stream as delegation-kind events).
func (a *Adapter) emitHook(ctx context.Context, events chan domain.TimelineEvent, in runInput, ev domain.HookEvent, d domain.HookDecision) {
	text := hookText(ev, d)
	chanKind, kind := domain.EventHookDecision, domain.EventHookDecision
	if in.isChild {
		chanKind = domain.EventDelegation
	}
	evt := domain.TimelineEvent{ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID, Kind: chanKind, Text: text, Agent: in.agent(), Hook: &d}
	payload, _ := json.Marshal(map[string]any{"text": text, "agent": in.agent(), "hook": d, "event": ev.Event})
	_ = a.st.AppendEvent(store.Event{ID: evt.ID, SessionID: in.sessionID, ExecutionID: in.execID, Kind: string(kind), Payload: string(payload)}) //nolint:errcheck // attempted-durable-first (F-15 posture)
	if a.bus != nil {
		_ = a.bus.Publish(context.Background(), evt) //nolint:errcheck // fire-and-forget emit (F-15) // ctx-ok: F-15
	}
	select {
	case events <- evt:
	case <-ctx.Done():
	}
}

// finishRun is the one exit every run goes through: the SessionEnd hook
// (root runs; advisory) and the execution's terminal status.
func (a *Adapter) finishRun(ctx context.Context, events chan domain.TimelineEvent, in runInput, status, errText string, tokensIn, tokensOut int64) {
	if !in.isChild {
		a.runHook(ctx, events, in, domain.HookEvent{Event: domain.HookSessionEnd, SessionID: in.sessionID,
			Payload: map[string]any{"status": status, "tokens_in": tokensIn, "tokens_out": tokensOut}})
	}
	_ = a.st.FinishExecution(in.execID, status, errText, tokensIn, tokensOut)
}

// sessionStart raises SessionStart for a root run (advisory).
func (a *Adapter) sessionStart(ctx context.Context, events chan domain.TimelineEvent, in runInput) {
	if in.isChild {
		return
	}
	a.runHook(ctx, events, in, domain.HookEvent{Event: domain.HookSessionStart, SessionID: in.sessionID, Payload: map[string]any{"goal": firstLine(in.goal)}})
}

// toolInput parses a call's arguments for the hook payload (empty on garbage).
func toolInput(argsJSON string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return map[string]any{}
	}
	return m
}

// executeTool is THE tool-call seam for both run paths: PreToolUse → the
// call → record + the durable tool row → PostToolUse. A pre-block never
// executes the call (the model sees the reason, recorded like a refusal);
// a post-block replaces the MODEL-FACING result only. Returns what the
// model sees.
func (a *Adapter) executeTool(ctx context.Context, events chan domain.TimelineEvent, in runInput, tracker *progress.Tracker, name, argsJSON, callID string, call func() tools.Result) string {
	ev := domain.HookEvent{Event: domain.HookPreToolUse, SessionID: in.sessionID, ToolName: name, ToolInput: toolInput(argsJSON)}
	if d := a.runHook(ctx, events, in, ev); d.Action == domain.HookBlock {
		content := "error: blocked by hook: " + d.Reason
		tracker.Record(name, argsJSON, "blocked by hook")
		a.emitToolCall(ctx, events, in, schema.ToolCall{ID: callID, Function: schema.FunctionCall{Name: name}}, content)
		return content
	}
	res := call()
	content, errText := res.Output, ""
	if res.Err != nil {
		content, errText = "error: "+res.Err.Error(), res.Err.Error()
	}
	tracker.Record(name, argsJSON, errText)
	a.emitToolCall(ctx, events, in, schema.ToolCall{ID: callID, Function: schema.FunctionCall{Name: name}}, content) // the durable row keeps the REAL outcome
	ev.Event = domain.HookPostToolUse
	ev.Payload = map[string]any{"output": boundText(content, 4096), "error": errText}
	if d := a.runHook(ctx, events, in, ev); d.Action == domain.HookBlock {
		return "error: blocked by hook: " + d.Reason
	}
	return content
}

// boundText keeps the first n bytes of a hook payload value.
func boundText(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
