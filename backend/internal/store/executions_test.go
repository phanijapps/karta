package store

import (
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
