# Proposal

## Why

The backend-code-quality-refactor change introduced the engineering-rules
document (`docs/development/go-rules.md`) and its mechanical enforcement
(`backend/.golangci.yml`, `scripts/backend-gates.sh`, `task gates:backend` /
`budget:backend` in `task verify`) — but the constitution files that the
constitution's own rule says must move with such changes were not updated in
that change. The root `AGENTS.md` Verification section still described the
boundary checks as hand-run greps and did not name the new tasks; the backend
`AGENTS.md` did not reference the rules document or its enforcement chain.

## What Changes

- Root `AGENTS.md` (Verification): backend bullet names `task lint:backend`
  (with its `.golangci.yml` config), `task gates:backend` (mechanical gates
  that replaced the hand-run boundary greps), `task budget:backend`, and links
  `docs/development/go-rules.md` as the rules source; the `task verify` bullet
  describes the full pipeline incl. contract-drift regenerate-and-compare and
  the `CONTRACT_CHANGE=1` declaration semantics.
- `backend/AGENTS.md`: Testing Rule gains the `contracttest` conformance-suite
  requirement (fakes AND real adapter); new "Engineering Rules and Enforcement"
  section — rules doc ↔ enforcement chain ↔ `ctx-ok:` marker convention ↔
  characterization-test discipline for deferred defects.
- No code, contract, or spec behavior changes (docs-only; `skip_specs: true`).
  Landed as commit `46b2e3b`; this change supplies the OpenSpec record.

## Capabilities

### New Capabilities

(none — docs-only, `skip_specs: true`)

### Modified Capabilities

(none)

## Impact

`AGENTS.md`, `backend/AGENTS.md` only. Every future agent reading either
constitution file now finds the rules doc and the enforcement, bidirectionally
with `docs/development/go-rules.md`.
