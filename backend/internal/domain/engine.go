package domain

import "context"

// AgentEngine abstracts the underlying orchestration framework (CloudWeGo
// Eino by default; Google ADK Go, a custom ReAct loop, or any future Go
// framework behind the same seam). Implementations live in
// internal/engine/<name>/ and are selected by configuration; nothing outside
// an engine package may import the framework directly (see backend/AGENTS.md).
type AgentEngine interface {
	// Name identifies the engine (e.g. "eino").
	Name() string
	// RunGoal starts an autonomous run toward req.Goal, streaming every step
	// as TimelineEvents on the returned channel until the run completes or
	// ctx is cancelled.
	RunGoal(ctx context.Context, req GoalRequest) (<-chan TimelineEvent, error)
	// Resume continues a waiting session with user input, streaming events
	// like RunGoal.
	Resume(ctx context.Context, sessionID string, input UserInput) (<-chan TimelineEvent, error)
	// Cancel stops a session's active run.
	Cancel(ctx context.Context, sessionID string) error
}

// GoalRequest starts a goal run.
type GoalRequest struct {
	// SessionID optionally names the session; the daemon assigns one when empty.
	SessionID string `json:"session_id,omitempty"`
	// Goal is the objective for the autonomous run.
	Goal string `json:"goal"`
	// Agent optionally selects a registry agent; the default agent is used when empty.
	Agent string `json:"agent,omitempty"`
	// Model optionally names the qualified provider/model ("provider/model-id").
	Model string `json:"model,omitempty"`
	// Context carries prior conversation messages into the run, when resuming
	// history client-side.
	Context []Message `json:"context,omitempty"`
	// MaxSteps optionally bounds this run's model calls (advisory: 0 =
	// the engine default; values above the engine's hard cap clamp to
	// it). The intent middleware sets it from the analysis complexity.
	MaxSteps int `json:"max_steps,omitempty"`
	// SystemPrompt overrides the ROOT run's system prompt. json:"-":
	// it is daemon-only wiring (the intent middleware's composed
	// prompt) — never client input, never contract-visible.
	SystemPrompt string `json:"-"`
}

// UserInput is a user turn delivered to a waiting session.
type UserInput struct {
	Text string `json:"text"`
}
