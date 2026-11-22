// Package scheduler fires cron-expressed goal runs: it evaluates the
// registry's schedule definitions on a second tick, starts RunGoal into
// each schedule's persistent session (sched-<name>), skips overlapping
// fires, applies the missed-fire policy on wake, and emits every
// decision to the eventbus (constitution rule 4: no silent work).
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/registry"
)

// Engine is the slice of domain.AgentEngine the scheduler needs.
type Engine interface {
	RunGoal(ctx context.Context, req domain.GoalRequest) (<-chan domain.TimelineEvent, error)
}

// Scheduler runs the cron loop until its context is cancelled.
type Scheduler struct {
	reg   *registry.Registry
	eng   Engine
	bus   *eventbus.Bus
	idGen func() string
	state string // persisted last-fire map (JSON file); "" = in-memory only (tests)

	mu       sync.Mutex
	inFlight map[string]bool // schedule name → run active
	last     map[string]Outcome
	lastFire map[string]time.Time // schedule name → last fire we initiated (wake/missed detection)

	now func() time.Time // fake clock for tests
}

// fireState is the persisted last-fire map (JSON).
type fireState map[string]time.Time

// Outcome records a schedule's most recent decision (list surface).
type Outcome struct {
	At     time.Time
	Status string // fired | skipped_overlapping | skipped_missed | catch_up_fired
}

// New builds the scheduler. now is injectable for tests (nil = real
// clock). stateFile (when non-empty) persists last-fire times across
// restarts so the missed-fire policy can decide honestly.
func New(reg *registry.Registry, eng Engine, bus *eventbus.Bus, idGen func() string, now func() time.Time, stateFile string) *Scheduler {
	if now == nil {
		now = time.Now
	}
	s := &Scheduler{
		reg: reg, eng: eng, bus: bus, idGen: idGen, state: stateFile,
		inFlight: map[string]bool{}, last: map[string]Outcome{}, lastFire: map[string]time.Time{},
		now: now,
	}
	s.loadState()
	return s
}

// loadState restores persisted last-fire times (missing file = first run).
func (s *Scheduler) loadState() {
	if s.state == "" {
		return
	}
	raw, err := os.ReadFile(s.state)
	if err != nil {
		return // first run or unreadable: in-memory defaults hold
	}
	var parsed fireState
	if json.Unmarshal(raw, &parsed) == nil {
		for name, t := range parsed {
			s.lastFire[name] = t
		}
	}
}

// persistState writes the last-fire map atomically.
func (s *Scheduler) persistState() {
	if s.state == "" {
		return
	}
	raw, err := json.Marshal(s.lastFire)
	if err != nil {
		return
	}
	tmp := s.state + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err == nil {
		if err := os.Rename(tmp, s.state); err != nil {
			_ = os.Remove(tmp) // best-effort state persist: in-memory truth holds
		}
	}
}

// Run ticks every second until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	s.wake(ctx) // missed-fire policy applies at start
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.tick(ctx, t.Truncate(time.Second))
		}
	}
}

// wake applies the missed-fire policy honestly: for each schedule with a
// KNOWN last fire whose next cadence slot already passed while the
// daemon was down, catch_up fires once now (catch_up_fired) and skip
// records skipped_missed. Schedules without a recorded last fire (first
// run after adopting the feature) record the present without firing.
func (s *Scheduler) wake(ctx context.Context) {
	snap := s.reg.Snapshot()
	now := s.now()
	for _, spec := range snap.Schedules {
		schedule, err := cron.ParseStandard(spec.Schedule)
		if err != nil {
			continue // invalid hand edits are registry load errors; skip here
		}
		s.mu.Lock()
		last, seen := s.lastFire[spec.Name]
		if !seen {
			s.lastFire[spec.Name] = now
			s.mu.Unlock()
			s.persistState()
			continue
		}
		s.mu.Unlock()
		due := schedule.Next(last)
		if due.After(now) {
			continue // nothing missed
		}
		if spec.MissedPolicy == "catch_up" {
			s.fire(ctx, spec, now, "catch_up_fired")
		} else {
			s.mu.Lock()
			s.last[spec.Name] = Outcome{At: now, Status: "skipped_missed"}
			s.lastFire[spec.Name] = now
			s.mu.Unlock()
			s.persistState()
			s.emit(spec.Name, fmt.Sprintf("schedule %s: missed fire skipped (daemon was down)", spec.Name))
		}
	}
}

