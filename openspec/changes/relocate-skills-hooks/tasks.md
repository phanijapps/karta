# Tasks

## 1. Roots + loader

- [x] 1.1 `config.Paths` gains SkillsDir/HooksDir; Subdirs drops
  skills+hooks; ensureDirs; zbotd's LoadAll + Watch Dirs wiring uses
  the new roots. Verify: config tests (tree, self-heal lists) +
  registry loads from a top-level skills dir.
- [x] 1.2 Skill frontmatter sink keys (license/metadata/allowed-tools
  decoded-ignored; everything else still strict). Verify: loader table
  tests — raw operator skill loads clean, unknown key still errors,
  name/description rules unchanged.
- [x] 1.3 One-time hand-move-aware migration (legacy config/{skills,
  hooks} entries rename up when absent at the root; conflicts left
  inert; no-op on the operator's state). Verify: table tests — classic
  install migrates once, hand-moved no-op, conflict never overwrites,
  re-bootstrap idempotent.

## 2. Seeds + docs

- [x] 2.1 Skill seeds target the new root (prompt seeds unchanged;
  markers untouched). Verify: fresh-home bootstrap seeds skills at
  ~/.zbot/skills/; marked homes unchanged.
- [x] 2.2 Constitution rule 1 tree text + config-plane spec sync +
  AGENTS.md notes (config, registry). Verify: grep — no stale
  config/skills references in docs/specs (archive exempt).

## 3. Review and hardening

- [x] 3.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol read-only) after
  group 1; fix P0/P1, P2s fix or record. Verify: ledger zero open
  P0/P1.
- [ ] 3.2 zbot-reviewer over the change + `task verify` green. Verify:
  reviewer zero open P0/P1; verify exits zero.
