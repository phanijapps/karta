package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// HistoryWindow is the compaction-aware, exchange-complete provider
// context view for one scope. The append log is session-wide; the
// context view is execution-scoped (root scope = NULL execution_id in
// compactions). Rules (design D3):
//   - the latest in-scope compaction marker contributes exactly its
//     summary row (mapped to a system message by the caller), and the
//     suffix contains rows with seq > base_seq EXCLUDING summary rows;
//   - without a marker the suffix is the bounded tail;
//   - the suffix front is trimmed ONLY at exchange boundaries: a window
//     never starts with a tool row whose assistant call is outside it,
//     nor with an assistant row whose tool_calls lack their results;
//   - NeedsCompaction reports that the untrimmed suffix exceeded Bound
//     (the compaction change consumes this signal).
type HistoryWindow struct {
	Summary         *Message // latest marker's summary row (nil when none)
	Rows            []Message
	Bound           int
	NeedsCompaction bool
}

// HistoryWindow computes the provider context for the root scope
// (executionID "" selects markers with NULL execution_id).
func (s *Store) HistoryWindow(sessionID, executionID string, bound int) (HistoryWindow, error) {
	hw := HistoryWindow{Bound: bound}

	var baseSeq int64
	var summaryID string
	var err error
	if executionID == "" {
		err = s.db.QueryRow(`SELECT base_seq, summary_message_id FROM compactions
			WHERE session_id = ? AND execution_id IS NULL ORDER BY base_seq DESC LIMIT 1`, sessionID).
			Scan(&baseSeq, &summaryID)
	} else {
		err = s.db.QueryRow(`SELECT base_seq, summary_message_id FROM compactions
			WHERE session_id = ? AND execution_id = ? ORDER BY base_seq DESC LIMIT 1`, sessionID, executionID).
			Scan(&baseSeq, &summaryID)
	}
	if err == nil {
		sum, err := s.messageByID(summaryID)
		if err != nil {
			return hw, err
		}
		hw.Summary = &sum
	} else if !errors.Is(err, sql.ErrNoRows) {
		return hw, fmt.Errorf("store: marker lookup: %w", err)
	}

	// Untrimmed suffix (excluding summary rows) after the marker.
	suffix, err := s.replaySince(sessionID, baseSeq, 1<<30)
	if err != nil {
		return hw, err
	}
	if len(suffix) > bound {
		hw.NeedsCompaction = true
		// Keep the bounded TAIL, then trim the front to an exchange boundary.
		suffix = suffix[len(suffix)-bound:]
		suffix = trimToExchangeBoundary(suffix)
	} else {
		suffix = trimToExchangeBoundary(suffix)
	}
	hw.Rows = suffix
	return hw, nil
}

// RootWindow returns the ROOT execution's own rows for provider context,
// compaction-aware: the session's latest root marker (base_seq + summary)
// applies as the window floor, and only rows of the ROOT execution above
// it are returned (child transcripts excluded). The summary rides on the
// returned slice's Summary field via RootWindowFull when callers want it.
func (s *Store) RootWindow(sessionID, rootExecID string, bound int) ([]Message, error) {
	hw, err := s.RootWindowFull(sessionID, rootExecID, bound)
	if err != nil {
		return nil, err
	}
	return hw.Rows, nil
}

// RootWindowFull is RootWindow with the compaction summary attached.
func (s *Store) RootWindowFull(sessionID, rootExecID string, bound int) (HistoryWindow, error) {
	hw := HistoryWindow{Bound: bound}
	var baseSeq int64
	var summaryID string
	err := s.db.QueryRow(`SELECT base_seq, summary_message_id FROM compactions
		WHERE session_id = ? AND execution_id IS NULL ORDER BY base_seq DESC LIMIT 1`, sessionID).
		Scan(&baseSeq, &summaryID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return hw, fmt.Errorf("store: root window marker: %w", err)
	}
	if summaryID != "" {
		sum, err := s.messageByID(summaryID)
		if err != nil {
			// A marker whose summary row is unreadable would silently
			// DROP compacted context — surface it instead.
			return hw, fmt.Errorf("store: root window summary %s: %w", summaryID, err)
		}
		hw.Summary = &sum
	}
	rows, err := s.db.Query(`SELECT id, session_id, execution_id, seq, role, content, COALESCE(agent, ''),
		COALESCE(reasoning, ''), COALESCE(tool_calls, ''), COALESCE(tool_call_id, ''), COALESCE(tool_name, ''),
		COALESCE(provider, ''), COALESCE(model, ''), prompt_tokens, completion_tokens, created_at
		FROM messages WHERE session_id = ? AND execution_id = ? AND role != 'summary' AND seq > ?
		ORDER BY seq DESC LIMIT ?`, sessionID, rootExecID, baseSeq, bound)
	if err != nil {
		return hw, fmt.Errorf("store: root window: %w", err)
	}
	tail, err := scanMessages(rows)
	if err != nil {
		return hw, err
	}
	// scan order is DESC (tail selection); reverse to chronological, then
	// trim the front to an exchange boundary (no orphan tool results at
	// the context start).
	for i, j := 0, len(tail)-1; i < j; i, j = i+1, j-1 {
		tail[i], tail[j] = tail[j], tail[i]
	}
	hw.Rows = trimToExchangeBoundary(tail)
	return hw, nil
}

// replaySince returns non-summary rows with seq > afterSeq in order.
func (s *Store) replaySince(sessionID string, afterSeq int64, limit int) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, session_id, execution_id, seq, role, content, COALESCE(agent, ''),
		COALESCE(reasoning, ''), COALESCE(tool_calls, ''), COALESCE(tool_call_id, ''), COALESCE(tool_name, ''),
		COALESCE(provider, ''), COALESCE(model, ''), prompt_tokens, completion_tokens, created_at
		FROM messages WHERE session_id = ? AND seq > ? AND role != 'summary'
		ORDER BY seq LIMIT ?`, sessionID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("store: window replay: %w", err)
	}
	return scanMessages(rows)
}

func (s *Store) messageByID(id string) (Message, error) {
	rows, err := s.db.Query(`SELECT id, session_id, execution_id, seq, role, content, COALESCE(agent, ''),
		COALESCE(reasoning, ''), COALESCE(tool_calls, ''), COALESCE(tool_call_id, ''), COALESCE(tool_name, ''),
		COALESCE(provider, ''), COALESCE(model, ''), prompt_tokens, completion_tokens, created_at
		FROM messages WHERE id = ?`, id)
	if err != nil {
		return Message{}, fmt.Errorf("store: message by id: %w", err)
	}
	msgs, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, fmt.Errorf("store: summary message %q not found", id)
	}
	return msgs[0], nil
}

// trimToExchangeBoundary drops leading rows that would start the window
// mid-exchange: tool rows whose assistant call is outside the window,
// and an assistant row carrying tool_calls whose results are outside.
func trimToExchangeBoundary(rows []Message) []Message {
	for len(rows) > 0 {
		first := rows[0]
		switch {
		case first.Role == "tool":
			// Its assistant intent row is outside — drop the orphan.
			rows = rows[1:]
		case first.Role == "assistant" && first.ToolCalls != "":
			// Intent row retained but its tool results are outside the
			// window (next row is not its tool result) — drop the group head.
			if len(rows) < 2 || rows[1].Role != "tool" {
				rows = rows[1:]
				continue
			}
			return rows
		default:
			return rows
		}
	}
	return rows
}
