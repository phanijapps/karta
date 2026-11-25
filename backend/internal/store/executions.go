package store

// Execution reads for the run's shape and cost (run-legibility): a
// child's usage read back after its loop ends, and the session's whole
// execution tree for the state surface.

import (
	"fmt"
	"time"
)

// ExecutionInfo is one execution row as clients see it.
type ExecutionInfo struct {
	ID         string
	ParentID   string
	Agent      string
	Delegation string // "root" for the root run, else the delegated agent's name
	Task       string
	Status     string
	TokensIn   int64
	TokensOut  int64
	StartedAt  time.Time
	FinishedAt time.Time // zero while running
}

// Role names the execution's part: root, analyst, or delegated.
func (e ExecutionInfo) Role() string {
	switch e.Delegation {
	case "root":
		return "root"
	case "intent-analyst":
		return "analyst"
	default:
		return "delegated"
	}
}

// SetExecutionAgent records the acting agent on an execution row (the
// root row is created before the run knows its agent; rows created
// before this existed carry the schema default "default").
func (s *Store) SetExecutionAgent(execID, agent string) error {
	if _, err := s.db.Exec(`UPDATE executions SET agent = ? WHERE id = ?`, agent, execID); err != nil {
		return fmt.Errorf("store: set execution agent: %w", err)
	}
	return nil
}

// ExecutionUsage reads one execution's terminal usage (the child's
// tokens are rolled in by FinishExecution before this is called).
func (s *Store) ExecutionUsage(execID string) (tokensIn, tokensOut int64, status string, err error) {
	var e ExecutionInfo
	if err := s.db.QueryRow(`SELECT status, tokens_in, tokens_out FROM executions WHERE id = ?`, execID).
		Scan(&e.Status, &e.TokensIn, &e.TokensOut); err != nil {
		return 0, 0, "", fmt.Errorf("store: execution usage: %w", err)
	}
	return e.TokensIn, e.TokensOut, e.Status, nil
}

// MaxSessionExecutions bounds the state surface's execution list (the
// contract's maxItems); a session never approaches it (one run = root +
// analyst + a few children).
const MaxSessionExecutions = 256

// SessionExecutions lists the session's execution tree in creation
// order (root first by convention), bounded.
func (s *Store) SessionExecutions(sessionID string) ([]ExecutionInfo, error) {
	rows, err := s.db.Query(`SELECT id, COALESCE(parent_id,''), agent, delegation, COALESCE(task,''), status,
		tokens_in, tokens_out, created_at, COALESCE(finished_at,'')
		FROM executions WHERE session_id = ? ORDER BY rowid LIMIT ?`, sessionID, MaxSessionExecutions)
	if err != nil {
		return nil, fmt.Errorf("store: session executions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ExecutionInfo
	for rows.Next() {
		var e ExecutionInfo
		var created, finished string
		if err := rows.Scan(&e.ID, &e.ParentID, &e.Agent, &e.Delegation, &e.Task, &e.Status, &e.TokensIn, &e.TokensOut, &created, &finished); err != nil {
			return nil, fmt.Errorf("store: session executions scan: %w", err)
		}
		e.StartedAt, _ = time.Parse(time.RFC3339Nano, created)
		if finished != "" {
			e.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
