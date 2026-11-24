package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func openMem(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func msg(role, content string) Message {
	return Message{ID: "msg-" + fmt.Sprint(role, content), Role: role, Content: content}
}

func appendAll(t *testing.T, s *Store, msgs ...Message) []Message {
	t.Helper()
	root, err := s.EnsureSession("default", "research")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		m.SessionID, m.ExecutionID = "default", root
		appended, err := s.Append(m)
		if err != nil {
			t.Fatalf("append %s: %v", m.Role, err)
		}
		out = append(out, appended)
	}
	return out
}

func TestEnsureSessionIdempotentAndConcurrent(t *testing.T) {
	s := openMem(t)
	a, err := s.EnsureSession("default", "research")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureSession("default", "research")
	if err != nil || a != b {
		t.Fatalf("idempotent ensure: %q %q %v", a, b, err)
	}
	// Concurrent ensures all converge.
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.EnsureSession("default", "research")
			if err != nil {
				t.Error(err)
			}
			results <- id
		}()
	}
	wg.Wait()
	close(results)
	for id := range results {
		if id != a {
			t.Fatalf("concurrent ensure diverged: %q vs %q", id, a)
		}
	}
}

func TestAppendReplayOrderAndCursor(t *testing.T) {
	s := openMem(t)
	inserted := appendAll(t, s, msg("user", "hello"), msg("assistant", "hi"), msg("user", "bye"))
	if inserted[0].Seq != 1 || inserted[1].Seq != 2 || inserted[2].Seq != 3 {
		t.Fatalf("seqs: %+v", inserted)
	}
	all, err := s.Replay("default", 0, 100)
	if err != nil || len(all) != 3 {
		t.Fatalf("replay: %+v err %v", all, err)
	}
	if all[0].Content != "hello" || all[2].Content != "bye" {
		t.Fatalf("order: %+v", all)
	}
	tail, err := s.Replay("default", 2, 100)
	if err != nil || len(tail) != 1 || tail[0].Content != "bye" {
		t.Fatalf("cursor: %+v err %v", tail, err)
	}
	empty, err := s.Replay("never", 0, 100)
	if err != nil || len(empty) != 0 {
		t.Fatalf("unused session replay: %+v err %v", empty, err)
	}
	latest, err := s.LatestSeq("default")
	if err != nil || latest != 3 {
		t.Fatalf("latest: %d %v", latest, err)
	}
}

func TestConcurrentAppendsUniqueSeq(t *testing.T) {
	s := openMem(t)
	root, _ := s.EnsureSession("default", "research")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Append(Message{ID: fmt.Sprintf("msg-race-%d", i), SessionID: "default",
				ExecutionID: root, Role: "user", Content: fmt.Sprint(i)})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	rows, err := s.Replay("default", 0, 1000)
	if err != nil || len(rows) != 16 {
		t.Fatalf("want 16 rows, got %d err %v", len(rows), err)
	}
	seen := map[int64]bool{}
	for _, r := range rows {
		if seen[r.Seq] {
			t.Fatalf("duplicate seq %d", r.Seq)
		}
		seen[r.Seq] = true
	}
}

func TestRestartPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	appendAll(t, s1, msg("user", "persisted"))
	_ = s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	rows, err := s2.Replay("default", 0, 10)
	if err != nil || len(rows) != 1 || rows[0].Content != "persisted" {
		t.Fatalf("restart replay: %+v err %v", rows, err)
	}
}

func TestAppendOnlyNoMutationAPI(t *testing.T) {
	// The store exposes no update/delete for messages by design; this
	// test pins the guarantee by asserting the message count survives a
	// failed-append attempt (constraint violation path).
	s := openMem(t)
	root, _ := s.EnsureSession("default", "research")
	first, _ := s.Append(Message{ID: "msg-dup", SessionID: "default", ExecutionID: root, Role: "user", Content: "x"})
	_, err := s.Append(Message{ID: "msg-dup", SessionID: "default", ExecutionID: root, Role: "user", Content: "y"})
	if err == nil {
		t.Fatal("duplicate id must fail")
	}
	rows, _ := s.Replay("default", 0, 10)
	if len(rows) != 1 || rows[0].Seq != first.Seq {
		t.Fatalf("append-only violated: %+v", rows)
	}
}

