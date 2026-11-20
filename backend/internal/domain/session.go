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
	SessionCreated   SessionStatus = "created"
	SessionRunning   SessionStatus = "running"
	SessionWaiting   SessionStatus = "waiting" // paused, awaiting user input
	SessionCompleted SessionStatus = "completed"
	SessionCancelled SessionStatus = "cancelled"
	SessionFailed    SessionStatus = "failed"
)
