# Tasks

## 1. Vault + schema

- [x] 1.1 `internal/wards`: vault resolution (OS-aware Documents/wards
  default, config override, ~ expansion, absolute-path validation),
  Open + Index (scan → name, archetype, conf presence), bootstrap seeds
  (.templates/{generic,research}: ward-conf.yaml + starter pages) —
  user-owned after first write. Verify: table tests (fresh vault seeds,
  re-bootstrap never overwrites, missing-Documents fallback, config
  override wins, relative path rejected typed).
- [x] 1.2 ward-conf.yaml schema: strict decode of the trimmed tree
  (id/kind/match/required/repeat/format/children), typed errors naming
  file+field. Verify: decode table tests incl. unknown-field rejection.
- [x] 1.3 Linter: walk a ward's tree against its conf — missing
  required, unexpected entries, format mismatch → findings with node
  ids; conf-less ward = clean/generic. Verify: lint table tests
  (clean research ward, missing canonical, unexpected dir, format
  mismatch, freeform).

## 2. Contract + handlers

- [x] 2.1 Contract: GET /api/wards (name, archetype, lint summary,
  canonical_title) + GET /api/wards/{name} (layout tree with node ids);
  strict additive schemas; codegen. Verify: task codegen zero drift.
- [x] 2.2 Handlers wired to the wards package (read-only; unknown ward
  404 typed). Verify: handler tests (list incl. lint states, detail
  tree shape, 404).

## 3. Frontend

- [x] 3.1 Wards surface replaces the Code Wards placeholder: sidebar
  row → wards page (cards: name, archetype badge, lint badge, canonical
  title; empty state pointing at the vault path + `wards.vault_root`
  override hint); generated client types. Verify: Playwright flows
  (roster with lint states, empty state) + svelte-check clean.

## 4. Constitution + docs

- [x] 4.1 Root AGENTS.md local-first rule gains the scoped
  vault exception (user-chosen local data, named default
  ~/Documents/wards); go-rules untouched (no rule change); exploration
  doc links this change. Verify: grep — no stale Code Wards placeholder
  references.

## 5. Review and hardening

- [ ] 5.1 zbot-reviewer read-only over the change; fix P0/P1, P2s fix
  or record. Verify: reviewer report zero open P0/P1.
  (KEPT PENDING per operator: machine gates + reviewer passes are
  recorded in findings.md; the final review sign-off is outstanding.)
- [x] 5.2 task verify green. Verify: exits zero.
