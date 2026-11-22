# Karta Capability Backlog

> Phased roadmap from the current system toward a full personal-agent
> platform. Each item names the capability, the seams it builds on, and the
> rough change size (in OpenSpec changes). Items become OpenSpec changes when
> picked up; this document is the queue's source of truth. Sequencing below is
> recommended, not mandatory — but Phase 1 unlocks Phase 2's daily-driver
> value.

## Where we are (2026-11)

Done: providers plane (CRUD, keyring secrets, live model catalog) · bounded
goal runs behind the swappable `domain.AgentEngine` seam (with conformance
suite) · skills (registry, marketplace, byte-bound import) · MCPs (CRUD,
prober, paste-import) · hooks/wards · chitta memory + knowledge graph with
distillation and recall floors · Timeline Tree observability over the
contract API · engineering rules with mechanical enforcement.

## Phase 1 — Close the agent loop (next up)

### add-engine-delegation (subagents) — medium

Spawn sub-agents from a goal run: the engine's tool plane gains a delegation
tool that starts a child `RunGoal` with its own agent definition, streams
`domain.EventDelegation` (the event kind already exists), and folds the
child's result back into the parent run. Builds on: the agents registry
(already loads agent definitions), `contracttest` (a delegation scenario
joins the conformance suite), and the Timeline Tree (delegations must be
observable per constitution rule 4).

### add-cron-scheduler — small/medium

A scheduler package that triggers `RunGoal` on cron/timer expressions, with
scheduled runs visible as Timeline sessions and cancellable like any other
run. Turns Karta from "answers when asked" into "acts on its own calendar".
Builds on: the engine seam (nothing new), config tree (schedules live under
`~/.zbot/config/`), and the contract (a schedules CRUD endpoint, plus SSE
for upcoming runs — contract-first as always).

**Phase 1 exit:** a daily-driver local agent — you talk to it, it delegates,
it remembers, and it runs things on a schedule without being asked.

## Phase 2 — Reach the user where they are (the gateway)

### add-channel-telegram, add-channel-whatsapp — one each (large-ish)

Messaging-channel adapters over the daemon contract. This is the defining
capability of the personal-agent category: the agent lives in the apps the
user already uses, not in a dedicated window. Each channel is a pure adapter
implementing the existing `domain.Channel` seam — the channel-agnostic-core
rule means zero core changes. The real work per channel is auth/pairing,
allowlists/DM safety, mention activation in groups, and media passthrough.
Recommended first pair: Telegram (bot API is friendly), then WhatsApp.
Later fast-follows (Signal, Discord, Slack, email) reuse the adapter pattern
and should get cheaper each time.

### harden-mcp-tool-plane — medium

Goal runs already register MCP tools; this change makes them trustworthy
inside autonomous runs: per-tool timeouts, hooks/wards enforcement on every
MCP call, failure taxonomy in the Timeline, and rate limits. Strategic point:
browser automation, web search, and code execution all exist today as MCP
servers — hardening the MCP tool plane imports the whole tool ecosystem
without Karta writing tools. A native exec tool (with sandboxing) remains a
later option if MCP coverage disappoints.

**Phase 2 exit:** Karta answers in Telegram/WhatsApp, delegates, schedules,
and uses the wider MCP tool ecosystem safely.

## Phase 3 — Differentiators and depth

### add-mobile-client — medium

A mobile shell pairing with the daemon over the same contract (the API was
designed multi-client). Phone-side: camera, voice memo, notifications.

### add-media-plane — large

Images/audio/documents in and out through channels and the desktop shell;
transcription and TTS behind provider seams (local ONNX first where viable,
consistent with local-first).

### add-skill-synthesis (self-improving loop) — medium/large

The distiller already turns finished sessions into memory; this extends it
to propose skills from repeated successful patterns (draft SKILL.md into
staging, human-approves via the existing import flow). Keeps the
zero-hardcoded-agents rule intact: skills are proposed and adopted, never
silently self-installed.

### add-remote-presence — medium

Always-on deployment story (single-board/VPS), pairing-based remote access
to the home daemon, and supervision/restart — without moving state off the
user's device (local-first holds: remote clients, local truth).

## What we deliberately do NOT copy from the category

- **Cloud-hosted anything**: state stays local; channels are adapters, not
  data homes.
- **Silent self-modification**: skill synthesis proposes, the user approves.
- **Loop monoliths**: the engine stays swappable behind the conformance
  suite; the category's incumbents largely are their loops.

## Standing advantages to protect

Memory + knowledge graph in one local store with distillation; Timeline Tree
observability of every step; contract-first multi-client API; mechanically
enforced engineering rules. Every backlog item must strengthen, not erode,
these.
