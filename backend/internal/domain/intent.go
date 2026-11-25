package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// IntentAnalysis is the typed contract one pre-execution intent call
// produces for a ROOT goal run. It classifies the task, bounds the run
// (complexity → step budget), recommends registry resources, picks the
// execution approach, and recommends a ward (advisory until slice 3).
// The analysis never runs tools and never browses; the runtime supplies
// the schema and validates every recommended name against the index.
type IntentAnalysis struct {
	PrimaryIntent      string              `json:"primary_intent"`
	HiddenIntents      []string            `json:"hidden_intents"`
	SolutionPath       string              `json:"solution_path"`
	Complexity         IntentComplexity    `json:"complexity"`
	RecommendedSkills  []string            `json:"recommended_skills"`
	RecommendedAgents  []string            `json:"recommended_agents"`
	RecommendedMCPs    []string            `json:"recommended_mcp_tools"`
	WardRecommendation WardRecommendation  `json:"ward_recommendation"`
	ExecutionStrategy  IntentExecutionPlan `json:"execution_strategy"`
}

// IntentComplexity classifies task size; it maps to the run's step
// budget (the engine's hard cap still applies).
type IntentComplexity string

const (
	// ComplexityS is a small task (4-step budget).
	ComplexityS IntentComplexity = "S"
	// ComplexityM is a medium task (8-step budget).
	ComplexityM IntentComplexity = "M"
	// ComplexityL is a large task (16-step budget).
	ComplexityL IntentComplexity = "L"
	// ComplexityXL is an extra-large task (24-step budget).
	ComplexityXL IntentComplexity = "XL"
)

// StepBudget returns the engine step budget for the complexity
// (S=4, M=8, L=16, XL=24). An unknown complexity is an error — the
// caller degrades to the engine default.
func (c IntentComplexity) StepBudget() (int, error) {
	switch c {
	case ComplexityS:
		return 4, nil
	case ComplexityM:
		return 8, nil
	case ComplexityL:
		return 16, nil
	case ComplexityXL:
		return 24, nil
	}
	return 0, fmt.Errorf("intent: unknown complexity %q", string(c))
}

// WardAction says whether the run should enter an existing ward or
// create a new one.
type WardAction string

const (
	// WardUseExisting enters a ward from the vault index by its exact name.
	WardUseExisting WardAction = "use_existing"
	// WardCreateNew instantiates a ward from an archetype.
	WardCreateNew WardAction = "create_new"
)

// WardRecommendation is the ward part of the contract. The name must
// be a reusable domain category (never task-specific), and
// use_existing must name a ward from the vault index EXACTLY.
type WardRecommendation struct {
	Action    WardAction `json:"action"`
	WardName  string     `json:"ward_name"`
	Archetype string     `json:"archetype,omitempty"`
	Reason    string     `json:"reason"`
}

// ExecutionApproach gates the run shape: simple = root alone with
// provisions; graph = multi-agent / pipeline / spec-plan-artifact work.
type ExecutionApproach string

const (
	// ApproachSimple runs the root alone with its provisions.
	ApproachSimple ExecutionApproach = "simple"
	// ApproachGraph runs multi-agent / pipeline work.
	ApproachGraph ExecutionApproach = "graph"
)

// IntentExecutionPlan is the strategy part of the contract.
type IntentExecutionPlan struct {
	Approach    ExecutionApproach `json:"approach"`
	Explanation string            `json:"explanation"`
}

// wardNamePattern enforces the reusable-domain shape (the semantic
// rule — never a task/ticker name — rides in the prompt; the pattern
// enforces the syntax).
var wardNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// ValidWardName reports whether a ward name has the reusable-domain
// shape.
func ValidWardName(name string) bool { return wardNamePattern.MatchString(name) }

// ParseIntentAnalysis strictly decodes ONE flat JSON contract: no
// envelope, no unknown fields, no trailing values, and the required
// semantic fields present (primary_intent, complexity, approach, ward
// action, ward name). A response that is empty, partial, null, or
// followed by more JSON is a typed error — the caller retries or
// degrades; a malformed response must never influence execution as if
// it were a valid analysis.
func ParseIntentAnalysis(raw string) (IntentAnalysis, error) {
	var a IntentAnalysis
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return IntentAnalysis{}, fmt.Errorf("intent: parse contract: %w", err)
	}
	if dec.More() {
		return IntentAnalysis{}, fmt.Errorf("intent: parse contract: trailing data after the JSON object")
	}
	if err := a.requiredFields(); err != nil {
		return IntentAnalysis{}, err
	}
	return a, nil
}

