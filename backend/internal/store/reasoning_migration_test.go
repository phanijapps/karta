package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrateReasoningOnLegacyDB proves the guarded ALTER: a database
// created BEFORE the reasoning column opens cleanly, gains the column, and
// keeps its rows (null reasoning reads back as "").
func TestMigrateReasoningOnLegacyDB(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Legacy schema (pre-reasoning), minimal but shape-compatible.
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at TEXT NOT NULL);
	CREATE TABLE executions (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, parent_id TEXT, kind TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL);
	CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, execution_id TEXT NOT NULL,
		seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, tool_calls TEXT,
		tool_call_id TEXT, tool_name TEXT, provider TEXT, model TEXT,
		prompt_tokens INTEGER, completion_tokens INTEGER, created_at TEXT NOT NULL,
		UNIQUE(session_id, seq));`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, created_at) VALUES ('s1','2020-01-01T00:00:00Z');
		INSERT INTO executions (id, session_id, kind, status, created_at) VALUES ('e1','s1','root','completed','2020-01-01T00:00:00Z');
		INSERT INTO messages (id, session_id, execution_id, seq, role, content, created_at)
		VALUES ('m1','s1','e1',1,'user','legacy row','2020-01-01T00:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer func() { _ = st.Close() }()

	rows, err := st.Replay("s1", 0, 10)
	if err != nil || len(rows) != 1 || rows[0].Content != "legacy row" {
		t.Fatalf("legacy row lost: %v %+v", err, rows)
	}
	if rows[0].Reasoning != "" {
		t.Fatalf("legacy reasoning should read empty, got %q", rows[0].Reasoning)
	}
	// New writes persist reasoning and it round-trips.
	if _, err := st.Append(Message{ID: "m2", SessionID: "s1", ExecutionID: "e1", Role: "assistant", Content: "new", Reasoning: "because"}); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.Replay("s1", 1, 10)
	if len(rows) != 1 || rows[0].Reasoning != "because" {
		t.Fatalf("reasoning round-trip failed: %+v", rows)
	}
}
