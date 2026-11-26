// Run-loop helpers (extracted from adapter.go, LOC budget): the model
// cache key, the offered-inventory executor, the require_tool_use nudge
// text, and the per-step usage reader.

package eino

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
)

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

// toolUseNudge is the one bounded instruction a require_tool_use run
// receives after answering without tools (delegation-truthfulness).
const toolUseNudge = "You answered without using any tools. If the task required reading files, running commands, or searching, do that NOW and report only what you actually did, citing the files you read or the commands you ran. Never describe steps you did not take. If you could not access the evidence you needed, say so explicitly instead of inferring."

// usageOrNull reads one side of a completion's request usage onto a
// message row (NULL when the provider reported none).
func usageOrNull(rm *schema.ResponseMeta, prompt bool) sql.NullInt64 {
	if rm == nil || rm.Usage == nil {
		return sql.NullInt64{}
	}
	if prompt {
		return sql.NullInt64{Int64: int64(rm.Usage.PromptTokens), Valid: true}
	}
	return sql.NullInt64{Int64: int64(rm.Usage.CompletionTokens), Valid: true}
}

// emitToolCall records one tool call durably (the row keeps "tool_call"
// under the call's execution, attributed), publishes it, streams it (a
// child's call surfaces as a delegation event in the parent's stream),
// and appends the transcript's tool row.
func (a *Adapter) emitToolCall(ctx context.Context, events chan domain.TimelineEvent, in runInput, tc schema.ToolCall, content string) {
	payload, _ := json.Marshal(map[string]any{"tool": tc.Function.Name, "output": content, "agent": in.agent()})
	_ = a.st.AppendEvent(store.Event{
		ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID,
		Kind: "tool_call", Payload: string(payload),
	})
	toolEvt := domain.TimelineEvent{
		ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID, Kind: domain.EventToolCall,
		Text:     tc.Function.Name, // the name rides Text too: every SSE consumer (UI work stream, /v1 frames) renders it
		Agent:    in.agent(),
		ToolCall: &domain.ToolCallPayload{Tool: tc.Function.Name, Output: content},
	}
	if in.isChild {
		// Child tool calls surface as delegation events (the durable row
		// keeps "tool_call" under the child's execution).
		toolEvt.Kind = domain.EventDelegation
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
		ExecutionID: in.execID, Role: "tool", Agent: in.agent(), Content: content,
		ToolCallID: tc.ID, ToolName: tc.Function.Name,
	})
}
