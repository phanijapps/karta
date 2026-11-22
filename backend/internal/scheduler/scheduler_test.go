package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/registry"
)

// fakeEngine records started runs; RunGoal returns a channel the test
// closes (immediately, or under the test's control for overlap cases).
type fakeEngine struct {
	mu     sync.Mutex
	starts []domain.GoalRequest
	block  chan struct{} // non-nil → runs stay "in flight" until closed
}

func (f *fakeEngine) Name() string { return "fake" }
func (f *fakeEngine) RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error) {
	f.mu.Lock()
	f.starts = append(f.starts, req)
	block := f.block
	f.mu.Unlock()
	ch := make(chan domain.TimelineEvent)
	go func() {
		if block != nil {
			<-block
		}
		close(ch)
	}()
	return ch, nil
}
func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

func specOf(name, expr, policy string) registry.ScheduleSpec {
	return registry.ScheduleSpec{Name: name, Schedule: expr, Goal: "G-" + name, MissedPolicy: policy}
}

func regWith(specs ...registry.ScheduleSpec) *registry.Registry {
	dir, _ := os.MkdirTemp("", "sched-*")
	for _, s := range specs {
		policy := ""
		if s.MissedPolicy == "catch_up" {
			policy = "\nmissed_policy: catch_up"
		}
		_ = os.WriteFile(filepath.Join(dir, s.Name+".yaml"), []byte(fmt.Sprintf("name: %s\nschedule: %q\ngoal: %s%s\n", s.Name, s.Schedule, s.Goal, policy)), 0o644)
	}
	reg := registry.New()
	reg.LoadAll(registry.Dirs{Schedules: dir})
	return reg
}

// waitFor polls until cond passes (fires are async).
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal("condition never met")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestFireAtCadence(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 22, 7, 0, 0, 0, time.UTC)
	clock := base
	reg := regWith(specOf("digest", "0 7 * * *", "skip"))
	eng := &fakeEngine{}
	s := New(reg, eng, eventbus.New(), fixedID, func() time.Time { return clock }, "")

	s.tick(context.Background(), base) // 07:00:00 → fires (async)
	waitFor(t, func() bool { return eng.count() >= 1 })
	if got := eng.count(); got != 1 {
		t.Fatalf("want 1 fire at cadence, got %d", got)
	}
	if req := eng.starts[0]; req.SessionID != "sched-digest" || req.Goal != "G-digest" {
		t.Fatalf("run request wrong: %+v", req)
	}
	s.tick(context.Background(), base.Add(time.Second)) // 07:00:01 → no
	s.tick(context.Background(), base.Add(time.Minute))
	if got := eng.count(); got != 1 {
		t.Fatalf("off-cadence tick fired: %d", got)
	}
	next, err := s.NextFire("0 7 * * *", base)
	if err != nil || !next.Equal(base.Add(24*time.Hour)) {
		t.Fatalf("next fire wrong: %v %v", next, err)
	}
}

func TestOverlapSkipsAndRecords(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 22, 7, 0, 0, 0, time.UTC)
	clock := base
	reg := regWith(specOf("busy", "0 7 * * *", "skip"))
	eng := &fakeEngine{block: make(chan struct{})}
	s := New(reg, eng, eventbus.New(), fixedID, func() time.Time { return clock }, "")

	s.tick(context.Background(), base)
	waitFor(t, func() bool { return eng.count() >= 1 })
	// Second cadence fire while run 1 is blocked → skipped_overlapping.
	nextDay := base.Add(24 * time.Hour)
	clock = nextDay
	s.tick(context.Background(), nextDay)
	if eng.count() != 1 {
		t.Fatalf("overlap must not double-fire: %d", eng.count())
	}
	o, ok := s.LastOutcome("busy")
	if !ok || o.Status != "skipped_overlapping" {
		t.Fatalf("outcome not recorded: %+v", o)
	}
	close(eng.block)
}

func TestDecisionsEmitToBus(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 22, 7, 0, 0, 0, time.UTC)
	clock := base
	reg := regWith(specOf("emit", "0 7 * * *", "skip"))
	eng := &fakeEngine{}
	bus := eventbus.New()
	sub := bus.Subscribe(context.Background(), "sched-emit")
	s := New(reg, eng, bus, fixedID, func() time.Time { return clock }, "")

	s.tick(context.Background(), base)
	deadline := time.After(2 * time.Second)
	var got string
	for got == "" {
		select {
		case evt, ok := <-sub.Events():
			if !ok {
				t.Fatal("subscription closed")
			}
			if strings.Contains(evt.Text, "schedule emit") {
				got = evt.Text
			}
		case <-deadline:
			t.Fatal("no decision event on the schedule session")
		}
	}
	if !strings.Contains(got, "fired") {
		t.Fatalf("event should record the fire: %q", got)
	}
}