// TestSessionModelRoundTrip (add-chat-modes 1.1): sessions carry a
// type set at creation (immutable on re-ensure) and a mutable ward
// binding; the list orders by recent activity and exposes both.
func TestSessionModelRoundTrip(t *testing.T) {
	s := openMem(t)
	if _, err := s.EnsureSession("default", "chat"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureSession("research-1", "research"); err != nil {
		t.Fatal(err)
	}
	// Type is creation-only: re-ensuring with a different type is a
	// no-op (the row keeps its original type).
	if _, err := s.EnsureSession("research-1", "chat"); err != nil {
		t.Fatal(err)
	}

	// Ward binding: set, switch, unset.
	if err := s.SetSessionWard("research-1", "stock-analysis"); err != nil {
		t.Fatal(err)
	}
	sess, err := s.Session("research-1")
	if err != nil || sess == nil {
		t.Fatalf("session: %v %v", sess, err)
	}
	if sess.Type != "research" || sess.Ward != "stock-analysis" {
		t.Fatalf("want research/stock-analysis, got %s/%s", sess.Type, sess.Ward)
	}
	if err := s.SetSessionWard("research-1", "india-analysis"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionWard("research-1", ""); err != nil {
		t.Fatal(err)
	}
	sess, _ = s.Session("research-1")
	if sess.Ward != "" {
		t.Fatalf("want unbound, got %q", sess.Ward)
	}

	// Title round-trip.
	if err := s.SetSessionTitle("research-1", "Cloud pricing"); err != nil {
		t.Fatal(err)
	}
	sess, _ = s.Session("research-1")
	if sess.Title != "Cloud pricing" {
		t.Fatalf("title: %q", sess.Title)
	}

	// List: recency order (activity beats creation), type + ward exposed.
	if err := s.SetSessionWard("research-1", "stock-analysis"); err != nil {
		t.Fatal(err)
	}
	root2, err := s.EnsureSession("research-2", "research")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Message{ID: "m-r2", SessionID: "research-2", ExecutionID: root2, Role: "user", Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	infos, err := s.ListSessions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 3 {
		t.Fatalf("want 3 sessions, got %d", len(infos))
	}
	if infos[0].ID != "research-2" || !infos[0].HasMessages {
		t.Fatalf("most-recent-first broken: %+v", infos[0])
	}
	var r1, def *SessionInfo
	for i := range infos {
		switch infos[i].ID {
		case "research-1":
			r1 = &infos[i]
		case "default":
			def = &infos[i]
		}
	}
	if r1 == nil || def == nil {
		t.Fatalf("missing rows: %+v", infos)
	}
	if r1.Type != "research" || r1.Ward != "stock-analysis" || r1.HasMessages {
		t.Fatalf("research-1 row: %+v", r1)
	}
	if def.Type != "chat" || def.Ward != "" {
		t.Fatalf("default row: %+v", def)
	}
}

// TestMigrateSessionType (add-chat-modes 1.1): a toggle-era database
// (sessions.mode) renames to type and gains ward; a type-era database
// is untouched.
func TestMigrateSessionType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY, title TEXT, mode TEXT NOT NULL DEFAULT 'chat', created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, mode, created_at) VALUES ('default', 'chat', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sess, err := s.Session("default")
	if err != nil || sess == nil {
		t.Fatalf("session: %v %v", sess, err)
	}
	if sess.Type != "chat" {
		t.Fatalf("migrated type: %q", sess.Type)
	}
	// Idempotent: reopening a migrated database is a no-op.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	if sess, _ = s2.Session("default"); sess == nil || sess.Type != "chat" {
		t.Fatalf("reopen row: %+v", sess)
	}
}

// TestLatestEventOfKindFractionalOrdering (gate fix): the latest-event
// read orders by insertion, not by RFC3339Nano text — unequal
// fractional widths (".1Z" vs ".11Z") must not misorder the result.
func TestLatestEventOfKindFractionalOrdering(t *testing.T) {
	s := openMem(t)
	if _, err := s.EnsureSession("default", "chat"); err != nil {
		t.Fatal(err)
	}
	// Hand-craft created_at strings whose text order inverts their
	// time order: .1Z is EARLIER than .11Z but sorts AFTER it.
	for _, ts := range []string{"2026-01-01T00:00:00.11Z", "2026-01-01T00:00:00.1Z"} {
		if _, err := s.db.Exec(`INSERT INTO timeline_events (id, session_id, kind, payload, created_at)
			VALUES (?, 'default', 'intent_analysis', '{}', ?)`, "evt-"+ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	ev, err := s.LatestEventOfKind("default", "intent_analysis")
	if err != nil || ev == nil {
		t.Fatalf("latest: %v %v", ev, err)
	}
	if ev.CreatedAt != "2026-01-01T00:00:00.1Z" {
		t.Fatalf("insertion order must win over text order: %q", ev.CreatedAt)
	}
}
