// The compaction bridge (engine-adk-spine slice 1, review F1): the thin
// ChatModelAgentMiddleware the backend constitution's Compaction Rule
// prescribes — the POLICY is internal/compaction behind domain.Compactor;
// this file only converts state, asks, and keeps the run's structured
// messages intact. BeforeModel hook: when the context is over budget, the
// oldest messages become one summary (PreCompact hooks fire inside the
// policy; a block skips the pass) and the recent tail stays verbatim —
// tool calls and results included, so provider exchange rules hold. The
// pass is observable: one durable-first compaction_triggered event.

package eino

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/compaction"
	"zbot/backend/internal/domain"
)

type compactionMW struct {
	adk.BaseChatModelAgentMiddleware
	a         *Adapter
	ctx       context.Context
	events    chan domain.TimelineEvent
	in        runInput
	emit      emitFunc
	compactor domain.Compactor
	budget    domain.TokenBudget
}

func (m *compactionMW) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if m.compactor == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	dm := toDomainMessages(state.Messages)
	cs := domain.ContextState{SessionID: m.in.sessionID, TotalTokens: compaction.Tokens(dm), MessageCount: len(dm), Budget: m.budget}
	if !m.compactor.ShouldCompact(ctx, cs) {
		return ctx, state, nil
	}
	out, stats, err := m.compactor.Compact(ctx, dm, m.budget)
	switch {
	case err != nil:
		if !errors.Is(err, compaction.ErrNothingToSummarize) {
			m.emit(domain.EventAgentStep, "engine: in-run compaction skipped: "+err.Error())
		}
		return ctx, state, nil
	case stats.Strategy != "summarize":
		if stats.Strategy == "blocked" {
			m.emit(domain.EventAgentStep, "engine: in-run compaction blocked by a PreCompact hook")
		}
		return ctx, state, nil
	}
	// The policy's contract: [verbatim system prefix] + [summary] + [verbatim
	// suffix]. Rebuild with OUR originals for the suffix.
	prefix := 0
	for prefix < len(state.Messages) && state.Messages[prefix].Role == schema.System && !strings.HasPrefix(state.Messages[prefix].Content, compaction.SummaryHeader) {
		prefix++
	}
	tail := len(out) - prefix - 1
	if tail < 0 || tail > len(state.Messages)-prefix {
		m.emit(domain.EventAgentStep, fmt.Sprintf("engine: in-run compaction skipped: unexpected policy shape (prefix %d, tail %d)", prefix, tail))
		return ctx, state, nil
	}
	fresh := make([]*schema.Message, 0, prefix+1+tail)
	fresh = append(fresh, state.Messages[:prefix]...)
	fresh = append(fresh, schema.SystemMessage(out[prefix].Content))
	fresh = append(fresh, state.Messages[len(state.Messages)-tail:]...)
	state.Messages = fresh
	m.a.emitCompaction(m.ctx, m.events, m.in, &stats)
	return ctx, state, nil
}

// toDomainMessages converts the model-facing messages for the policy:
// roles map through; an assistant message's tool calls are rendered into
// its content so the transcript the summarizer reads names them; every
// message carries its token estimate (the policy's own unit).
func toDomainMessages(msgs []*schema.Message) []domain.Message {
	out := make([]domain.Message, 0, len(msgs))
	for _, m := range msgs {
		dm := domain.Message{Content: m.Content, Tokens: estimateTokens([]*schema.Message{m})}
		switch m.Role {
		case schema.System:
			dm.Role = domain.RoleSystem
		case schema.User:
			dm.Role = domain.RoleUser
		case schema.Tool:
			dm.Role = domain.RoleTool
			if m.ToolName != "" {
				dm.Content = m.ToolName + ": " + m.Content
			}
		default:
			dm.Role = domain.RoleAssistant
			for _, tc := range m.ToolCalls {
				dm.Content += "\n[tool call " + tc.Function.Name + " " + tc.Function.Arguments + "]"
			}
		}
		out = append(out, dm)
	}
	return out
}

// estimateTokens is the characters / 4 heuristic the middlewares share.
func estimateTokens(msgs []*schema.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content) + len(m.ReasoningContent)
		for _, tc := range m.ToolCalls {
			n += len(tc.Function.Arguments)
		}
	}
	return n / 4
}
