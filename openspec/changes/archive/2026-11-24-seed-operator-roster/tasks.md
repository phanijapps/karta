# Tasks

## 1. Seed assets + bootstrap

- [x] 1.1 Embed the skill seeds under `internal/config/seedassets/skills/`
  (clean-code, explain-like-socrates, humanizer, book-reader +
  references/, eagle-eye): bodies verbatim from
  `~/Documents/zbot/skills/`, frontmatter normalized to strict keys,
  provenance footer, descriptions within the 512-char bound. Verify:
  table test — every embedded SKILL.md passes `ParseSkillMD` strict and
  every description ≤ 512.
- [x] 1.2 Embed + tune the agent seeds under
  `internal/config/seedassets/agents/` per the design roster table
  (research-agent, reviewer-agent upgraded; planner-agent,
  writing-agent, builder-agent, adversarial-reviewer added; typo fixed;
  model ""/no tools key/max_depth 0). Verify: every seed decodes via
  the agent frontmatter loader with the intended keys.
- [x] 1.3 Replace `seedExampleAgents` with the embed-walking O_EXCL
  seeder (agents + skills; ErrExist = user-owned; absent-only writes).
  Verify: table tests — fresh home seeds 6 agents + 5 skills (+ book
  reader references), re-bootstrap never overwrites a user edit,
  deleted example stays deleted (no resurrection).

## 2. Registry + docs

- [x] 2.1 Registry round-trip: a freshly bootstrapped home loads with
  zero registry load errors; the snapshot lists the seeded skills and
  agents (origin local). Verify: integration test over a temp home.
- [x] 2.2 Docs: `docs/product/backlog.md` gains the seed-operator-roster
  entry (with the deferred-content ledger pointing at slices 3/4);
  `docs/product/product-context.md` notes the seeded example content.
  Verify: docs read; no stale claims.

## 3. Review and hardening

- [x] 3.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol read-only) after
  task group 1 and after group 2; fix P0/P1, fix or record P2s in the
  change's findings ledger. Verify: ledger records zero open P0/P1.
- [ ] 3.2 zbot-reviewer read-only over the whole change; fix P0/P1,
  P2s fix or record. Verify: reviewer report zero open P0/P1.
  (KEPT PENDING per operator: machine gates + reviewer passes are
  recorded in findings.md; the final review sign-off is outstanding.)
- [x] 3.3 `task verify` green (CONTRACT_CHANGE not needed — no contract
  edits). Verify: exits zero.
