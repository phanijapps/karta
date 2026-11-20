package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
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
	root, err := s.EnsureSession("default")
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
	a, err := s.EnsureSession("default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureSession("default")
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
			id, err := s.EnsureSession("default")
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
	root, _ := s.EnsureSession("default")
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
	root, _ := s.EnsureSession("default")
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
