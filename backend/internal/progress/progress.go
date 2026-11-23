// Package progress is the goal-run progress tracker: scores each tool
// call (diversity, success, repetition) to distinguish productive work
// from stuck loops, drives nudges instead of hard stops, and decides
// when a run deserves auto-extension (the operator's agentzero shape:
// the engine never stops a productive agent — it nudges at soft
// limits, extends when the score says the agent is working, and only
// kills runs that are genuinely stuck).
//
// Boundaries: pure state + scoring (no I/O, no LLM calls); the engine
// adapter feeds it tool outcomes and reads its decisions per step.
package progress

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// Tracker scores one goal run's tool-call stream.
type Tracker struct {
	// Rolling state (last N calls).
	recentCalls    []callKey // exact-repeat detection (last 5)
	toolNameWindow []string  // diversity scoring (last 20 FAILED calls)
	uniqueTools    map[string]bool
	failingCalls   map[callKey]failingCall

	// Score for the current window (resets on extension).
	score int
	// totalIterations counts every step consumed.
	totalIterations int
	windowToolCalls int

	// Nudge state (each fires at most once per window).
	stuckWarned   bool
	turnWarned    bool
	planningSent  bool
	failureNudged map[callKey]bool

	// Extensions granted so far.
	extensions int
}

type callKey struct {
	name string
	hash uint64
}

type failingCall struct {
	count int
	err   string
}

// New builds a fresh tracker.
func New() *Tracker {
	return &Tracker{
		recentCalls:   []callKey{},
		uniqueTools:   map[string]bool{},
		failingCalls:  map[callKey]failingCall{},
		failureNudged: map[callKey]bool{},
	}
}

// hashArgs produces a stable per-call-arguments hash.
func hashArgs(args string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(args))
	return h.Sum64()
}

// Record observes one tool call (name + raw args JSON + optional error).
// The score updates immediately; the engine reads Nudge()/Stuck()/
// ShouldExtend() on the next step boundary.
func (t *Tracker) Record(name, args string, callErr string) {
	t.totalIterations++
	t.windowToolCalls++
	key := callKey{name: name, hash: hashArgs(args)}

	// First-ever use of a tool gets a small bonus.
	if !t.uniqueTools[name] {
		t.uniqueTools[name] = true
		t.score++
	}

	// Successful calls get a bonus; failures clear on success.
	if callErr == "" {
		t.score++
		delete(t.failingCalls, key)
	} else {
		t.failingCalls[key] = failingCall{
			count: t.failingCalls[key].count + 1,
			err:   callErr,
		}
		// Failed calls feed the diversity window.
		t.toolNameWindow = append(t.toolNameWindow, name)
		if len(t.toolNameWindow) > 20 {
			t.toolNameWindow = t.toolNameWindow[1:]
		}
		// Score diversity every 5 FAILED calls (>= 10 in window).
		if len(t.toolNameWindow) >= 10 && len(t.toolNameWindow)%5 == 0 {
			distinct := map[string]bool{}
			for _, n := range t.toolNameWindow {
				distinct[n] = true
			}
			ratio := float64(len(distinct)) / float64(len(t.toolNameWindow))
			if ratio <= 0.15 {
				t.score -= 8 // same tool failing repeatedly — stuck
			} else if ratio <= 0.25 {
				t.score -= 3
			}
		}
	}

	// Exact-repeat detection (same call + args as any of the last 5).
	for _, prev := range t.recentCalls {
		if prev == key && callErr != "" {
			t.score -= 5 // repeated identical failing call
			break
		}
	}
	t.recentCalls = append(t.recentCalls, key)
	if len(t.recentCalls) > 5 {
		t.recentCalls = t.recentCalls[1:]
	}
}

// Tick records one engine step consumed (model call, not tool call).
func (t *Tracker) Tick() { t.totalIterations++ }

// Stuck reports whether the agent is clearly looping: negative score
// after at least 10 tool calls in this window.
func (t *Tracker) Stuck() bool {
	return t.windowToolCalls >= 10 && t.score <= -5
}

// ShouldStop reports whether the run should be killed NOW: stuck AND
// already warned AND the score has kept sinking.
func (t *Tracker) ShouldStop() bool {
	return t.stuckWarned && t.score <= -12
}

// ShouldExtend reports whether a productive agent that hit its budget
// deserves more turns: positive-or-neutral score and not stuck.
func (t *Tracker) ShouldExtend(maxExtensions int) bool {
	return t.extensions < maxExtensions && !t.Stuck() && t.score >= 0
}

// Extend grants one extension window (resets the window state).
func (t *Tracker) Extend() {
	t.extensions++
	t.score = 0
	t.windowToolCalls = 0
	t.stuckWarned = false
	t.turnWarned = false
}

// Extensions reports how many were granted.
func (t *Tracker) Extensions() int { return t.extensions }

// Iterations reports total steps consumed.
func (t *Tracker) Iterations() int { return t.totalIterations }

// Score reports the current window's score.
func (t *Tracker) Score() int { return t.score }

// Nudge returns the advisory message the engine should inject this
// step ("" when none). Each nudge fires at most once per window.
func (t *Tracker) Nudge(turn, softBudget, hardBudget int) string {
	var msg string

	// Stuck warning (once).
	if t.Stuck() && !t.stuckWarned {
		t.stuckWarned = true
		msg = "[SYSTEM: You appear to be repeating similar actions without progress. Step back, re-read the context, and try a different approach. If you cannot make progress, summarize what you've accomplished and what remains.]"
	}

	// Turn budget warning (once, at the soft limit).
	if softBudget > 0 && turn >= softBudget && !t.turnWarned {
		t.turnWarned = true
		if msg == "" {
			msg = fmt.Sprintf("[SYSTEM: You have used %d of %d tool calls. Wrap up your current work and produce your final answer. Do not start new explorations.]", turn, hardBudget)
		}
	}

	// Failing-call nudge (once per distinct failing call).
	for key, fc := range t.failingCalls {
		if fc.count >= 2 && !t.failureNudged[key] {
			t.failureNudged[key] = true
			if msg == "" {
				line := fmt.Sprintf("[SYSTEM: `%s` has failed %d times with these exact arguments. Last error: %s. Retrying the identical call will keep failing. Change the arguments or take a different approach.]", key.name, fc.count, truncate(fc.err, 200))
				msg = line
			}
			break // one failing-call nudge per step
		}
	}

	return msg
}

// PlanningNudge fires once when the agent has made several tool calls
// without any delegation (a proxy for "no plan").
func (t *Tracker) PlanningNudge(delegations int) string {
	if !t.planningSent && t.windowToolCalls >= 5 && delegations == 0 {
		t.planningSent = true
		return "[SYSTEM: You have made several tool calls without delegating or planning. For complex tasks, decompose the work and delegate self-contained steps. This helps you stay focused and avoid repeating work.]"
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = strings.TrimSpace // reserved
