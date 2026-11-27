// The persistence middleware (engine-adk-spine): the durable-first rows
// the legacy loop writes per step, from the ADK hooks — the assistant
// intent row after a tool-calling model response (AfterModelRewriteState,
// which runs inside the model call before its event reaches the driver,
// so the row lands before anything streams), and the tool row + tool_call
// event from the tool-call wrapper (execute → progress record → emit, the
// legacy order). The driver persists the final/nudge rows itself: only it
// knows whether a tool-free response is THE answer.
//
// Refusals live here too: a call the legacy loop's execOffered would have
// refused — a name outside the inventory (hallucinated), or any call once
// tools are withdrawn near the ceiling or on a tool-less run — gets the
// typed error text as its result, is recorded with the tracker and
// surfaces as a tool_call row/event exactly like an executed call.

package eino

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/tools"
)

type persistenceMW struct {
	adk.BaseChatModelAgentMiddleware
	a      *Adapter
	ctx    context.Context
	events chan domain.TimelineEvent
	in     runInput
	st     *runState
	emit   emitFunc
}

func (m *persistenceMW) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if n := len(state.Messages); n > 0 {
		last := state.Messages[n-1]
		// Tool round: persist the intent before any tool executes.
		if last.Role == schema.Assistant && len(last.ToolCalls) > 0 {
			intentJSON, _ := json.Marshal(last.ToolCalls)
			m.a.appendAssistant(m.in, "intent", last, string(intentJSON))
			m.emit(domain.EventAgentStep, last.Content)
			// Remember name → call ID so a refusal (which the ADK hands
			// to the unknown-tool handler without the ID) pairs its row.
			if m.st.pendingIDs == nil {
				m.st.pendingIDs = map[string][]string{}
			}
			for _, tc := range last.ToolCalls {
				m.st.pendingIDs[tc.Function.Name] = append(m.st.pendingIDs[tc.Function.Name], tc.ID)
			}
		}
	}
	return ctx, state, nil
}

// WrapInvokableToolCall runs every call through execute — the compiled
// node's tool objects are only name slots, the LIVE inventory decides
// (legacy parity: the loop rebuilt its inventory per step). An endpoint
// error never surfaces to the ADK, which would fail the run; the legacy
// loop showed the model an "error: …" result instead.
func (m *persistenceMW) WrapInvokableToolCall(_ context.Context, _ adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(_ context.Context, argsJSON string, _ ...tool.Option) (string, error) {
		return m.execute(tCtx.Name, argsJSON, tCtx.CallID), nil
	}, nil
}

// unknownHandler routes names the compiled node does not know (hallucinated,
// or appeared mid-run) through the same live-inventory round.
func (m *persistenceMW) unknownHandler() func(context.Context, string, string) (string, error) {
	return func(_ context.Context, name, input string) (string, error) {
		return m.execute(name, input, m.st.pendingID(name)), nil
	}
}

// execute is the legacy execOffered round against the live inventory:
// withdrawn (near the ceiling), an empty plane, or not offered → the typed
// ErrToolNotOffered text as the result; otherwise the tool's output or
// its error as "error: …". Every outcome is recorded with the tracker
// (the stuck kill's failing-call scoring) and surfaces as a tool_call
// row + event, exactly like the legacy loop.
func (m *persistenceMW) execute(name, argsJSON, callID string) string {
	var res tools.Result
	if m.st.suppress {
		res = tools.Result{Err: fmt.Errorf("%w: %q", ErrToolNotOffered, name)}
	} else {
		res = execOffered(m.ctx, m.a.offeredTools(m.ctx, m.events, m.in), name, argsJSON) // the LIVE plane (empty → not offered)
	}
	content, errText := res.Output, ""
	if res.Err != nil {
		content, errText = "error: "+res.Err.Error(), res.Err.Error()
	}
	m.st.tracker.Record(name, argsJSON, errText)
	m.emitToolCall(callID, name, content)
	return content
}

func (m *persistenceMW) emitToolCall(callID, name, content string) {
	m.a.emitToolCall(m.ctx, m.events, m.in, schema.ToolCall{ID: callID, Function: schema.FunctionCall{Name: name}}, content)
}
