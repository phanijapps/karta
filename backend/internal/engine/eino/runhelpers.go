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
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/engine"
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
	toolEvt := domain.TimelineEvent{
		ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID, Kind: domain.EventToolCall,
		Text:     tc.Function.Name, // the name rides Text too: every SSE consumer (UI work stream, /v1 frames) renders it
		Agent:    in.agent(),
		ToolCall: &domain.ToolCallPayload{Tool: tc.Function.Name, Output: content},
	}
	// ONE identity per event: the durable row and the published/streamed
	// event share the ID (engine-adk-spine; the row used to mint its own,
	// which made an SSE tool_call uncorrelatable with the stored event).
	payload, _ := json.Marshal(map[string]any{"tool": tc.Function.Name, "output": content, "agent": in.agent()})
	_ = a.st.AppendEvent(store.Event{
		ID: toolEvt.ID, SessionID: in.sessionID, ExecutionID: in.execID,
		Kind: "tool_call", Payload: string(payload),
	})
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

// offeredTools assembles this run's EXECUTABLE inventory (the offered
// set is also the execution inventory — an allowlist binds execution,
// not just the menu): the builtin provider through the agent allowlist,
// the ward plane, the depth-granted delegate tool and the live MCP
// plane (root AND children share it; the allowlist bounds all of it).
// `tools: []` is an EXPLICIT no-tools inventory — but delegation is
// governed by the DEPTH grant, not the allowlist: an orchestrator with
// tools:[] still delegates.
func (a *Adapter) offeredTools(ctx context.Context, events chan domain.TimelineEvent, in runInput) []tools.Tool {
	kept, _, _ := a.inventory(ctx, events, in)
	return kept
}

// inventory assembles the plane ONCE and splits it: tools kept, and the
// reserved-prefixed user tools dropped (the ADK driver names the drops on
// the stream; the legacy loop drops silently).
func (a *Adapter) inventory(ctx context.Context, events chan domain.TimelineEvent, in runInput) (kept, dropped []tools.Tool, collided []string) {
	var offered []tools.Tool // collided (the named return) holds agents whose wire name collides
	if in.toolAllow != nil && len(in.toolAllow) == 0 {
		if a.delegationGranted(in) {
			offered, collided = a.delegationTools(ctx, events, in)
		}
		return offered, nil, collided
	}
	offered = engine.FilterTools(a.toolProv, in.toolAllow)
	offered = append(offered, filterWardTools(wardToolSet(in), in.toolAllow)...)
	if a.delegationGranted(in) {
		var dt []tools.Tool
		dt, collided = a.delegationTools(ctx, events, in)
		offered = append(offered, dt...)
	}

	if a.mcpTools != nil {
		offered = append(offered, filterWardTools(a.mcpTools(), in.toolAllow)...)
	}
	// The `karta_` prefix is RESERVED for engine tools (they would shadow
	// an engine tool or break a notice's pointer).
	for _, t := range offered {
		if strings.HasPrefix(t.Def().Name, reservedToolPrefix) {
			dropped = append(dropped, t)
			continue
		}
		kept = append(kept, t)
	}
	return kept, dropped, collided
}

// delegationGranted: this run may delegate (agent grant, bounded by the
// operator's depth cap) and there is a roster to read.
func (a *Adapter) delegationGranted(in runInput) bool {
	return in.depth < in.grant && in.depth < a.depthCap && a.snapFn != nil
}

// delegationTools: the spine's per-agent task tools (decision D1); the
// legacy loop keeps its single dispatch tool.
func (a *Adapter) delegationTools(ctx context.Context, events chan domain.TimelineEvent, in runInput) (tools_ []tools.Tool, collided []string) {
	if a.loop == LoopADK {
		return a.perAgentDelegationTools(ctx, events, in)
	}
	return []tools.Tool{a.delegateTool(ctx, events, in)}, nil
}

// dispatchDelegate runs the legacy dispatch form (`delegate_agent`,
// {agent, task}) — on the spine it is the alias prompts that still name
// it reach through the unknown-tool handler.
func (a *Adapter) dispatchDelegate(ctx context.Context, events chan domain.TimelineEvent, in runInput, argsJSON string) tools.Result {
	return a.delegateTool(ctx, events, in).Exec(ctx, argsJSON)
}
