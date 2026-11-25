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

	// EventAnalysisStarted — the intent-analyst's call went out (the
	// trail's first event; the payload rides IntentAnalysis on all four
	// analysis kinds).
	EventAnalysisStarted TimelineEventKind = "analysis_started"
	// EventAnalysisCompleted — the decision landed: the contract + its
	// cost (tokens, ms, attempts). Terminal.
	EventAnalysisCompleted TimelineEventKind = "analysis_completed"
	// EventAnalysisBypassed — the router or the kill-switch skipped
	// analysis (reason in the payload). Terminal.
	EventAnalysisBypassed TimelineEventKind = "analysis_bypassed"
	// EventAnalysisFailed — the decision failed; the run proceeds
	// without it (reason + attempts in the payload). Terminal.
	EventAnalysisFailed TimelineEventKind = "analysis_failed"
	// EventWardEntered — a session entered or switched into an existing
	// ward (hosting is observable; constitution rule 4).
	EventWardEntered TimelineEventKind = "ward_entered"
	// EventWardCreated — a session instantiated a new ward from an
	// archetype and entered it.
	EventWardCreated TimelineEventKind = "ward_created"
	// EventChatDelta — a VOLATILE streamed chat fragment (reasoning or
	// content) during a send; ephemeral typing notification, never
	// persisted to the timeline (the persisted rows are the truth).
	EventChatDelta TimelineEventKind = "chat_delta"
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
	// IntentAnalysis carries the pre-execution analysis outcome
	// (exactly one of analyzed/bypassed/failed).
	IntentAnalysis *IntentAnalysisPayload `json:"intent_analysis,omitempty"`
	Ward           *WardEventPayload      `json:"ward,omitempty"`
}

// IntentAnalysisPayload details one pre-execution intent analysis. A
// failed analysis is NON-FATAL: the run proceeds without one and
// Reason says why. The cost fields (Model, TokensIn/Out, DurationMS,
// Attempts, Trimmed) are filled on the terminal kinds of an analyzed
// run; the window fields on every analyzed event.
type IntentAnalysisPayload struct {
	Status        IntentAnalysisStatus `json:"status"`
	Reason        string               `json:"reason,omitempty"`
	PrimaryIntent string               `json:"primary_intent,omitempty"`
	Complexity    IntentComplexity     `json:"complexity,omitempty"`
	StepBudget    int                  `json:"step_budget,omitempty"`
	Approach      ExecutionApproach    `json:"approach,omitempty"`
	WardAction    WardAction           `json:"ward_action,omitempty"`
	WardName      string               `json:"ward_name,omitempty"`
	// RecommendedSkills/Agents carry the SANITIZED (provisioned)
	// rosters — the session-state surface renders them (the analysis
	// card's skill chips and the context panel's roster).
	RecommendedSkills []string `json:"recommended_skills,omitempty"`
	RecommendedAgents []string `json:"recommended_agents,omitempty"`
	Dropped           []string `json:"dropped,omitempty"`

	// Model is the resolved analysis model as "provider/model".
	Model string `json:"model,omitempty"`
	// Mode is the output mode the analyst used ("text_json" until the
	// provider-mode ladder lands).
	Mode string `json:"mode,omitempty"`
	// ContextWindow + WindowSource: the model-derived limits the decision
	// ran under.
	ContextWindow int                `json:"context_window,omitempty"`
	WindowSource  IntentWindowSource `json:"window_source,omitempty"`
	// TokensIn/Out sum over every attempt; DurationMS covers the whole
	// decision; Attempts counts completions including correction retries.
	TokensIn   int64 `json:"tokens_in,omitempty"`
	TokensOut  int64 `json:"tokens_out,omitempty"`
	DurationMS int64 `json:"duration_ms,omitempty"`
	Attempts   int   `json:"attempts,omitempty"`
	// Trimmed: the one safety valve fired (packet caps shrank to fit
	// window − reserve).
	Trimmed bool `json:"trimmed,omitempty"`
}

// IntentWindowSource says where the analysis model's context window came
// from (the resolution chain: discovered → table → fallback).
type IntentWindowSource string

const (
	// WindowOllama — discovered from the provider (Ollama /api/show).
	WindowOllama IntentWindowSource = "ollama"
	// WindowTable — the built-in or user-editable limits table.
	WindowTable IntentWindowSource = "table"
	// WindowFallback — unknown model, the conservative default.
	WindowFallback IntentWindowSource = "fallback"
)

// WardEventPayload details a ward hosting transition (entered /
// switched / created). Previous names the ward a switch left.
type WardEventPayload struct {
	Ward      string `json:"ward"`
	Archetype string `json:"archetype"`
	Previous  string `json:"previous,omitempty"`
	Digest    string `json:"digest,omitempty"`
}

// IntentAnalysisStatus is the analysis outcome.
type IntentAnalysisStatus string

const (
	// IntentStatusStarted marks the in-flight call (analysis_started).
	IntentStatusStarted IntentAnalysisStatus = "started"
	// IntentStatusAnalyzed marks a successful analysis.
	IntentStatusAnalyzed IntentAnalysisStatus = "analyzed"
	// IntentStatusBypassed marks a trivial goal (deterministic router).
	IntentStatusBypassed IntentAnalysisStatus = "bypassed"
	// IntentStatusFailed marks a non-fatal analysis failure.
	IntentStatusFailed IntentAnalysisStatus = "failed"
)

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
