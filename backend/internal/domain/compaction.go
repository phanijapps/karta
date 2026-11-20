package domain

import "context"

// Compactor encapsulates the context-window compaction algorithm. The
// algorithm lives in internal/compaction/ and operates purely on domain
// types; engines integrate it through a thin middleware adapter and never
// implement compaction policy themselves (see backend/AGENTS.md).
type Compactor interface {
	// ShouldCompact reports whether the current context state warrants
	// compaction (evaluated before each LLM call by engine middleware).
	ShouldCompact(ctx context.Context, state ContextState) bool
	// Compact produces a replacement message slice fitting the budget,
	// with statistics for the CompactionTriggered timeline event.
	Compact(ctx context.Context, messages []Message, budget TokenBudget) ([]Message, CompactionStats, error)
}

// Message is one conversation message, engine-agnostic.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// Tokens is the message's token count when known; 0 when unknown.
	Tokens int `json:"tokens,omitempty"`
}

// Role is a message author role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// TokenBudget bounds the context window for compaction decisions.
type TokenBudget struct {
	// MaxTotalTokens is the model's context window.
	MaxTotalTokens int `json:"max_total_tokens"`
	// ReserveOutput is the token count reserved for the model's reply.
	ReserveOutput int `json:"reserve_output"`
}

// ContextState is the token accounting snapshot evaluated by ShouldCompact.
type ContextState struct {
	SessionID    string      `json:"session_id"`
	TotalTokens  int         `json:"total_tokens"`
	MessageCount int         `json:"message_count"`
	Budget       TokenBudget `json:"budget"`
}

// CompactionStats reports what one compaction pass did. Carried on the
// CompactionTriggered timeline event so compaction is always observable.
type CompactionStats struct {
	Strategy       string `json:"strategy"`
	TokensBefore   int    `json:"tokens_before"`
	TokensAfter    int    `json:"tokens_after"`
	MessagesBefore int    `json:"messages_before"`
	MessagesAfter  int    `json:"messages_after"`
}
