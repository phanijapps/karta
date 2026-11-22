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
