package domain

import "context"

// HookRunner executes Claude Code-style lifecycle hooks declared in
// ~/.zbot/hooks/*.yaml (top-level root). The executor lives in internal/hooks/.
type HookRunner interface {
	// Run executes the hooks matching event and returns the combined
	// decision. Exit-code semantics: 0 -> allow (optionally modifying input
	// or injecting context), 2 -> block with stderr surfaced to the agent as
	// a recoverable error, other non-zero -> warn and continue.
	Run(ctx context.Context, event HookEvent) (HookDecision, error)
}

// HookEventName enumerates lifecycle hook trigger points.
type HookEventName string

const (
	// HookPreToolUse — HookEventName values — the lifecycle points where hooks run.
	HookPreToolUse HookEventName = "PreToolUse"
	// HookPostToolUse — after a tool call, with its outcome.
	HookPostToolUse HookEventName = "PostToolUse"
	// HookPreCompact — one of the HookEventName values.
	HookPreCompact HookEventName = "PreCompact"
	// HookSessionStart — one of the HookEventName values.
	HookSessionStart HookEventName = "SessionStart"
	// HookSessionEnd — one of the HookEventName values.
	HookSessionEnd HookEventName = "SessionEnd"
	// HookSubagentStop — one of the HookEventName values.
	HookSubagentStop HookEventName = "SubagentStop"
)

// HookEvent is the payload delivered to hook commands as JSON on stdin.
type HookEvent struct {
	Event     HookEventName `json:"event"`
	SessionID string        `json:"session_id"`
	// ToolName is set for tool events; hook matchers are regexes over it.
	ToolName string `json:"tool_name,omitempty"`
	// ToolInput carries the tool call arguments, when relevant.
	ToolInput map[string]any `json:"tool_input,omitempty"`
	// Payload carries kind-specific extras (e.g. compaction stats).
	Payload map[string]any `json:"payload,omitempty"`
}

// HookAction is the outcome of running hooks for one event.
type HookAction string

const (
	// HookAllow — proceed (hook exit 0). Context may carry injected context.
	HookAllow HookAction = "allow"
	// HookBlock — stop the action; Reason is surfaced to the agent as a
	// recoverable error (hook exit 2, from stderr).
	HookBlock HookAction = "block"
	// HookWarn — log a warning event and continue (other non-zero exit).
	HookWarn HookAction = "warn"
)

// HookDecision is the combined outcome for a HookEvent.
type HookDecision struct {
	Action HookAction `json:"action"`
	// Reason explains a block and is surfaced to the agent.
	Reason string `json:"reason,omitempty"`
	// Context optionally injects context into the agent conversation
	// (allowed hook stdout).
	Context map[string]any `json:"context,omitempty"`
}
