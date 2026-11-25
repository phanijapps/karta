# Design

## Context

The Test probe (`POST /api/intent-analysis/test`) returns the
`IntentAnalysisPayload` (full v1 contract since intent-analysis-detail);
the card rendered one summary line.

## Goals / Non-Goals

**Goal** — show the whole decision where the operator runs the probe.
**Non-Goals** — no contract or backend change.

## Decisions

- **D1** Reuse the popover's grouping (What the analyst saw / Solution
  path / Resources / Ward) in a compact list form inside the result;
  a `<details>` holds the raw JSON for debugging prompts.

## Risks / Trade-offs

- None beyond vertical space; the result block is already inside the
  collapsible card.
