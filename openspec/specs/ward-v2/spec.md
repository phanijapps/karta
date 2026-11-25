# ward-v2 Specification

## Purpose

Ward v2: wards run as full sessions — the v2 layout (.zbot identity,
wiki/, reports/, apps/), warden sessions (every ward's persistent
selectable agent), the ward-scoped execution plane (shell), and the
retrieval plane (live MCP tools on engine runs).

## Requirements

### Requirement: The ward v2 layout

Every ward SHALL carry the v2 body alongside the v1 roles:
`.zbot/config.yaml` (name, description, last_used, agents, skills —
user-owned; the daemon touches only last_used, on ward entry),
`wiki/` (dated knowledge entries — `wiki/YYYY-MM-DD-<slug>.md`),
`reports/` (run outputs organized by run date
`reports/YYYY-MM-DD/`), and `apps/` (the ward's code, organized).
Existing wards migrate additively (directories created write-once;
nothing moved or deleted); the v1 roles (canonical, spec/plan, log,
sources) keep their meaning.

#### Scenario: A new ward instantiates the full template

- **WHEN** ward_create instantiates a ward from an archetype
- **THEN** the v2 directories exist with starter files and the ward
  lints clean against the v2 layout

#### Scenario: A legacy ward migrates on boot

- **WHEN** the daemon opens a vault whose wards predate v2
- **THEN** the missing directories are created (write-once) and the
  ward's existing files are untouched

#### Scenario: last_used tracks ward entries

- **WHEN** a run enters a ward
- **THEN** the ward's `.zbot/config.yaml` last_used updates
  (best-effort) and the read surface exposes it

### Requirement: Warden sessions — every ward has a persistent selectable agent

Each ward SHALL have a warden session: a persistent research session
(id `ward-<name>`, ward-bound), created on demand and idempotent. The
sidebar's ward row selects the warden; sends route through the full
research pipeline in that session. Wardens appear once per ward.

#### Scenario: Selecting a ward opens its latest session

- **WHEN** the user selects a ward's unique row in the sidebar's Ward
  Bots group
- **THEN** the ward expands to show its sessions and its LAST
  EXECUTED session activates with its transcript and the ward's
  context pre-loaded; a ward with no sessions ensures its warden and
  activates that

#### Scenario: The warden survives restarts

- **WHEN** the daemon restarts
- **THEN** the warden session persists in the database and re-selects
  with its transcript intact

#### Scenario: A session can leave its ward

- **WHEN** the user leaves a research session's ward
- **THEN** the binding clears (the session re-groups under Research),
  the engine host drops the ward so the next run starts unhosted, the
  transcript is untouched, and the quick chat refuses (409)
### Requirement: The execution plane

Hosted runs (root and children) SHALL offer `ward_shell`: a bash
execution tool pinned to the ACTIVE ward's directory — env scrubbed
to a safe allowlist, 120s default timeout (300s max), combined
output capped at 256KB (truncated with a marker). Without an active
ward the tool refuses with a typed error naming ward_use. Every call
emits a tool_call event.

#### Scenario: A hosted run executes its staged script

- **WHEN** a run in a ward calls ward_shell("python3 apps/x.py")
- **THEN** the command executes with the ward as cwd, the output
  (capped) returns to the model, and the Timeline carries the call

#### Scenario: The shell stays inside the ward

- **WHEN** a ward_shell command runs
- **THEN** its working directory is the ward root for the whole
  command (cd/mkdir operate relative to it) and no wardless run can
  invoke the tool

### Requirement: The retrieval plane (live MCP tools on hosted runs)

The daemon SHALL execute configured stdio MCP servers and mount their
tools on root engine runs, prefixed `mcp_<server>_<tool>`. Every call
SHALL be bounded (typed `timeout` past the bound) and every failure
SHALL carry a typed kind (timeout / unavailable / protocol /
server_error / bad_arguments) the Timeline surfaces. Server commands
SHALL resolve as written first, then against the user's common tool
directories (the daemon's PATH is thinner than a shell's). A server
that fails to start SHALL retry on a bounded backoff (not on every
tool-plane assembly). Quick chat mounts no MCP tools.

#### Scenario: A shell-style command resolves

- **WHEN** a config names a bare binary (`npx`, `uvx`) that exists in
  the user's tool directories but not on the daemon's PATH
- **THEN** the server starts (no absolute path required)

#### Scenario: A crash-looping server backs off

- **WHEN** a server fails to start repeatedly
- **THEN** retries space out (2s doubling to 2min) instead of firing
  on every model round

#### Scenario: A timed-out call is typed

- **WHEN** an MCP tool exceeds the per-call bound
- **THEN** the result carries kind `timeout` naming the server and
  tool, and the Timeline shows why

#### Scenario: A hosted run searches the web

- **WHEN** a run in a ward calls the ddg-search server's search tool
- **THEN** the query executes against the live MCP server and the
  results return to the model as the tool's output

#### Scenario: A dead server degrades, not fails

- **WHEN** an MCP server process cannot start (or dies mid-run)
- **THEN** its tools are absent from subsequent runs, the run
  proceeds, and the daemon logs the failure with restart attempts
