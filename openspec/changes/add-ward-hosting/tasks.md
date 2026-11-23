# Tasks

## 1. Hosting state + ward tools

- [ ] 1.1 `internal/wardtools`: hosting state (per-session active
  ward: name, dir, conf, digest, constitution) + `ward_use` (exact
  index match; typed unknown error listing wards) + `ward_create`
  (template instantiation: atomic copies, canonical rename, existing
  refused, invalid shape/unknown archetype typed; post-create lint
  clean). Verify: table tests over a temp vault (enter known/unknown,
  create happy/conflict/bad-name/bad-archetype, template lint clean).
- [ ] 1.2 Sticky ward on the engine run context (set by the tools,
  inherited by children read-only); `ward_entered`/`ward_created`
  Timeline kinds + payloads (contract first, codegen) + events on
  enter/create/switch. Verify: events emitted and persisted (rule 4);
  contract drift zero.

## 2. Active Ward + file tools

- [ ] 2.1 `sysprompt.ActiveWard` (constitution + layout + digest +
  untrusted-data framing); wrapper composes it when hosting (last
  section); the delegation path appends the same section to children.
  Verify: wrapper tests (section present on the root AND the child
  body; absent without a ward).
- [ ] 2.2 Ward file tools (`ward_read`/`ward_write`/`ward_list`,
  active-ward-only; lexical containment + symlink-directory
  rejection; writes append lint findings as warnings). Verify: table
  tests (happy read/write/list, traversal refused, symlink dir
  refused, write past a repeatable role surfaces lint text, no ward =
  tools absent).
- [ ] 2.3 Digest discipline: re-derive on ward-tool calls; mismatch
  warning in the result; conf/AGENTS.md hash. Verify: edit-between-
  calls test warns; matching digest silent.

## 3. Guard + intent + wiring

- [ ] 3.1 Delegation guard: delegate description carries the ward
  guard; children's tool inventories exclude ward_use/ward_create.
  Verify: delegation tests (child lacks structural tools; description
  carries the guard).
- [ ] 3.2 Task Analysis acts on ward_recommendation (instructional
  line naming the tool call; the middleware never enters). Verify:
  wrapper test asserts the instruction for use_existing + create_new.
- [ ] 3.3 zbotd wiring + daemon seam (tools offered when the vault
  exists; kill-switch with the intent plane's? no — wards follow the
  engine; record). Verify: daemon-level smoke via existing seams;
  task verify green.

## 4. Review and hardening

- [ ] 4.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol) after each task
  group; fix P0/P1, P2s fix or record. Verify: ledger zero open
  P0/P1.
- [ ] 4.2 zbot-reviewer over the change + `task verify` green +
  status logged WAITING FOR HUMAN REVIEW. Verify: reviewer zero open
  P0/P1; verify exits zero.
