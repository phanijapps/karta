# Tasks

## 1. Hosting state + ward tools

- [x] 1.1 `internal/wardtools`: hosting state (per-session active
  ward: name, dir, conf, digest, constitution) + `ward_use` (exact
  index match; typed unknown error listing wards) + `ward_create`
  (template instantiation: atomic copies, canonical rename, existing
  refused, invalid shape/unknown archetype typed; post-create lint
  clean). Verify: table tests over a temp vault (enter known/unknown,
  create happy/conflict/bad-name/bad-archetype, template lint clean).
- [x] 1.2 Sticky ward on the engine run context (set by the tools,
  inherited by children read-only); `ward_entered`/`ward_created`
  Timeline kinds + payloads (contract first, codegen) + events on
  enter/create/switch. Verify: events emitted and persisted (rule 4);
  contract drift zero.

## 2. Active Ward + file tools

- [x] 2.1 `sysprompt.ActiveWard` (constitution + layout + digest +
  untrusted-data framing); wrapper composes it when hosting (last
  section); the delegation path appends the same section to children.
  Verify: wrapper tests (section present on the root AND the child
  body; absent without a ward).
- [x] 2.2 Ward file tools (`ward_read`/`ward_write`/`ward_list`,
  active-ward-only; lexical containment + symlink-directory
  rejection; writes append lint findings as warnings). Verify: table
  tests (happy read/write/list, traversal refused, symlink dir
  refused, write past a repeatable role surfaces lint text, no ward =
  tools absent).
- [x] 2.3 Digest discipline: re-derive on ward-tool calls; mismatch
  warning in the result; conf/AGENTS.md hash. Verify: edit-between-
  calls test warns; matching digest silent.

## 3. Guard + intent + wiring

- [x] 3.1 Delegation guard: delegate description carries the ward
  guard; children's tool inventories exclude ward_use/ward_create.
  Verify: delegation tests (child lacks structural tools; description
  carries the guard).
- [x] 3.2 Task Analysis acts on ward_recommendation (instructional
  line naming the tool call; the middleware never enters). Verify:
  wrapper test asserts the instruction for use_existing + create_new.
- [x] 3.3 zbotd wiring + daemon seam (tools offered when the vault
  exists; kill-switch with the intent plane's? no — wards follow the
  engine; record). Verify: daemon-level smoke via existing seams;
  task verify green.

## 3.5 Chat research surface (operator refinement, 2026-11-24)

- [x] 3.5.1 wardtools.ChatToolSet + WardsTool (read-only index) —
  chat-mountable plane over one sticky host. Verify: wardtools tests.
- [x] 3.5.2 Chat: SetExtraTools/SetActiveWard seams; extra tools mount
  per exchange (Multi); Active Ward composes after the persona.
  Verify: TestChatWardPlane (tools offered; section absent → present).
- [x] 3.5.3 Seeded research-protocol.md (custom section in the default
  prompt; figure-it-out rules: assess → ward decision → discipline).
  Verify: config seed test (8 prompts); user-owned via the persona
  marker.
- [x] 3.5.4 Daemon: chat ward host + tool plane + Active Ward wired
  (engine goal-run wiring unchanged). Verify: build + suite + live
  smoke (below).

## 3.6 Per-ward orchestrator (operator direction, 2026-11-24)

- [x] 3.6.1 `.zbot/config.yaml` provider/model → the ward orchestrates
  its hosted sessions (analysis + root run) via the wrapper's
  OrchestratorModel seam; partial/empty inherits. Ward detail exposes
  `orchestrator`; the context panel shows it. Verify: TestOrchestrator
  Model, TestWrapperOrchestratorOverride; LIVE — india-analysis on
  ollama/glm-5.3-flash:cloud while the picker said zai.

## 4. Review and hardening

- [x] 4.1 Phase gates (gpt-6.1-terra + gpt-6.1-sol) after each task
  group; fix P0/P1, P2s fix or record. Verify: ledger zero open
  P0/P1.
- [x] 4.2 zbot-reviewer over the change + `task verify` green +
  status logged WAITING FOR HUMAN REVIEW. Verify: reviewer zero open
  P0/P1; verify exits zero.
