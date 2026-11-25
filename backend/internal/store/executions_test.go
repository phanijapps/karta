package store

import (
	"fmt"
	"strings"
	"testing"
)

// The intent-analyst's decision row: ensured root parent, the agent on
// both agent and delegation columns, the goal as task, and FinishExecution
// rolling tokens in.
func TestCreateAnalysisExecution(t *testing.T) {
	s, err := Open(t.TempDir() + "/exec.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// An ad-hoc session (no row yet) gets its research row + root exec.
	id, err := s.CreateAnalysisExecution("goal-7", "intent-analyst", "compare AMD and NVIDIA")
	if err != nil {
		t.Fatal(err)
	}
	var parent, agent, delegation, status, task string
	if err := s.db.QueryRow(`SELECT parent_id, agent, delegation, status, task FROM executions WHERE id = ?`, id).
		Scan(&parent, &agent, &delegation, &status, &task); err != nil {
		t.Fatal(err)
	}
	if parent != "exec-root-goal-7" || agent != "intent-analyst" || delegation != "intent-analyst" || status != "running" || task != "compare AMD and NVIDIA" {
		t.Fatalf("row: %s %s %s %s %q", parent, agent, delegation, status, task)
	}
	if err := s.FinishExecution(id, "completed", "", 1200, 180); err != nil {
		t.Fatal(err)
	}
	var tin, tout int64
	var typ string
	if err := s.db.QueryRow(`SELECT status, tokens_in, tokens_out FROM executions WHERE id = ?`, id).Scan(&status, &tin, &tout); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || tin != 1200 || tout != 180 {
		t.Fatalf("finished: %s %d %d", status, tin, tout)
	}
	if err := s.db.QueryRow(`SELECT type FROM sessions WHERE id = 'goal-7'`).Scan(&typ); err != nil || typ != "research" {
		t.Fatalf("ad-hoc session typed research: %q %v", typ, err)
	}
	// A long goal is bounded on the task column.
	id2, err := s.CreateAnalysisExecution("goal-7", "intent-analyst", strings.Repeat("x", 2000))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT length(task) FROM executions WHERE id = ?`, id2).Scan(&tin); err != nil || tin != 512 {
		t.Fatalf("task bound: %d %v", tin, err)
	}
}

// The usage read-back and the session's execution tree (run-legibility).
func TestExecutionUsageAndSessionExecutions(t *testing.T) {
	s, err := Open(t.TempDir() + "/exec2.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root, err := s.EnsureSession("s-exec", "research")
	if err != nil {
		t.Fatal(err)
	}
	analyst, _ := s.CreateAnalysisExecution("s-exec", "intent-analyst", "analyze")
	child, _ := s.CreateChildExecution("s-exec", root, "research-agent")
	if _, _, status, err := s.ExecutionUsage(child); err != nil || status != "running" {
		t.Fatalf("usage while running: %s %v", status, err)
	}
	_ = s.FinishExecution(child, "completed", "", 1102, 7961)
	_ = s.FinishExecution(analyst, "completed", "", 1911, 317)
	_ = s.FinishExecution(root, "completed", "", 721214, 36832)
	tin, tout, status, err := s.ExecutionUsage(child)
	if err != nil || status != "completed" || tin != 1102 || tout != 7961 {
		t.Fatalf("usage: %d/%d %s %v", tin, tout, status, err)
	}
	tree, err := s.SessionExecutions("s-exec")
	if err != nil || len(tree) != 3 {
		t.Fatalf("tree: %v %+v", err, tree)
	}
	if tree[0].ID != root || tree[0].Role() != "root" || tree[0].FinishedAt.IsZero() || tree[0].TokensIn != 721214 {
		t.Fatalf("root: %+v", tree[0])
	}
	if tree[1].Role() != "analyst" || tree[1].TokensIn != 1911 {
		t.Fatalf("analyst: %+v", tree[1])
	}
	if tree[2].Role() != "delegated" || tree[2].ParentID != root || tree[2].Delegation != "research-agent" || tree[2].FinishedAt.IsZero() {
		t.Fatalf("child: %+v", tree[2])
	}
}

// Rows carry their agent; databases from before the column exists gain
// it on open (legacy rows read as root).
func TestMigrateMessagesAgent(t *testing.T) {
	path := t.TempDir() + "/legacy-agent.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := s.EnsureSession("s-agent", "research")
	if _, err := s.Append(Message{ID: "m1", SessionID: "s-agent", ExecutionID: root, Role: "assistant", Content: "x", Agent: "research-agent"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-agent database: drop the column's data by recreating
	// the table without it, then reopen.
	if _, err := s.db.Exec(`ALTER TABLE messages RENAME TO messages_new`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, execution_id TEXT NOT NULL,
		seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, reasoning TEXT, tool_calls TEXT, tool_call_id TEXT,
		tool_name TEXT, provider TEXT, model TEXT, prompt_tokens INTEGER, completion_tokens INTEGER, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO messages SELECT id, session_id, execution_id, seq, role, content, reasoning, tool_calls, tool_call_id, tool_name, provider, model, prompt_tokens, completion_tokens, created_at FROM messages_new`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE messages_new`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen with legacy schema: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	rows, err := s2.Replay("s-agent", 0, 10)
	if err != nil || len(rows) != 1 || rows[0].Agent != "" {
		t.Fatalf("legacy row reads as root (empty agent): %+v %v", rows, err)
	}
	if _, err := s2.Append(Message{ID: "m2", SessionID: "s-agent", ExecutionID: root, Role: "assistant", Content: "y", Agent: "root"}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s2.Replay("s-agent", 0, 10)
	if rows[1].Agent != "root" {
		t.Fatalf("new rows keep their agent: %+v", rows[1])
	}
	// Idempotent on reopen.
	_ = s2.Close()
	if s3, err := Open(path); err != nil {
		t.Fatalf("reopen: %v", err)
	} else {
		_ = s3.Close()
	}
}

// The execution list keeps every root row and the NEWEST bounded set of
// the rest, in creation order.
func TestSessionExecutionsKeepsRootAndNewest(t *testing.T) {
	s, err := Open(t.TempDir() + "/exec3.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root, _ := s.EnsureSession("s-many", "research")
	for i := 0; i < MaxSessionExecutions+5; i++ {
		if _, err := s.CreateChildExecution("s-many", root, fmt.Sprintf("agent-%03d", i%7)); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := s.SessionExecutions("s-many")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != MaxSessionExecutions+1 {
		t.Fatalf("want root + %d newest, got %d", MaxSessionExecutions, len(tree))
	}
	if tree[0].ID != root || tree[0].Role() != "root" {
		t.Fatalf("root first: %+v", tree[0])
	}
	if !tree[len(tree)-1].StartedAt.After(tree[1].StartedAt) {
		t.Fatalf("creation order broken")
	}
}
