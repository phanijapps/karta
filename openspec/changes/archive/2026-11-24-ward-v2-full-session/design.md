# Design

## Context

agentzero's proof (conversations.db + the live ward dirs): the full
session = shell (6.8k calls, cwd-pinned, `python3 sources/x.py`) +
web search (brave/ddg MCP) + ward-organized outputs (outputs/ full of
HTML reports and charts) + a persistent ward agent per domain
("ward:financial-analysis" — continuity across sessions). Our engine
already has: sticky ward hosts, delegation, progress budgets, the
session model, intent packets. This change adds the missing planes
and the ward's richer body.

## Ward v2 layout

```
<ward>/
  .zbot/config.yaml   # name, description, last_used, agents, skills
  AGENTS.md           # rules + learnings (v1 role: agent-instructions)
  <ward>.md           # canonical page (v1 role kept)
  spec.md plan.md log.md sources/   # v1 roles kept
  wiki/               # dated knowledge entries (LLM wiki)
  reports/YYYY-MM-DD/ # run outputs organized by date
  apps/               # code: src/, data/, venv notes, README
```

- `.zbot/config.yaml` (user-owned, daemon-read; absent = defaults):
  `name`, `description`, `last_used` (touched by the daemon on ward
  entry — the ONE daemon write to the vault besides ward_create),
  `agents`, `skills` (ward-scoped rosters for future per-ward seeds).
- Lint: the v2 nodes are `required: false` additions to the archetype
  layouts — existing wards lint clean, missing dirs are created by the
  additive migration on daemon boot.
- wiki discipline (AGENTS.md seed): entries are
  `wiki/YYYY-MM-DD-<slug>.md` with a dated header; timebound — the
  canonical page holds the durable summary, wiki holds the timestamped
  detail.

## Warden sessions

- A warden session is a RESEARCH session with id `ward-<name>`,
  created on demand: selecting the ward's bot in the sidebar, sending
  to it, or the intent middleware recommending the ward (first
  ward_use in a shell session MAY fork… no: the binding model stays —
  a shell session that enters a ward keeps its own id; the WARDEN is
  the ward's HOME conversation, always there, like quick chat).
- Store: `EnsureWardenSession(ward)` — id `ward-<name>`, type
  research, ward bound. Sidebar renders wardens under Ward Bots (they
  already group by binding); selecting a ward's row selects the
  warden; the ward card in the context panel stays.
- `.zbot/config.yaml.last_used` updates on ward entry (best-effort).

## Persona seeding

`config/prompts/{soul,os,instructions}.md` seeded verbatim from
~/Documents/zbot/config/agent/{SOUL,OS,INSTRUCTIONS}.md at bootstrap
(write-once). sysprompt composes Role=instructions, Soul=soul,
OS=os — the existing section slots; user-editable, hot-reloaded.

## ward_shell (execution plane)

```
ward_shell(command, timeout_seconds<=300)  — root + children in HOSTED runs only
  cwd    = the active ward's dir (absolute; no -c escapes: cwd pinned)
  bash   -lc <command>, env scrubbed to a safe allowlist + ward path vars
  caps   : 256KB stdout+stderr (truncated, marked), 120s default / 300s max
  events : tool_call event with output (the Timeline shows the run)
  refuse : no active ward → typed error naming ward_use
```

agentzero ran unsandboxed by design (local-first trust); we keep that
posture v1 with cwd-pinning + caps. Implementation: `internal/
wardtools/shell.go` (tool), mounted with the file tools in the hosted
plane (root AND children — children execute inside the ward).

## MCP execution bridge (retrieval plane)

`internal/mcpclient` grows a stdio execution client:
- Daemon boot: for each ENABLED registry MCP with a stdio command →
  spawn (uvx/npx), initialize, tools/list, keep alive (restart on
  call failure, bounded).
- Engine mounting: hosted root runs gain the live MCP tools (names
  prefixed `mcp_<server>_<tool>` to avoid collisions); children
  inherit. Tool defs carry the MCP schema.
- Fail-soft: a server that fails to start degrades to absent + a
  boot log line; probe results keep feeding the resource index.
- ddg-search is the first consumer (keyless, uvx duckduckgo-mcp-server).

## Active Ward packet v2

The packet (constitution + layout) gains a short "Homes" section:
outputs → reports/YYYY-MM-DD/, durable findings → wiki/ dated entry,
code → apps/, and the canonical page remains the summary surface.
The ward AGENTS.md template ships the same discipline in long form.

## Decisions

1. **Unsandboxed shell, cwd-pinned** (agentzero parity; the operator's
   explicit direction). Recorded trust decision; sandboxing is a
   follow-up if ever wanted.
2. **Warden sessions are research sessions** with reserved ids — no
   new session type; the sidebar's existing grouping renders them.
3. **`.zbot/config.yaml` is user-owned**; the daemon touches only
   `last_used` (append-style update, best-effort).
4. **MCP tools mount on hosted runs only** — quick chat stays lean.
5. **wiki is timebound by naming convention** (dated files), not a
   new store; recall covers it via the ward file tools.

## Risks / Trade-offs

- [Shell is a real trust surface] — cwd-pinning + caps + refusal
  without a ward; the constitution's local-first posture accepts it.
- [MCP server lifecycle] — stdio processes need supervision; v1 is
  lazy-spawn-on-boot + restart-on-failure, bounded retries.
- [Vault writes grow] — last_used + run outputs; all additive.

## Migration Plan

Additive: daemon boot ensures the v2 dirs exist in every ward
(write-once, no overwrites); sessions need no migration (wardens are
created on demand).

## Open Questions

- Per-ward model overrides (`.zbot/config.yaml.provider/model`) —
  noted for a follow-up; the daemon default serves v1.
