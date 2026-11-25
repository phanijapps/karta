# Proposal

## Why

The intent analyst already produces the full v1 contract — hidden
intents, the solution path, MCP recommendations, the ward's reason and
archetype, the strategy explanation — but the analysis event and the
session-state block copy only the summary fields (`contractPayload`).
The Task Analysis popover therefore shows intent/complexity/approach
and nothing of *why* or *what the analyst expected* (operator report:
"I don't see the full details — hidden intent, procedures, etc."). This
is the G6 family (the analyst's output is thrown away) in its smallest
form; S2 widens the contract itself, this change stops losing what v1
already decides.

## What Changes

- `IntentAnalysisPayload` gains, additively: `hidden_intents[]`,
  `solution_path` (the v1 string; S2 makes it a list),
  `recommended_mcp_tools[]` (sanitized), `ward_archetype`, `ward_reason`,
  `explanation` (the strategy explanation).
- The wrapper copies them onto every analysis event and the Test probe's
  result; session state carries them (it already decodes the payload).
- The Task Analysis popover renders them: hidden intents, approach
  explanation, solution path, recommended agents and MCP tools, the ward
  reason.

## Capabilities

### Modified Capabilities

- `intent-analysis`: the session-state block (and the live event)
  carries the full v1 contract, not only its summary.

## Impact

Contract (additive, `CONTRACT_CHANGE=1`), `internal/domain/event.go`,
`internal/intent/analysis.go`, the chat surface's popover
(`frontend/src/features/chat-stream/Chat.svelte`), tests.