func TestValidExpression(t *testing.T) {
	t.Parallel()
	if err := ValidExpression("0 7 * * *"); err != nil {
		t.Fatal(err)
	}
	if err := ValidExpression("@daily"); err != nil {
		t.Fatal(err)
	}
	if err := ValidExpression("not a cron"); err == nil {
		t.Fatal("invalid expression must be rejected")
	}
}

var idCounter atomic.Int64

func fixedID() string {
	return fmt.Sprintf("evt-sched-%d", idCounter.Add(1))
}

// TestMissedPolicies: a persisted last-fire older than the cadence makes
// wake() fire catch_up once or record skipped_missed honestly.
func TestMissedPolicies(t *testing.T) {
	t.Parallel()
	writeState := func(path string, name string, last time.Time) {
		raw, _ := json.Marshal(fireState{name: last})
		_ = os.WriteFile(path, raw, 0o644)
	}
	base := time.Date(2026, 11, 22, 7, 0, 0, 0, time.UTC)
	now := base.Add(3 * 24 * time.Hour) // three days later: many missed @daily fires

	t.Run("catch_up fires once", func(t *testing.T) {
		reg := regWith(specOf("late", "@daily", "catch_up"))
		eng := &fakeEngine{}
		state := filepath.Join(t.TempDir(), "state.json")
		writeState(state, "late", base)
		s := New(reg, eng, eventbus.New(), fixedID, func() time.Time { return now }, state)
		s.wake(context.Background())
		waitFor(t, func() bool { return eng.count() >= 1 })
		if eng.count() != 1 {
			t.Fatalf("catch_up must fire EXACTLY once, got %d", eng.count())
		}
		if o, _ := s.LastOutcome("late"); o.Status != "catch_up_fired" {
			t.Fatalf("outcome: %+v", o)
		}
	})
	t.Run("skip records skipped_missed", func(t *testing.T) {
		reg := regWith(specOf("gone", "@daily", "skip"))
		eng := &fakeEngine{}
		state := filepath.Join(t.TempDir(), "state.json")
		writeState(state, "gone", base)
		s := New(reg, eng, eventbus.New(), fixedID, func() time.Time { return now }, state)
		s.wake(context.Background())
		if eng.count() != 0 {
			t.Fatalf("skip must not fire, got %d", eng.count())
		}
		if o, ok := s.LastOutcome("gone"); !ok || o.Status != "skipped_missed" {
			t.Fatalf("outcome: %+v ok=%v", o, ok)
		}
	})
	t.Run("first observation never fires", func(t *testing.T) {
		reg := regWith(specOf("fresh", "@daily", "catch_up"))
		eng := &fakeEngine{}
		s := New(reg, eng, eventbus.New(), fixedID, func() time.Time { return now }, filepath.Join(t.TempDir(), "state.json"))
		s.wake(context.Background())
		if eng.count() != 0 {
			t.Fatalf("no recorded last fire → no catch-up fire, got %d", eng.count())
		}
	})
}

// TestStatePersistsAcrossRestart: the fire map survives a new Scheduler.
func TestStatePersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 22, 7, 0, 0, 0, time.UTC)
	reg := regWith(specOf("persist", "0 7 * * *", "catch_up"))
	eng := &fakeEngine{}
	state := filepath.Join(t.TempDir(), "state.json")
	clock := base
	s1 := New(reg, eng, eventbus.New(), fixedID, func() time.Time { return clock }, state)
	s1.tick(context.Background(), base)
	waitFor(t, func() bool { return eng.count() >= 1 })
	// Restart: a NEW scheduler loads the persisted last fire — proven by
	// wake() firing catch_up (it can only know the fire was missed by
	// reading the state file).
	eng2 := &fakeEngine{}
	s2 := New(reg, eng2, eventbus.New(), fixedID, func() time.Time { return base.Add(48 * time.Hour) }, state)
	s2.wake(context.Background())
	waitFor(t, func() bool { return eng2.count() >= 1 })
	if eng2.count() != 1 {
		t.Fatalf("restart must see the missed fire via persisted state, got %d fires", eng2.count())
	}
	if o, _ := s2.LastOutcome("persist"); o.Status != "catch_up_fired" {
		t.Fatalf("outcome: %+v", o)
	}
}
