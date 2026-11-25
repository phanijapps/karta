# Design

## Context

Slice 1 shipped the vault (resolve/bootstrap/index/lint, read-only
API); slice 2 shipped the intent middleware whose contract recommends
wards. The operator's mini-bot model (recorded in the exploration doc
and the backlog): a ward IS a specialized agent ("stock analysis",
"india analysis") — hosting a run means executing AS that ward's
agent, under its own constitution, not merely cd-ing into a
directory.

## Investigation findings

- The engine tool plane: `internal/tools` (Provider seam, Def+
  ParamsJSON+Exec), the adapter offers `tools.Multi(builtin, chitta)`
  + `delegate_agent` (built per-run over the snapshot). Tools are
  plain structs — no eino imports needed outside the adapter.
- Delegation: children run with per-agent tool inventories
  (`FilterTools` allowlist); the delegate description is built per-run
  (`delegateDescription`) — the ward guard rides the description.
- The wrapper composes dynamic sections (`sysprompt.ActiveWard` is
  reserved in the system-prompt spec); GoalRequest.SystemPrompt is the
  daemon-only injection channel; the engine appends dynamics to agent
  bodies.
- Ward vault: `.templates/{generic,research}` seeded write-once;
  `ParseConf`/`Lint` are pure; `Vault.Index/Detail` give the roster;
  canonical node convention (`{ward}.md`, id "canonical").
- agentzero's shape (exploration doc): Active Ward Template packet +
  digest, untrusted-data framing, ward guard wording, sticky cwd — we
  adopt the semantics, not the Rust machinery.
- The operator's hand-moved `~/.zbot/skills/` carries the exec-
  dependent skills; this change does NOT execute scripts (the wardops
  exec plane is a named follow-up — skills remain context).

## Goals / Non-Goals

**Goals:** ward tools (root-only), sticky ward, run-as-the-ward
constitution with digest, ward-scoped file tools bounded by the ward,
the delegation guard, intent recommendations made actionable, events
for every ward action.

**Non-Goals:** shell/python/node execution (wardops Operator —
follow-up), flow skills (slice 4), chitta bridging (slice 5), a ward
UI surface, MCP runtime loading.

## Decisions

1. **`internal/wardtools`** (new, engine tier beside tools): owns the
   four tools + the hosting session state. The HOSTING STATE lives in
   the engine run, not the vault: the vault stays read-only except
   ward_create's one instantiation. State shape: per-session active
   ward (name, dir, conf, layout digest, constitution body) held by
   the adapter's run context; children inherit a read-only view.
2. **ward_create = template instantiation, write-once**: copy
   `.templates/<archetype>/` into `<vault>/<name>/` skipping the
   conf/constitution starters only when absent (never overwrite),
   rename `canonical.md` → `<name>.md`. Reuse `seedAsset`-style
   atomic writes? No — the vault is user data; plain copies with
   O_EXCL create semantics, refusing existing wards. Post-create:
   lint must be clean (typed error if the template itself fails — a
   build-time invariant surfaced at runtime).
3. **Active Ward section** (sysprompt gains `ActiveWard(ward WardView)
   string`): `## Active Ward` = constitution body + layout tree +
   digest + the untrusted-data framing line (the operator's packet
   wording). Injected via the wrapper: when hosting is active the
   wrapper composes it LAST (after Skills). Children: the delegation
   path (adapter) appends the same section to the child's body — the
   child sees structure + roles, never the ward tools.
4. **Digest = content hash** of conf + AGENTS.md (fnv or sha256 over
   the two files; cheap, re-derived on every ward-tool call; mismatch
   → typed warning appended to the tool result, never a hard failure
   — the user owns the ward).
5. **File tools bounded by lexical containment**: resolve
   filepath.Join(wardDir, rel) then verify the cleaned path stays
   under wardDir (symlink escape: Lstat the parents — v1 rejects
   symlinked directories inside wards with a typed error; wards with
   intentional symlinks are out until asked). ward_write lands the
   write and appends lint findings for the touched path as a WARNING
   line (the write stands; the conf speaks through lint).
6. **Delegation guard rides the description**: `delegateDescription`
   gains the guard paragraph (children: read + declared roles only;
   structural actions → bounded request to root). Enforcement is
   structural where cheap: children simply don't receive
   ward_use/ward_create (tool inventory), and ward file tools for
   children allow writes only inside conf-declared repeatable/dir
   roles... v1: children get ward_read/ward_list + ward_write bounded
   the same as root (role-declared write gating is the conf's lint
   voice, not a hard gate — recorded trade-off).
7. **Intent consumption is instructional**: the wrapper's Task
   Analysis gains the ward-tool call line (use_existing →
   ward_use(name); create_new → ward_create(name, archetype)). The
   middleware NEVER enters a ward itself — the model calls the tool,
   events record the transition.
8. **Events**: `ward_entered` / `ward_created` (new TimelineEvent
   kinds, contract-first) with the ward name, archetype, digest; the
   sticky switch emits too. Every tool call already events as
   tool_call; these are the semantic layer.

## Risks / Trade-offs

- [Biggest slice] → split within: tools + hosting first, file tools
  second (tasks grouped so the split can land mid-change if needed).
- [Children writing beyond roles in v1] → lint warns; hard role-
  gating deferred (recorded; the guard's substance is structural
  actions).
- [Symlink rejection] → simple and safe; revisit with the exec plane.
- [Digest churn on benign edits] → warning-only; the model re-reads
  the template via ward tools.

## Migration Plan

Additive; no existing behavior changes (tools appear when wards
exist). Rollback = revert.

## Open Questions

- Persist active ward across DAEMON restarts (session resume)? Leaning
  yes via the session store — decide in tasks (cheap: store on the
  session row; resume re-enters the ward).

## Operator refinement (2026-11-24): the chat IS the research surface

Recorded direction: wards are NOT a goal-run hosting mechanism alone —
a research CHAT figures wards out itself. Decisions:

1. The ward tool plane mounts on CHAT exchanges too (chat
   SetExtraTools): `wards` (read-only index), `ward_use`,
   `ward_create`, and the file tools, over ONE sticky host for the
   chat session. The chat user IS the root (no child split).
2. The Active Ward section composes onto the chat system prompt when a
   ward is entered — the conversation absorbs the ward's constitution
   (no persona swap; the packet rides in via ward_use's result AND the
   prompt section).
3. The figure-it-out rules are PROMPT CONTENT: a seeded
   `research-protocol.md` (custom section in the default system
   prompt; user-owned) — assess trivial vs research; the ward decision
   (index → use_existing exact | create_new reusable-name); ward
   discipline. Shape from the operator's first-turn-protocol +
   INSTRUCTIONS warm/cold paths.
4. The intent middleware STAYS for goal runs (the autonomous sprint
   surface: scheduled, budgeted, resumable). Chat gets no middleware —
   its analysis is the model's own reasoning under the protocol.
5. Delegation-in-chat (warm path → one delegation to a specialist)
   remains slice 4's orchestrator work; recorded, not built here.

The system-prompt capability's "chat static-only" rule is AMENDED by
this change: chat carries `## Active Ward` when hosting (the
add-intent-analysis delta is still unarchived — its spec text will
gain the amendment before sync).
