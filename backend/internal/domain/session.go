// Package domain holds z-Bot's pure types and interface seams. It imports
// nothing outside the Go standard library and no other internal package
// (enforced by purity_test.go). Everything else depends on domain; domain
// depends on nothing.
package domain

import "time"

// Session identifies one goal-oriented conversation with the daemon.
type Session struct {
	ID        string        `json:"id"`
	Status    SessionStatus `json:"status"`
	Agent     string        `json:"agent,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// SessionStatus is the lifecycle state of a session.
type SessionStatus string

const (
	// SessionCreated — SessionStatus values — a session lifecycle state.
	SessionCreated SessionStatus = "created"
	// SessionRunning — one of the SessionStatus values.
	SessionRunning SessionStatus = "running"
	// SessionWaiting — one of the SessionStatus values.
	SessionWaiting SessionStatus = "waiting" // paused, awaiting user input
	// SessionCompleted — one of the SessionStatus values.
	SessionCompleted SessionStatus = "completed"
	// SessionCancelled — one of the SessionStatus values.
	SessionCancelled SessionStatus = "cancelled"
	// SessionFailed — one of the SessionStatus values.
	SessionFailed SessionStatus = "failed"
)

// SessionType is a session's pipeline depth, set at creation and
// immutable afterwards (add-chat-modes: the session model — no mode
// toggle; the session's type IS the mode).
type SessionType string

const (
	// SessionTypeChat is the lean pipeline: no intent analysis, no ward
	// tools, no delegation. Memory recall still runs. Exactly ONE such
	// session exists ("default", the persistent quick chat).
	SessionTypeChat SessionType = "chat"
	// SessionTypeResearch is the full pipeline through the engine:
	// intent analysis, skills provision, ward tools, delegation,
	// progress budgets. Many sessions, each its own conversation.
	SessionTypeResearch SessionType = "research"
)

// NormalizeSessionType maps a stored type string to its canonical
// value (unknown → chat, the safe lean default).
func NormalizeSessionType(typ string) SessionType {
	if typ == string(SessionTypeResearch) {
		return SessionTypeResearch
	}
	return SessionTypeChat
}
