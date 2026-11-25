# Proposal

## Why

Slice 1 built the vault; slice 2 made runs smart about resources and
wards (the analysis recommends a ward). Nothing can ENTER a ward yet.
Under the operator's mini-bot model, slice 3 is the heart: **a ward IS
a specialized agent with its own constitution** — hosting a run means
executing AS that ward's agent, under its `AGENTS.md` + layout, inside
its directory. This change delivers ward tools for root agents, the
sticky ward, Active Ward Template injection (children included),
Karta's first filesystem tool plane (ward-scoped file tools), and the
delegation guard — everything the flow skills (slice 4) and the exec
plane plug into.

## What Changes

- **Ward tools for root agents** (engine tool plane, not MCP):
  `ward_use(name)` — enter an existing ward (exact-name, from the
  vault index); `ward_create(name, archetype)` — instantiate a mini
  bot from `.templates/<archetype>/` (copy + rename the canonical
  starter; conf + constitution + starter pages); both set the
  session's STICKY ward and inject the Active Ward section. Root
  only; children get the read-only guard below.
- **Run-as-the-ward**: when a ward is active, the run's identity is
  the ward's agent — `## Active Ward` composes the ward's OWN
  constitution (its `AGENTS.md`) + its layout template with a digest,
  framed as untrusted layout data (the digest detects concurrent
  modification). Children receive the same template.
- **Sticky ward per session**: set once (tool or intent
  recommendation consumption), persists for the session's runs;
  relative paths in ward tools resolve inside it.
- **Ward-scoped file tools** (Karta's first FS tool plane, bounded):
  `ward_read`, `ward_write`, `ward_list` — operate ONLY inside the
  active ward (path traversal rejected typed); roles follow the conf
  (writes land where the layout allows; linter findings explain
  misses).
- **Delegation guard**: the `delegate_agent` contract grows the ward
  guard — children may read the ward and write via declared roles;
  template-dependent STRUCTURAL mutations (creating the ward, arch
  changes) return a bounded request to the root, never silently
  execute.
- **Intent consumption**: the wrapper's `ward_recommendation` becomes
  actionable — on `use_existing`/`create_new` the Task Analysis
  section instructs the root to call the ward tool (analysis stays
  advisory; the MODEL calls the tool).
- OUT of scope: shell/python exec (the uv/wardops operator — recorded
  direction, own follow-up), spec→plan→tasks flow skills (slice 4),
  chitta bridging (slice 5), the research UI surface.

## Capabilities

### New Capabilities
- `ward-hosting`: ward tools, sticky ward, run-as-the-ward
  constitution, Active Ward Template injection, ward-scoped file
  tools, the delegation guard.

### Modified Capabilities
- `engine-delegation`: the delegate contract grows the ward guard.
- `intent-analysis`: ward_recommendation becomes actionable
  (instruction to call the ward tool).

## Impact

`internal/wards` (enter/create/hosting state + template instantiation),
new `internal/wardtools` (the tool plane, engine-tier), `internal/
engine/eino` (tool registration + delegation description), `internal/
intent` (recommendation → instruction), `internal/domain` (ward
hosting types + events), contracts (SSE ward events if cheap), tests
throughout. The constitution gains nothing (wards are already excepted
user data).
