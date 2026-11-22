# Tasks

## 1. Relocation

- [x] 1.1 Paths.AgentsDir (`~/.zbot/agents`); registry load + watch wired
  there; bootstrap seeds + one-time-migrates legacy `config/agents`.
  Verify: config tests (seed location, migration, user-ownership) + full
  backend suite green.
- [x] 1.2 Docs + specs updated in the same change (root AGENTS.md config
  tree, registry/config package comments). Verify: grep shows no stale
  `config/agents` references outside archives/history.

## 2. Review and hardening

- [x] 2.1 zbot-reviewer read-only over the delta; fix P0/P1. Verify: zero open P0/P1.
- [x] 2.2 `task verify` green. Verify: exits zero.
