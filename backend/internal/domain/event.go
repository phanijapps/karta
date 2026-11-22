package domain

import "time"

// TimelineEventKind enumerates the kinds of events in the Timeline Tree.
// Every agent step, delegation, tool call, hook decision, and compaction
// pass emits one; the set here and the contract's event schema must stay
// aligned (contracts/openapi.yaml).
type TimelineEventKind string

const (
	// EventSession — session lifecycle transitions (started, waiting, ...).
	EventSession TimelineEventKind = "session"
	// EventAgentStep — one agent reasoning/writing step.
	EventAgentStep TimelineEventKind = "agent_step"
	// EventDelegation — the agent spawned a sub-agent for a task.
	EventDelegation TimelineEventKind = "delegation"
	// EventToolCall — a tool was invoked (with its outcome).
	EventToolCall TimelineEventKind = "tool_call"
	// EventHookDecision — lifecycle hooks decided allow/block/warn.
	EventHookDecision TimelineEventKind = "hook_decision"
	// EventCompactionTriggered — the context window was compacted.
	EventCompactionTriggered TimelineEventKind = "compaction_triggered"
	// EventChittaToolInvoked — one chitta tool call (memory_*/knowledge_*),
	// success or failure — every chitta operation is observable.
	EventChittaToolInvoked TimelineEventKind = "chitta_tool_invoked"
)

// TimelineEvent is a single observable node in the Timeline Tree. Events
// stream to all clients over SSE and persist for recall.
type TimelineEvent struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	// ParentID nests events (a delegation's children reference it); empty at root.
	ParentID string `json:"parent_id,omitempty"`
	// Agent names the acting agent (root or sub-agent).
	Agent     string            `json:"agent,omitempty"`
	Kind      TimelineEventKind `json:"kind"`
	Timestamp time.Time         `json:"timestamp"`
	// Text is the human-readable description of the step.
	Text string `json:"text,omitempty"`

	// Kind-specific payloads; exactly the one matching Kind is set.
	ToolCall   *ToolCallPayload   `json:"tool_call,omitempty"`
	Delegation *DelegationPayload `json:"delegation,omitempty"`
	Hook       *HookDecision      `json:"hook,omitempty"`
	Compaction *CompactionStats   `json:"compaction,omitempty"`
	// ChittaToolInvoked carries one chitta tool invocation's observability
	// record (tool, outcome, affected ids, host provenance).
	ChittaToolInvoked *ChittaToolInvokedPayload `json:"chitta_tool_invoked,omitempty"`
}

// ChittaToolInvokedPayload details one chitta tool call. Publication is
// best-effort; a failed publication never fails the tool call.
type ChittaToolInvokedPayload struct {
	Tool        string            `json:"tool"`
	Outcome     string            `json:"outcome"` // "success" | "error"
	ErrorCode   string            `json:"error_code,omitempty"`
	AffectedIDs []string          `json:"affected_ids"`
	Provenance  *MemoryProvenance `json:"provenance,omitempty"`
}

// ToolCallPayload details one tool invocation.
type ToolCallPayload struct {
	Tool       string         `json:"tool"`
	Input      map[string]any `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	Error      string         `json:"error,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
}

// DelegationPayload details a sub-agent delegation.
type DelegationPayload struct {
	ChildAgent     string `json:"child_agent"`
	ChildSessionID string `json:"child_session_id,omitempty"`
	Task           string `json:"task"`
}
