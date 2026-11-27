// The budget middleware (engine-adk-spine): the progress-aware budget
// the legacy loop applies per step, as the before/after model-state
// hooks — the same tracker, soft/hard budgets, extension windows, stuck
// kill, advisory progress nudges and near-ceiling tool suppression.

package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
)

type budgetMW struct {
	adk.BaseChatModelAgentMiddleware
	st      *runState
	emit    emitFunc
	refresh func(state *adk.ChatModelAgentState) // re-derives the model-facing inventory from the live plane each call (nil = keep)
}

func (m *budgetMW) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	step := m.st.calls
	m.st.calls++
	t := m.st.tracker
	// 1. Kill only when genuinely stuck (warned + sinking score).
	if t.ShouldStop() {
		return ctx, nil, errStuck{score: t.Score(), iterations: t.Iterations()}
	}
	// 2. At the soft budget: extend if productive.
	if step > 0 && step%m.st.soft == 0 && t.ShouldExtend(maxExtensions) {
		t.Extend()
		m.emit(domain.EventAgentStep, fmt.Sprintf("engine: extending run (productive, score %d, extension %d)", t.Score(), t.Extensions()))
	}
	t.Tick()
	// Offer tools on all but the final step(s): the model must land a
	// final answer before the ceiling (the wrapper refuses suppressed calls).
	m.st.suppress = step >= m.st.steps-1 || step >= m.st.hard-1
	if m.st.suppress {
		state.ToolInfos = nil
	}
	// The progress nudge rides one TRANSIENT user message (advisory
	// steering the model sees, the transcript does not keep — the legacy
	// posture); AfterModelRewriteState strips it again.
	// Only while tools are offered (the legacy loop nudged inside its
	// tools-offered branch): Nudge also flips the tracker's warning flags
	// that ShouldStop reads, so calling it near the ceiling would change
	// when a stuck run dies.
	if !m.st.suppress {
		if nudge := t.Nudge(step, m.st.soft, m.st.hard); nudge != "" {
			m.st.nudgeAt = len(state.Messages)
			state.Messages = append(state.Messages, schema.UserMessage(nudge))
		}
	}
	if m.refresh != nil {
		m.refresh(state)
	}
	return ctx, state, nil
}

// removeMessage drops state.Messages[i] (i < 0 = nothing to do).
func removeMessage(state *adk.ChatModelAgentState, i int) {
	if i >= 0 && i < len(state.Messages) {
		state.Messages = append(state.Messages[:i], state.Messages[i+1:]...)
	}
}

func (m *budgetMW) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if i := m.st.nudgeAt; i >= 0 && i < len(state.Messages) && state.Messages[i].Role == schema.User {
		removeMessage(state, i)
	}
	m.st.nudgeAt = -1
	// The require_tool_use nudge rides ONE model call on a root run (the
	// legacy loop re-reads durable rows every step and never persisted
	// it). Matched by content — the state carries the instruction prefix,
	// so an input-relative index would be off.
	if n := m.st.stripMsg; n != nil {
		for i, msg := range state.Messages {
			if msg == n { // identity: a user may type the same words
				removeMessage(state, i)
				break
			}
		}
		m.st.stripMsg = nil
	}
	return ctx, state, nil
}
