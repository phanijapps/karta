// Package store owns Karta's local SQLite database at
// ~/.zbot/data/zbot.db: the eternal chat conversation, its execution
// tree, append-only messages, compaction markers, and durable timeline
// events. Pure-Go driver (modernc.org/sqlite — no cgo). Everything is
// append-only; no API here rewrites or deletes rows. History selection
// for providers goes through HistoryWindow (compaction-aware,
// exchange-complete); display reads go through Replay.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SchemaVersion bumps on schema changes; bootstrap is idempotent.
const SchemaVersion = 1

// Message is one conversation turn row. PromptTokens/CompletionTokens
// are REQUEST usage from the completion response (nullable; not content
// size — they bill the whole request context and are not summable as
// message size). Provider/Model carry provenance on assistant rows.
type Message struct {
	ID               string
	SessionID        string
	ExecutionID      string
	Seq              int64
	Role             string // user|assistant|tool|system|summary
	Content          string
	ToolCalls        string // JSON array (assistant intent rows)
	ToolCallID       string
	ToolName         string
	Provider         string
	Model            string
	PromptTokens     sql.NullInt64
	CompletionTokens sql.NullInt64
	CreatedAt        time.Time
}

// Session is a conversation row ('default' — the eternal chat).
type Session struct {
	ID        string
	Title     string
	CreatedAt time.Time
}

var schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    title       TEXT,
    created_at  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS executions (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    parent_id   TEXT REFERENCES executions(id),
    agent       TEXT NOT NULL DEFAULT 'default',
    task        TEXT,
    status      TEXT NOT NULL,
    delegation  TEXT NOT NULL DEFAULT 'root',
    error       TEXT,
    tokens_in   INTEGER NOT NULL DEFAULT 0,
    tokens_out  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_executions_session ON executions(session_id);
CREATE INDEX IF NOT EXISTS idx_executions_parent  ON executions(parent_id);
CREATE TABLE IF NOT EXISTS messages (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL REFERENCES sessions(id),
    execution_id      TEXT NOT NULL REFERENCES executions(id),
    seq               INTEGER NOT NULL,
    role              TEXT NOT NULL,
    content           TEXT NOT NULL,
    tool_calls        TEXT,
    tool_call_id      TEXT,
    tool_name         TEXT,
    provider          TEXT,
    model             TEXT,
    prompt_tokens     INTEGER,
    completion_tokens INTEGER,
    created_at        TEXT NOT NULL,
    UNIQUE(session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_messages_execution ON messages(execution_id);
CREATE TABLE IF NOT EXISTS compactions (
    id                 TEXT PRIMARY KEY,
    session_id         TEXT NOT NULL REFERENCES sessions(id),
    execution_id       TEXT REFERENCES executions(id),
    base_seq           INTEGER NOT NULL,
    summary_message_id TEXT NOT NULL REFERENCES messages(id),
    tokens_before      INTEGER NOT NULL,
    tokens_after       INTEGER NOT NULL,
    created_at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_compactions_scope ON compactions(session_id, execution_id, base_seq);
CREATE TABLE IF NOT EXISTS timeline_events (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL,
    execution_id TEXT,
    parent_id    TEXT,
    kind         TEXT NOT NULL,
    payload      TEXT NOT NULL,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session ON timeline_events(session_id, created_at);
CREATE TABLE IF NOT EXISTS app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS model_catalog (
    provider     TEXT PRIMARY KEY,
    model_ids    TEXT NOT NULL,
    refreshed_at TEXT NOT NULL
);
PRAGMA user_version = %d;
`

// Store is the local database handle. One writer connection keeps
// SQLite's locking simple; reads share it (SQLite serializes).
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the database at path with WAL mode,
// a busy timeout, and idempotent schema bootstrap.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: ensure %s: %w", filepath.Dir(path), err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if _, err := db.Exec(fmt.Sprintf(schemaSQL, SchemaVersion)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: bootstrap schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// EnsureSession idempotently ensures the session row exists and returns
// its root execution id (created lazily, concurrently safe).
func (s *Store) EnsureSession(sessionID string) (rootExecID string, err error) {
	// Session row.
	if _, err = s.db.Exec(`INSERT INTO sessions (id, created_at) VALUES (?, ?)
		ON CONFLICT(id) DO NOTHING`, sessionID, now()); err != nil {
		return "", fmt.Errorf("store: ensure session: %w", err)
	}
	// Root execution row (one per session by convention: id derived).
	rootExecID = "exec-root-" + sessionID
	_, err = s.db.Exec(`INSERT INTO executions (id, session_id, status, delegation, created_at)
		VALUES (?, ?, 'running', 'root', ?) ON CONFLICT(id) DO NOTHING`, rootExecID, sessionID, now())
	if err != nil {
		return "", fmt.Errorf("store: ensure root execution: %w", err)
	}
	return rootExecID, nil
}

// Append inserts one message with its per-session seq assigned ATOMICALLY
// inside the INSERT (the agentzero trick — no next-seq race).
func (s *Store) Append(m Message) (Message, error) {
	if m.SessionID == "" || m.ExecutionID == "" || m.Role == "" {
		return m, fmt.Errorf("store: append requires session, execution, and role")
	}
	m.CreatedAt = time.Now().UTC()
	res, err := s.db.Exec(`INSERT INTO messages
		(id, session_id, execution_id, seq, role, content, tool_calls, tool_call_id, tool_name,
		 provider, model, prompt_tokens, completion_tokens, created_at)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?),
		        ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.ExecutionID, m.SessionID,
		m.Role, m.Content, nullStr(m.ToolCalls), nullStr(m.ToolCallID), nullStr(m.ToolName),
		nullStr(m.Provider), nullStr(m.Model), m.PromptTokens, m.CompletionTokens, fmtTime(m.CreatedAt))
	if err != nil {
		return m, fmt.Errorf("store: append: %w", err)
	}
	// Read back the assigned seq (single-writer: safe).
	row := s.db.QueryRow(`SELECT seq FROM messages WHERE id = ?`, m.ID)
	if err := row.Scan(&m.Seq); err != nil {
		return m, fmt.Errorf("store: append readback: %w", err)
	}
	_ = res
	return m, nil
}

// Replay returns rows with seq > afterSeq in order, bounded by limit.
func (s *Store) Replay(sessionID string, afterSeq int64, limit int) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, session_id, execution_id, seq, role, content,
		COALESCE(tool_calls, ''), COALESCE(tool_call_id, ''), COALESCE(tool_name, ''),
		COALESCE(provider, ''), COALESCE(model, ''), prompt_tokens, completion_tokens, created_at
		FROM messages WHERE session_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		sessionID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("store: replay: %w", err)
	}
	return scanMessages(rows)
}

