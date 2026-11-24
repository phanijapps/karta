package store

import (
	"fmt"
	"strings"
	"testing"
)

func TestHistoryWindowWithoutMarker(t *testing.T) {
	s := openMem(t)
	appendAll(t, s, msg("user", "one"), msg("assistant", "two"), msg("user", "three"))
	hw, err := s.HistoryWindow("default", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if hw.Summary != nil || hw.NeedsCompaction || len(hw.Rows) != 3 {
		t.Fatalf("window: %+v", hw)
	}
}

func TestHistoryWindowUsesMarkerAndExcludesSummaries(t *testing.T) {
	s := openMem(t)
	rows := appendAll(t, s,
		msg("user", "old1"), msg("assistant", "old2"),
		msg("summary", "SUMMARY-A"), // seq 3
		msg("user", "new1"), msg("assistant", "new2"),
	)
	markerSeq := rows[1].Seq
	summaryID := rows[2].ID
	root, _ := s.EnsureSession("default", "research")
	if err := s.AppendEvent(Event{ID: "evt-x", SessionID: "default", Kind: "session", Payload: "{}"}); err != nil {
		t.Fatal(err)
	}
	// Insert marker via direct SQL (the compaction change owns the API).
	_, err := s.db.Exec(`INSERT INTO compactions (id, session_id, execution_id, base_seq, summary_message_id, tokens_before, tokens_after, created_at)
		VALUES ('cmp-1', 'default', NULL, ?, ?, 100, 10, ?)`, markerSeq, summaryID, now())
	if err != nil {
		t.Fatal(err)
	}
	_ = root

	hw, err := s.HistoryWindow("default", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if hw.Summary == nil || hw.Summary.ID != summaryID {
		t.Fatalf("summary: %+v", hw.Summary)
	}
	if len(hw.Rows) != 2 || hw.Rows[0].Content != "new1" || hw.Rows[1].Content != "new2" {
		t.Fatalf("suffix must exclude covered rows and summaries: %+v", hw.Rows)
	}
	if hw.NeedsCompaction {
		t.Fatal("no compaction needed at this size")
	}
}

func TestHistoryWindowBoundAndExchangeTrim(t *testing.T) {
	s := openMem(t)
	// Exchange: assistant intent (tool_calls) -> tool result -> assistant final.
	intent := Message{ID: "msg-intent", Role: "assistant", Content: "", ToolCalls: `[{"id":"c1"}]`}
	toolRow := Message{ID: "msg-tool", Role: "tool", Content: "12:00", ToolCallID: "c1", ToolName: "clock.now"}
	var all []Message
	all = append(all, msg("user", "q0"), msg("assistant", "a0"))
	all = append(all, intent, toolRow, msg("assistant", "final0"))
	all = append(all, msg("user", "q1"), msg("assistant", "a1"))
	appendAll(t, s, all...)

	// Bound 4 keeps the last 4 rows: [tool, final0, q1, a1] — front trim
	// drops the orphan tool row (its intent is outside).
	hw, err := s.HistoryWindow("default", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !hw.NeedsCompaction {
		t.Fatal("oversized suffix must signal compaction")
	}
	if len(hw.Rows) != 3 || hw.Rows[0].Role != "assistant" {
		t.Fatalf("exchange-boundary trim: %+v", hw.Rows)
	}
	for _, r := range hw.Rows {
		if r.Role == "tool" {
			t.Fatalf("orphan tool row survived: %+v", hw.Rows)
		}
	}
}

func TestHistoryWindowKeepsCompleteToolGroup(t *testing.T) {
	s := openMem(t)
	intent := Message{ID: "msg-i2", Role: "assistant", Content: "", ToolCalls: `[{"id":"c2"}]`}
	toolRow := Message{ID: "msg-t2", Role: "tool", Content: "echo", ToolCallID: "c2", ToolName: "echo"}
	appendAll(t, s, msg("user", "q"), intent, toolRow, msg("assistant", "done"))
	hw, err := s.HistoryWindow("default", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	// All 4 fit; the window keeps the complete exchange (group head with
	// its tool result directly after).
	if len(hw.Rows) != 4 {
		t.Fatalf("complete group: %+v", hw.Rows)
	}
	if hw.Rows[1].ID == "msg-i2" && hw.Rows[2].ID != "msg-t2" {
		t.Fatalf("intent/result adjacency broken: %+v", hw.Rows)
	}
}

func TestHistoryWindowLargeSuffix(t *testing.T) {
	s := openMem(t)
	var batch []Message
	for i := 0; i < 30; i++ {
		batch = append(batch, msg("user", fmt.Sprintf("m%d", i)))
	}
	appendAll(t, s, batch...)
	hw, err := s.HistoryWindow("default", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !hw.NeedsCompaction || len(hw.Rows) != 10 {
		t.Fatalf("bounded tail: %d needsCompaction=%v", len(hw.Rows), hw.NeedsCompaction)
	}
	if !strings.Contains(hw.Rows[9].Content, "m29") {
		t.Fatalf("tail must keep the newest rows: %+v", hw.Rows)
	}
}

func TestEventsReplayOrdered(t *testing.T) {
	s := openMem(t)
	for i := 0; i < 5; i++ {
		if err := s.AppendEvent(Event{ID: fmt.Sprintf("evt-%d", i), SessionID: "default",
			Kind: "session", Payload: fmt.Sprintf(`{"n":%d}`, i)}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.Events("default", "", 100)
	if err != nil || len(all) != 5 {
		t.Fatalf("events: %+v err %v", all, err)
	}
	for i, e := range all {
		if e.ID != fmt.Sprintf("evt-%d", i) {
			t.Fatalf("order: %+v", all)
		}
	}
	after, err := s.Events("default", "evt-2", 100)
	if err != nil || len(after) != 2 || after[0].ID != "evt-3" {
		t.Fatalf("after cursor: %+v err %v", after, err)
	}
	// Restart keeps events.
	path := s.dbPath(t)
	_ = s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	again, _ := s2.Events("default", "", 100)
	if len(again) != 5 {
		t.Fatalf("durable events lost: %+v", again)
	}
}

// dbPath recovers the file path for reopen tests (test-only helper).
func (s *Store) dbPath(t *testing.T) string {
	t.Helper()
	var path string
	if err := s.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	return path
}
