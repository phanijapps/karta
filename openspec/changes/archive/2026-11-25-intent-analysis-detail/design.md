# Design

## Context

`contractPayload` (internal/intent/analysis.go) builds the wire payload
from the sanitized `domain.IntentAnalysis` but copies only the summary
fields; the domain contract already holds everything v1 decides.

## Goals / Non-Goals

**Goals** — the full v1 decision rides the event/state/probe and renders
in the popover. **Non-Goals** — contract v2 (plan, constraints,
expected outputs, capabilities, confidence, context items) and the
advisory packet handover stay in S2; the popover stays one scrollable
dialog.

## Decisions

- **D1** Additive payload fields with the contract's own names
  (`hidden_intents`, `solution_path`, `recommended_mcp_tools`,
  `ward_archetype`, `ward_reason`, `explanation`), optional everywhere;
  legacy events without them render as before.
- **D2** `contractPayload` is the ONE place the payload is built
  (completed event, Test probe) — the copy lives there; `Sanitize` runs
  before it, so `recommended_mcp_tools` is the surviving set and
  `dropped` names the rest.
- **D3** The popover groups the new fields under their question: "What
  the analyst saw" (hidden intents, explanation), "Solution path",
  "Resources" (skills, agents, MCP tools — a visible note marks the MCP
  tools advisory until S3 mounts them), and an inline "Ward — <reason>
  (<archetype> archetype)" note. Empty sections are omitted.

## Risks / Trade-offs

- `solution_path` is a v1 string; S2 turns it into `plan[]` and this
  field goes away with the versioned contract — acceptable for one
  slice.
