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

// LatestEventOfKind returns the session's most recent event of one
// kind (nil when none) — the session-state surface reads the latest
// intent_analysis this way.
func (s *Store) LatestEventOfKind(sessionID, kind string) (*Event, error) {
	var e Event
	var execID, parentID sql.NullString
	// rowid = insertion order (single writer): strictly monotonic,
	// unlike the lexicographic RFC3339Nano text whose variable-width
	// fractions (".1Z" vs ".11Z") can misorder.
	err := s.db.QueryRow(`SELECT id, session_id, execution_id, parent_id, kind, payload, created_at
		FROM timeline_events WHERE session_id = ? AND kind = ? ORDER BY rowid DESC LIMIT 1`,
		sessionID, kind).Scan(&e.ID, &e.SessionID, &execID, &parentID, &e.Kind, &e.Payload, &e.CreatedAt)
	e.ExecutionID, e.ParentID = execID.String, parentID.String
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: latest event of kind: %w", err)
	}
	return &e, nil
}

// ActiveRuns returns the session ids with a live engine run (the
// sidebar's working badges). Root executions are per-session by
// convention (exec-root-<id>): a non-terminal status means the run
// is live.
func (s *Store) ActiveRuns() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT session_id FROM executions
		WHERE id = 'exec-root-' || session_id AND finished_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("store: active runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		out[sid] = true
	}
	return out, rows.Err()
}

// ExecutionResult reads one execution's terminal status and error (the
// research send path consumes the root execution's outcome after a run
// drains — failures surface as typed exchange errors, not silent
// non-answers).
func (s *Store) ExecutionResult(execID string) (status, errMsg string, err error) {
	var e sql.NullString
	if err := s.db.QueryRow(`SELECT status, error FROM executions WHERE id = ?`, execID).Scan(&status, &e); err != nil {
		if err == sql.ErrNoRows {
			return "", "", nil
		}
		return "", "", fmt.Errorf("store: execution result: %w", err)
	}
	if e.Valid {
		errMsg = e.String
	}
	return status, errMsg, nil
}