// requiredFields validates the contract's mandatory semantics.
func (a IntentAnalysis) requiredFields() error {
	if strings.TrimSpace(a.PrimaryIntent) == "" {
		return fmt.Errorf("intent: contract missing primary_intent")
	}
	if _, err := a.Complexity.StepBudget(); err != nil {
		return fmt.Errorf("intent: contract missing/invalid complexity")
	}
	if a.ExecutionStrategy.Approach != ApproachSimple && a.ExecutionStrategy.Approach != ApproachGraph {
		return fmt.Errorf("intent: contract missing/invalid execution_strategy.approach")
	}
	if a.WardRecommendation.Action != WardUseExisting && a.WardRecommendation.Action != WardCreateNew {
		return fmt.Errorf("intent: contract missing/invalid ward_recommendation.action")
	}
	if strings.TrimSpace(a.WardRecommendation.WardName) == "" {
		return fmt.Errorf("intent: contract missing ward_recommendation.ward_name")
	}
	return nil
}

// Contract bounds (the wire payload's declared limits): lists and
// strings the analyst may produce at most this much of. Enforced by
// clamping in Sanitize — an over-long advisory text is trimmed, never a
// reason to fail a decision.
const (
	MaxHiddenIntents    = 12
	MaxHiddenIntentLen  = 512
	MaxSolutionPathLen  = 2048
	MaxExplanationLen   = 2048
	MaxWardReasonLen    = 1024
	MaxWardArchetypeLen = 64
)

// clampList bounds a string list's length and each entry's length.
func clampList(in []string, maxItems, maxLen int) []string {
	if len(in) > maxItems {
		in = in[:maxItems]
	}
	for i, s := range in {
		in[i] = clampLen(s, maxLen)
	}
	return in
}

// clampLen bounds a string's byte length, cutting on a rune boundary so
// a multi-byte character is never split into an invalid sequence.
func clampLen(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen]
}

// Sanitize clamps the contract to values the runtime can act on:
// budgets/approach fall back when invalid; recommended names are
// filtered through the known predicates (a name the predicate does not
// know is DROPPED, not guessed); use_existing must match a known ward
// exactly or degrades to create_new; ward names must be valid shapes;
// the advisory texts are clamped to the contract's declared bounds.
// It returns the dropped names so the caller can warn.
func (a IntentAnalysis) Sanitize(knownSkill, knownAgent, knownMCP, knownWard func(string) bool) (IntentAnalysis, []string) {
	dropped := []string{}
	keepNames := func(in []string, known func(string) bool) []string {
		out := make([]string, 0, len(in))
		for _, n := range in {
			if known != nil && known(n) {
				out = append(out, n)
			} else {
				dropped = append(dropped, n)
			}
		}
		return out
	}
	a.RecommendedSkills = keepNames(a.RecommendedSkills, knownSkill)
	a.RecommendedAgents = keepNames(a.RecommendedAgents, knownAgent)
	a.RecommendedMCPs = keepNames(a.RecommendedMCPs, knownMCP)
	switch a.WardRecommendation.Action {
	case WardUseExisting:
		if knownWard == nil || !knownWard(a.WardRecommendation.WardName) {
			// Never invent a near-duplicate: degrade to create_new and
			// REPORT the rejected recommendation.
			a.WardRecommendation.Action = WardCreateNew
			dropped = append(dropped, "ward(use_existing not in index):"+a.WardRecommendation.WardName)
		}
	case WardCreateNew:
		// valid below
	default:
		a.WardRecommendation.Action = WardCreateNew
	}
	if !ValidWardName(a.WardRecommendation.WardName) {
		if a.WardRecommendation.WardName != "" {
			dropped = append(dropped, "ward:"+a.WardRecommendation.WardName)
		}
		a.WardRecommendation = WardRecommendation{Action: WardCreateNew, Reason: a.WardRecommendation.Reason}
	}
	if a.ExecutionStrategy.Approach != ApproachSimple && a.ExecutionStrategy.Approach != ApproachGraph {
		a.ExecutionStrategy.Approach = ApproachSimple
	}
	if _, err := a.Complexity.StepBudget(); err != nil {
		if a.Complexity != "" {
			dropped = append(dropped, "complexity:"+string(a.Complexity))
		}
		a.Complexity = ComplexityM
	}
	a.HiddenIntents = clampList(a.HiddenIntents, MaxHiddenIntents, MaxHiddenIntentLen)
	a.SolutionPath = clampLen(a.SolutionPath, MaxSolutionPathLen)
	a.ExecutionStrategy.Explanation = clampLen(a.ExecutionStrategy.Explanation, MaxExplanationLen)
	a.WardRecommendation.Reason = clampLen(a.WardRecommendation.Reason, MaxWardReasonLen)
	a.WardRecommendation.Archetype = clampLen(a.WardRecommendation.Archetype, MaxWardArchetypeLen)
	return a, dropped
}
