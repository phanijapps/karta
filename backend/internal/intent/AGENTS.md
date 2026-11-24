# internal/intent

The pre-execution intent middleware for ROOT goal runs (slice 2 of
ward-based research): a deterministic router (pure greetings bypass),
one small-model intent call producing `domain.IntentAnalysis`, and the
engine decorator that injects the Task Analysis context and the
complexity step budget. Chat and child runs are NEVER analyzed.

## The context packet

`packet.go` builds the analyzer's context: the FULL host roster
(registry skills/agents/MCPs + ward vault) with descriptions, ranked
by the semantic recall's per-kind hit order, capped 20/20/20/64. The
user request embeds as escaped advisory data. Validation runs against
the packet's ID sets with CORRECTION-FEEDBACK retries (≤3 attempts,
45s deadline); the last PARSED decision returns on residue (the
wrapper's Sanitize drops the rest). z.ai glm-5.3* analysis runs with
reasoning_effort=low (the ParamCompleter seam).

## Responsibility

- `Router` — conservative bypass for trivial conversational traffic.
- `Analyzer` — renders the user-owned prompt
  (`config/prompts/intent-analysis.md`, template slots
  `{{.Skills}}/{{.Agents}}/{{.MCPs}}/{{.Wards}}`; embedded
  `default_prompt.md` fallback, kept in sync by test), one Completer
  call, strict parse, one retry, then a TYPED non-fatal degradation
  (`AnalysisUnavailable`) — analysis never blocks a run.
- `Wrapper` (engine decorator) — router → analyze → validate against
  the live index (`Sanitize`, drop-and-warn) → compose the run prompt
  (static sections + `## Context` + `## Task Analysis` + `## Skills`)
  → RunGoal with the step budget; events for bypass/analysis/failure.

## Boundaries

- Decorates `domain.AgentEngine` from the OUTSIDE; never imports the
  engine package, server, or client code.
- The model call goes through the `Completer` seam (the daemon's
  llmCaller; fakes in tests) — no provider imports.
- Analysis never executes tools and never browses.

## Key entrypoints

`Router.Bypass`, `(*Analyzer).Analyze`, `Resources`, and the Wrapper
constructor. See the change artifacts:
`openspec/changes/add-intent-analysis/`.