// tick fires every schedule whose expression matches this second.
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	snap := s.reg.Snapshot()
	for _, spec := range snap.Schedules {
		schedule, err := cron.ParseStandard(spec.Schedule)
		if err != nil {
			continue
		}
		if !matches(schedule, now) {
			continue
		}
		s.fire(ctx, spec, now, "fired")
	}
}

// fire starts one scheduled run unless one is already in flight.
func (s *Scheduler) fire(ctx context.Context, spec registry.ScheduleSpec, now time.Time, status string) {
	s.mu.Lock()
	if s.inFlight[spec.Name] {
		s.last[spec.Name] = Outcome{At: now, Status: "skipped_overlapping"}
		s.mu.Unlock()
		s.emit(spec.Name, fmt.Sprintf("schedule %s: fire skipped (previous run still active)", spec.Name))
		return
	}
	s.inFlight[spec.Name] = true
	s.last[spec.Name] = Outcome{At: now, Status: status}
	s.lastFire[spec.Name] = now
	s.mu.Unlock()
	s.persistState()

	s.emit(spec.Name, fmt.Sprintf("schedule %s: %s", spec.Name, status))

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inFlight, spec.Name)
			s.mu.Unlock()
		}()
		ch, err := s.eng.RunGoal(context.WithoutCancel(ctx), domain.GoalRequest{
			SessionID: "sched-" + spec.Name,
			Goal:      spec.Goal,
			Agent:     spec.Agent,
			Model:     spec.Model,
		})
		if err != nil {
			s.emit(spec.Name, fmt.Sprintf("schedule %s: run failed to start: %v", spec.Name, err))
			return
		}
		for range ch { // drain: the run's own events stream to its session
		}
	}()
}

// NextFire returns the schedule's next fire after now (list surface).
func (s *Scheduler) NextFire(expr string, after time.Time) (time.Time, error) {
	schedule, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("scheduler: invalid expression %q: %w", expr, err)
	}
	return schedule.Next(after), nil
}

// LastOutcome returns the recorded decision for a schedule name.
func (s *Scheduler) LastOutcome(name string) (Outcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.last[name]
	return o, ok
}

// ValidExpression reports whether the daemon accepts the expression —
// the contract's create/update validation shares it.
func ValidExpression(expr string) error {
	if _, err := cron.ParseStandard(expr); err != nil {
		return fmt.Errorf("scheduler: invalid expression: %w", err)
	}
	return nil
}

// emit records a scheduler decision on the schedule's session timeline.
// ctx-ok: fire-and-forget volatile notification (drop-oldest bus).
func (s *Scheduler) emit(name, text string) {
	if s.bus == nil {
		return
	}
	_ = s.bus.Publish(context.Background(), domain.TimelineEvent{ // ctx-ok: fire-and-forget volatile scheduler decision (drop-oldest bus)
		ID: s.idGen(), SessionID: "sched-" + name, Kind: domain.EventSession, Text: text,
	})
}

// matches reports whether the schedule fires at t (second granularity).
func matches(schedule cron.Schedule, t time.Time) bool {
	next := schedule.Next(t.Add(-time.Second))
	return next.Equal(t)
}

// Describe renders a short human cadence hint for the UI.
func Describe(expr string) string {
	switch strings.TrimSpace(expr) {
	case "@hourly":
		return "Every hour"
	case "@daily", "0 0 * * *":
		return "Every day at midnight"
	case "@weekly":
		return "Every week"
	case "@monthly":
		return "Every month"
	}
	return expr
}
