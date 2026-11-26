# Design

## Context

Legacy transcripts carry no agent; the only signal that a row belongs to
a delegation is its position between the `delegate_agent` intent and the
call's result. The turn builder used adjacency (previous row = intent),
which a report row in between breaks.

## Goals / Non-Goals

**Goals** — one turn per run even when children report mid-delegation;
no report ever tagged as the answer; readable task gists. **Non-Goals**
— no backend change.

## Decisions

- **D1** Both turn builders (thread + Work tab) keep a set of open
  delegate call ids: added on intents (ids from the calls, or a counter
  for id-less legacy rows), removed on results. While any is open: a
  user row is child context; a tool-free assistant row is a report (a
  row, not an answer). Delegation groups are derived per turn from the
  same rows, so every pill resolves its result.
- **D2** A task gist strips a leading `Ward: …` preamble sentence and
  clamps to one line; the full text stays a `title`/`<details>`.
- **D3** `fmtTokens`: integer millions/thousands print without `.0`.

## Risks / Trade-offs

- Legacy rows without call ids resolve open delegations by count (the
  oldest open one first) — the same nearest-open assumption as before.
