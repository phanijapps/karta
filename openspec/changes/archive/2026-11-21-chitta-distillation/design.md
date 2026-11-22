# Design

## Context

Loop state: agents call chitta tools (engine-tools change); the
conversation store (`internal/store`, append-only SQLite) accumulates
sessions; chitta offers the trusted seam (`Remember` batch atomic in
one engine transaction, `Connect` idempotent, contribution identity =
(source, session, execution, seq, subject, kind, content)) and
ChittaMeta key-value storage. The reviewed distillation prompt
(agentzero lineage) defines a 5-field extraction contract that needs
mapping onto chitta's enums. Review carry-over: `Dispatch` reads
`s.store` outside `System.mu` — the distiller is a concurrent caller,
so the snapshot fix lands here.

## Goals / Non-Goals

**Goals:** sessions → chitta, idempotent, atomic per session,
transcripts never persisted, model configurable live via Settings →
Advanced, one event per seam op, dead-lettering.

**Non-Goals:** briefing middleware (next change), belief synthesis /
contradiction / decay (graph-accuracy), procedures as an executable
surface, episode records in chitta, embedding work.

## Decisions

### D1: Package and boundaries

`backend/distiller/` — plugin-zone composition component: imports
kernel seams (`internal/store` reads, `internal/eventbus`,
`internal/providers` for the LLM call, `internal/config` for paths)
and the chitta public surface (`backend/chitta`). Nothing imports it
except `cmd/zbotd`. This extends the zone rule (plugins may compose
other plugins' public surfaces; kernel stays clean) — backend/AGENTS.md
updated in this change. chitta never imports store; store never
imports chitta; the loop closes here.

### D2: Worker shape

`distiller.New(sys chitta-ish, sessions SessionSource, llm LLMCaller,
settings Settings, log)` → `Run(ctx)` on a ticker (default 5m) —
no session-end event exists yet on the bus, so v1 is ticker-only; the
briefing change may add the event trigger. One session at a time
(serial), each pass: select → extract → map → write → mark.

- **SessionSource** (kernel adapter): list finished sessions with
  messages; supplied by cmd from `internal/store` (Replay).
- **LLMCaller** (kernel adapter): `providers.ChatClient` completion,
  temperature 0, wrapped to retry once on transport error.
- **Settings**: re-read `~/.zbot/config/distillation.json` each pass
  (cheap; no fsnotify needed); falls back to defaults + any
  `distillation-prompt.md` override, else the embedded prompt.

### D3: Selection + markers (in chitta, not zbot.db)

`chitta` seam additions: `MarkDistilled(sessionID string, attempts int,
deadLetter bool)` and `DistillationMarkers(limit)` — ChittaMeta nodes
`distilled:<sessionID> = {"attempts": n, "dead": bool, "at": ts}`. The
checkpoint lives with the writer; zbot.db stays conversations-only.
Selection = sessions from the store MINUS marked ones; dead-letter
after 3 attempts.

### D4: Extraction + mapping (the reviewed prompt, adapted)

Embedded default prompt (config-overridable). Contract changes vs the
reviewed file: observations carry `kind` (chitta's five), `subject`,
`content`, `tags[]` (dot-keys + `epistemic:*`), `confidence`,
`entities[]`; entity types collapse to chitta's seven (location→place,
document/artifact/ward/file→other, role/event/time_period→concept;
durable properties become facts); relationships keep the open verb
vocabulary; episode → learnings/strategy as observations (subject
`user`), task_summary/outcome stay in the pass log (out of chitta);
procedure → `kind=procedure` observation whose content is the
structured JSON; caps (20/20/20) and confidence bands kept; ward
vocabulary generalized. Mapping is pure Go on validated JSON
(DecodeChittaWrite semantics — strict).

### D5: Writes and idempotency

Per session: ONE `Remember` batch (all observations; atomic engine
transaction) with `ChittaHostContext{Source: distiller, SessionID,
ExecutionID}` + `Connect` per relationship (entity upserts by name
resolve through the existing merge). Then `MarkDistilled`. Re-runs:
already-marked sessions skipped; crashed-before-mark re-distills —
contribution identity makes the re-run a no-op. Model swaps: same
idempotency, so Settings changes are migration-free.

### D6: Seam observability + the mutex fix

`Remember`/`Revise`/`Connect` on System emit one `ChittaToolInvoked`
IFF `host.Source == distiller` (the agent/user path emits in Dispatch
— exactly one per op either way). Store snapshot: seam entry points
read `s.store` under `s.mu` once and pass the snapshot down
(carry-over from the engine-tools review).

### D7: Settings + API + frontend

Daemon-managed `~/.zbot/config/distillation.json` `{provider, model,
prompt}` (prompt inline; writing also refreshes the effective prompt —
file override wins only when json omits prompt). Contract:
`GET/PUT /api/distillation` (PUT validates provider against the
configured set, model non-empty; 400 typed errors). Server handler in
`internal/server` (kernel; touches no chitta types — handler works on
the settings file + provider list). Frontend: Settings → Advanced
gains the live Distillation card (provider/model picker from
`/api/models`, prompt textarea, Save → PUT), visually distinct from
the remaining read-only previews; connection state when the daemon is
down (no fixtures).

## Risks / Trade-offs

- [Extraction quality varies by model] — model is a live setting
  precisely so the user can tune it; caps + confidence bands + the
  evidence model absorb noise; dead-lettering bounds cost.
- [Long sessions exceed context] — v1 chunking: transcript truncated
  to the newest N messages (N=200) with a note; full chunked windows
  if quality demands (follow-up).
- [Marker churn in ChittaMeta] — bounded by session count; fine.
- [Serial passes] — one session at a time is correct v1 (LLM cost
  control); parallelism later if needed.

## Migration Plan

None (greenfield markers; no existing data reshaped).

## Open Questions

- Session-end bus event as an additional trigger — decide with the
  briefing change (the ticker keeps latency ≤ 5m meanwhile).
- Whether the prompt override file should win over the settings-JSON
  prompt (v1: file wins only if json omits prompt — recorded, may
  simplify to json-only).
