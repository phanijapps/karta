// Package intent is the pre-execution middleware for ROOT goal runs:
// a deterministic router (trivial goals bypass analysis), one
// small-model intent call producing the typed contract, and the engine
// decorator that injects the Task Analysis context and the
// complexity-derived step budget. Chat and child runs are never
// analyzed. Everything degrades non-fatally: a failed analysis runs
// the goal without one, with an event saying why.
//
// Boundaries: imports domain + registry + sysprompt (peer/lower tiers)
// and a Completer seam for the model call; never the engine internals,
// server, or client code. It decorates domain.AgentEngine from the
// outside.
package intent

import (
	"regexp"
	"strings"
)

// greetingPattern matches conversational openers/closers that need no
// analysis: the answer does not depend on resources, wards, or
// budgets. Deliberately conservative — a real question is never
// bypassed.
var greetingPattern = regexp.MustCompile(`(?i)^(hi|hello|hey|yo|thanks|thank you|thx|ok|okay|great|good|nice|cool|yes|no|bye|goodbye|good (morning|afternoon|evening)|how are you|what'?s up|sup)[\s!.,?]*$`)

// Router decides deterministically (no model call) whether a goal
// needs analysis at all.
type Router struct{}

// Bypass reports whether the goal is trivial conversational traffic
// that should skip the analysis entirely. Empty goals also bypass
// (the engine handles them); the router never inspects anything but
// the goal text.
func (Router) Bypass(goal string) bool {
	g := strings.TrimSpace(goal)
	if g == "" {
		return true
	}
	return greetingPattern.MatchString(g)
}
