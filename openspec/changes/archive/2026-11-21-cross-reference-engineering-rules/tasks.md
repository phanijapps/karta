# Tasks

## 1. Documentation cross-references

- [x] 1.1 Update root `AGENTS.md` Verification: gates/budget/lint-config tasks,
  gate-enforced boundaries, verify pipeline + CONTRACT_CHANGE semantics, link
  go-rules.md. Verify: section names all backend verification tasks that exist
  in `Taskfile.yml`.
- [x] 1.2 Update `backend/AGENTS.md`: contracttest conformance-suite testing
  rule + Engineering Rules and Enforcement section referencing go-rules.md and
  its enforcement. Verify: both constitution files link the rules doc
  bidirectionally.

## 2. Review and hardening

- [x] 2.1 Run the read-only `zbot-reviewer` subagent over the docs delta
  (commit 46b2e3b: AGENTS.md + backend/AGENTS.md) and the five constitution
  boundaries; fix every P0/P1 finding, fix P2s or record why not. Verify:
  reviewer report shows zero open P0/P1.
- [x] 2.2 Re-run `task verify` green after the final edits. Verify: `task
  verify` exits zero.
