package store

import (
	"database/sql"
	"fmt"
)

// Event is one durable timeline event row (persisted BEFORE any volatile
// notification — the durable-first contract).
type Event struct {
	ID          string
	SessionID   string
	ExecutionID string
	ParentID    string
	Kind        string // domain.TimelineEventKind values
	Payload     string // JSON kind-specific payload
	CreatedAt   string // RFC3339Nano UTC
}

// AppendEvent durably records one timeline event.
func (s *Store) AppendEvent(e Event) error {
	if e.ID == "" || e.SessionID == "" || e.Kind == "" {
		return fmt.Errorf("store: event requires id, session, kind")
	}
	if e.CreatedAt == "" {
		e.CreatedAt = now()
	}
	_, err := s.db.Exec(`INSERT INTO timeline_events (id, session_id, execution_id, parent_id, kind, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.SessionID, nullStr(e.ExecutionID), nullStr(e.ParentID), e.Kind, e.Payload, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: append event: %w", err)
	}
	return nil
}

// Events replays durable events ordered by creation (rowid as the stable
// insertion order), bounded; afterID empty replays from the start.
func (s *Store) Events(sessionID, afterID string, limit int) ([]Event, error) {
	q := `SELECT id, COALESCE(execution_id,''), COALESCE(parent_id,''), kind, payload, created_at
		FROM timeline_events WHERE session_id = ?`
	args := []any{sessionID}
	if afterID != "" {
		q += ` AND rowid > (SELECT rowid FROM timeline_events WHERE id = ?)`
		args = append(args, afterID)
	}
	q += ` ORDER BY rowid LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ExecutionID, &e.ParentID, &e.Kind, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: event scan: %w", err)
		}
		e.SessionID = sessionID
		out = append(out, e)
	}
	return out, rows.Err()
}

// Execution updates (engine change consumes; kept minimal here).

// FinishExecution sets a terminal status (+ optional error) and rolls
// request-usage tokens into the execution row.
func (s *Store) FinishExecution(execID, status, errMsg string, tokensIn, tokensOut int64) error {
	var errVal any
	if errMsg != "" {
		errVal = errMsg
	}
	_, err := s.db.Exec(`UPDATE executions SET status = ?, error = ?, tokens_in = tokens_in + ?,
		tokens_out = tokens_out + ?, finished_at = ? WHERE id = ?`,
		status, errVal, tokensIn, tokensOut, now(), execID)
	if err != nil {
		return fmt.Errorf("store: finish execution: %w", err)
	}
	return nil
}

var _ = sql.ErrNoRows