// LatestSeq returns the session's highest seq (0 when empty).
func (s *Store) LatestSeq(sessionID string) (int64, error) {
	var seq sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM messages WHERE session_id = ?`, sessionID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("store: latest seq: %w", err)
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}

// Session returns the session row (nil when absent).
func (s *Store) Session(sessionID string) (*Session, error) {
	var sess Session
	var created string
	err := s.db.QueryRow(`SELECT id, COALESCE(title,''), created_at FROM sessions WHERE id = ?`, sessionID).
		Scan(&sess.ID, &sess.Title, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: session: %w", err)
	}
	sess.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &sess, nil
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	defer func() { _ = rows.Close() }()
	var out []Message
	for rows.Next() {
		var m Message
		var created string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ExecutionID, &m.Seq, &m.Role, &m.Content,
			&m.ToolCalls, &m.ToolCallID, &m.ToolName, &m.Provider, &m.Model,
			&m.PromptTokens, &m.CompletionTokens, &created); err != nil {
			return nil, fmt.Errorf("store: scan: %w", err)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func now() string                { return fmtTime(time.Now().UTC()) }
func fmtTime(t time.Time) string { return t.Format(time.RFC3339Nano) }

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ClearSession removes all messages and events for a session (the
// user-initiated "clear chat" — destructive by design).
func (s *Store) ClearSession(sessionID string) error {
	if _, err := s.db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("store: clear messages: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM timeline_events WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("store: clear events: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM compactions WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("store: clear compactions: %w", err)
	}
	return nil
}
