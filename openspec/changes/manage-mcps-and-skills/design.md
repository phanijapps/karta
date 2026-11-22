# Design

## Context

The registry (`internal/registry`) is the existing read-only loader for
`config/{prompts,agents,skills,hooks,mcps}`: immutable snapshots,
per-file `LoadError`s, fsnotify watch with 150ms debounce and whole-kind
rebuilds, `reloadMu` serializing load→swap. Nothing consumes MCP or
skill entries yet (the tools seam reserves them), Settings renders
fixtures, and the contract has no registry endpoints. Precedent: the
providers change built `internal/providers.Store` because providers had
no loader at all — MCPs and skills already have one, which changes the
write-path decision. The daemon is loopback-enforced (providers change
D6), and the registry's notification model is in-process by rule.

## Goals / Non-Goals

**Goals:**

- MCP and skill CRUD that lands in the registry snapshot before the API
  responds, coexisting with hand edits and the existing watcher.
- A real connection probe so an MCP configuration can be verified (not
  just saved) before any runtime wiring exists.
- Settings sections that are honest live views, including broken
  hand-edited files (load errors).

**Non-Goals:**

- Runtime wiring (MCP tools into agent runs, skill injection) — engine
  change on the existing tools seam.
- Secrets: no keyring interaction; MCP `env` is file content the user
  authored, nothing new is secret-managed.
- Prompts/agents/hooks CRUD; bundle upload/install; enable/disable.

## Decisions

### D1: Write paths live in the registry, not a parallel store

MCP and skill mutations are registry methods (`CreateMCP`, `UpdateMCP`,
`DeleteMCP`, `CreateSkill`, `UpdateSkill`, `DeleteSkill`) executing
under the existing reload mutex: validate → write → rebuild that kind
from disk under the lock → swap snapshot → notify → return. This gives
synchronous publish and watcher coexistence for free: mutations and
watcher reloads serialize on the same mutex, making "external reload
publishes over a completed mutation" structurally impossible (the
property the providers store had to build for itself). Rejected:
separate `internal/mcps` / `internal/skills` store packages — they
would duplicate the validated loaders and drift from them; providers
needed a store only because no loader existed. Prompts/agents/hooks
stay write-free. Two review-hardened rules:

- **Disk truth wins.** After ANY write failure the kind is reloaded
  from disk and THAT is published — the snapshot never diverges from
  disk, and the API returns the typed error (D3 documents the residual
  states this can leave; each is loadable, visible, and retry-safe).
- **Mutations deliver their own diff.** `OnChange` subscribers receive
  the added/changed/removed notifications from the mutation itself;
  the watcher's later self-reload of our write compares kind maps and
  emits nothing when nothing changed (today's path-keyed diff would
  misclassify a post-mutation settle — create seen as "changed",
  delete invisible).

Registry swap-despite-errors semantics are RETAINED for loads (unlike
the providers store's last-good): errors describe the current snapshot
— an entry whose file no longer loads is absent, and the error names
the file that went away. Load errors become snapshot-adjacent state
refreshed on every load (initial and watcher-triggered), exposed via
`Registry.Errors()`.

### D2: MCP files are the CRUD unit; name = file stem; at-least-one transport

`config/mcps/<name>.yaml`, managed-name rule copied from providers
(`^[a-z0-9][a-z0-9-]{0,63}$` minus Windows-reserved), enforced on
CREATE and RENAME targets only — existing files with broader stems or
`.yml` extensions stay loadable AND manageable: mutations address the
loader-provided source `Path` (trusted, assigned from the directory
walk), so editing or deleting `Old-Server.yml` rewrites or removes
that very file, and renaming it normalizes to `<managed-name>.yaml`.
The manifest rule broadens in this change from transport-exclusive to
at-least-one: `loadMCPs` today rejects manifests declaring both
`command` and `url`; henceforth it rejects only manifests declaring
neither, making dual-transport servers — one entry reachable as a
local command or a remote endpoint — first-class (`MCPSpec` needs no
structural change, only the validation line). The API validates with
the same rules, so nothing the API writes can be a file the loader
rejects. Snapshot name checks are necessary but NOT sufficient — the
snapshot never contains invalid files, files inside the watcher's
debounce window, or assets-only directories — so create and rename
INSTALL WITH NO-CLOBBER semantics (exclusive create, like the
providers' file install): an occupied target path fails
`duplicate_name` whatever occupies it. Rename protocol: write the new
file (no-clobber) → reload+publish+notify → remove the old file; if
the removal fails, the published state is disk truth (whatever still
loads is listed) and the retry is idempotent. Creates are capacity-bounded per kind (mirroring the
provider list bound; 409 `mcp_limit`); a hand-edited tree overflowing
the capacity at load reports the lexicographically-last excess files
as load errors — so API lists are always complete within the bound.

### D3: Skill directories; slug = managed-name; SKILL.md is the only managed file

**The loader's `SkillSpec.Path` becomes non-decodable from YAML**
(decode into a frontmatter DTO of `name`/`description` only): today a
hand-authored `SKILL.md` carrying `path:` overwrites the trusted
directory before management ever runs — with `os.RemoveAll` in scope
that is an arbitrary-directory delete primitive. Mutations therefore
NEVER trust authored paths: targets are derived as
`<skills-root>/<slug>`, and regression tests prove a frontmatter `path`
cannot redirect detail, edit, rename, or delete outside the skills
root.

Create: no-clobber directory create, then `SKILL.md` via temp+rename
inside it — an occupied path (assets-only dir, watcher-window dir,
slug collision under a different frontmatter name) fails
`duplicate_name`; a crash between the two steps leaves an assets-only
directory the loader already skips by design — acceptable, documented.
Edit rewrites only `SKILL.md` at its trusted source path (frontmatter +
body, capped by the contract at 256 KiB enforced in BYTES); bundles
are never touched, and any directory — including hand-authored
non-slug ones — is editable in place. Rename protocol with
compensation: (1) rename the directory to the new slug (atomic,
no-clobber), (2) rewrite `SKILL.md` with the new name; if step 2
fails, rename the directory back; if the compensation also fails,
publish disk truth and return the typed error — the residual (old
name inside the new slug dir) is loadable, visible, and the retry is
idempotent. Delete: `os.RemoveAll` — on partial failure, reload the
kind from disk, publish that, and return `io_error`; the documented
residual is a partially removed directory the loader treats as
assets-only or reports as a load error. Deliberately no daemon-side
trash; the UI confirmation names the directory and bundle. Skills are
keyed by frontmatter name in snapshots, so duplicate checks compare
names plus the no-clobber path checks; same capacity policy as MCPs
(409 `skill_limit`).

### D4: Probe behind a `Prober` seam over the official go-sdk

New `internal/mcpclient`: `Prober` interface
`Test(ctx, MCPSpec) (ProbeResult, error)` where `ProbeResult` is either
tools (`[]{name, description}`) or a typed failure reason
(`spawn_failed | connect_failed | timeout | protocol_error`). Default
implementation over `github.com/modelcontextprotocol/go-sdk/mcp`:
`CommandTransport` for stdio (env from the manifest passed to the
child — that is what it is for), `StreamableClientTransport` for HTTP;
connect (initialize) → `ListTools` → `Close`, under a ~10s context
deadline shared across transports — a dual-transport server is probed
concurrently with one result per transport, so worst-case wall time
stays one deadline. Error mapping: exec-not-found → `spawn_failed`; dial errors →
`connect_failed`; deadline → `timeout`; everything the SDK reports as
protocol-level → `protocol_error`. Tool listings are capped (256) with
an explicit `truncated` flag; the preflight records the SDK's
`ListTools` pagination behavior so the cap is honest. A preflight task pins the version
and records transport behavior (like the keyring preflight);
`mark3labs/mcp-go` is the recorded fallback if the official SDK fails
preflight — the seam keeps the swap local. The daemon returns probe
failures as 200 bodies (failure reason + message): a broken server
config is expected data, not a 5xx. Not using eino-ext's MCP component:
the eino import boundary reserves eino for the engine adapter, and a
settings probe is not engine wiring. Handlers take a `Prober`; tests
inject fakes (zero processes, zero network in `go test`).

Preflight record (pinned v1.8.0): `mcp.NewClient` +
`StreamableClientTransport{Endpoint}` / `CommandTransport{Command:
*exec.Cmd}`; `Connect` performs initialize; `session.ListTools`
returns `Tools` + `NextCursor` (pagination — probe follows the cursor
up to the cap); dial failures surface as wrapped errors containing
`connection refused` (→ `connect_failed`), missing stdio binaries as
`*fs.PathError` `fork/exec ... no such file` (→ `spawn_failed`);
`session.Close()` shuts the transport down; `InMemoryTransports`
exist but are unused (the probe tests real loopback HTTP and spawned
processes instead). Graph placement: `mcpclient` imports `registry`
(only the `MCPSpec` type) and stdlib+SDK — it sits beside `tools`,
below `server`, never touching `engine/*`. Fallback (mark3labs/mcp-go)
not needed.

### D5: Contract grows three endpoint groups, additive only, with a full failure matrix

`GET/POST /api/mcps`, `GET`-less `PATCH/DELETE /api/mcps/{name}`,
`POST /api/mcps/{name}/test`; `GET /api/skills`,
`GET/PATCH/DELETE /api/skills/{name}`, `POST /api/skills`. MCP entries
list their declared transports (stdio and/or http, each with command or
url); the test endpoint returns one typed result per declared
transport. List responses carry `load_errors: [{path, kind, message}]`
from `Registry.Errors()` (bounded, first 64) and are complete within
the per-kind capacity — create-time 409 `mcp_limit` / `skill_limit`;
hand-edited overflow becomes load errors, never silent truncation.
Skill detail carries the body. **PATCH is full-replace** (PUT semantics
over PATCH, learning from the providers' omitted/null lesson): the MCP
body is the complete manifest — at-least-one transport enforced
post-replace, so clearing both transports is a 400 — and the skill body
is the complete frontmatter + markdown (a changed name is the rename).
Failure matrix, identical across both groups: 400
`invalid_name`/`invalid_manifest` (create AND edit), 404 unknown, 409
`duplicate_name` (snapshot name OR occupied target path), 409
`mcp_limit`/`skill_limit`, 500 `io_error` for genuine write failures
after validation. No 503 — no keyring in this change. No existing
endpoint changes; the regenerated TS client is the only consumer.

### D6: Settings sections follow the Providers pattern

`Settings.svelte` keeps navigation; MCP and Skills become live section
components like `Providers.svelte`: load from the daemon, add/edit
forms (MCP: stdio command/args/env and/or url; Skills:
name, description, body textarea), delete confirmations (skill
confirmation names the directory and bundled files), test-connection
panel for MCPs showing per-transport tool lists (with truncation
noted) or typed failures, load-error rows,
daemon-unreachable states. Fixture data and Mock badges for these
sections are deleted; the draft store survives only for Logging and
Advanced. Playwright flows are route-mocked, mirroring the provider
specs including failure paths.

### D7: No eventbus changes

Registry notifications stay in-process (registry AGENTS.md rule); the
constitution's eventbus enumeration (agent steps, delegations, tool
calls, hook decisions, compaction) does not cover config CRUD or
settings probes, matching the providers change, whose CRUD and
discovery tests likewise emit no timeline events. The D1 mutation
diffs keep those in-process notifications CORRECT (added/changed/
removed delivered once, never misclassified by the watcher's settle).

### D8: Guard hardening ships with the env-bearing surface

Today's `guardMutations` checks nothing on GET/HEAD and accepts an
Origin whose host equals the request Host — fine when responses
carried no secrets, but this change adds plaintext env reads (MCP
list/detail GETs) and a command-spawning probe endpoint, and a
DNS-rebound Host satisfies that same-host comparison. The guard gains
trusted-Host validation for EVERY request: Host must match the
configured loopback `server.address` (or `localhost` variants),
absent-Host HTTP/1.0-style clients pass, and the existing origin rule
stays for mutations. Legitimate clients are unaffected — the Wails
shell and the Vite dev proxy both reach `127.0.0.1:<port>` with that
Host. Tests cover the hostile matrix (foreign Host on GET reads,
foreign/absent Origin on mutations, rebound-style same-host Origin)
over env listing, CRUD, and probe endpoints. This is why D2/D5's env
reasoning stands: the mitigation is the hardened guard plus loopback
binding, not loopback alone.

## Risks / Trade-offs

- [go-sdk churn or transport gaps] → preflight pins a version before
  any handler exists; the `Prober` seam plus the recorded fallback keep
  the blast radius to `internal/mcpclient`.
- [`os.RemoveAll` destroys bundles] → by-design destructive with a
  bundle-naming UI confirmation, no hidden trash directory — and it
  can never leave the daemon lying: partial failure reloads from disk
  (D1 disk-truth rule) and reports the residual as a load error.
- [Watcher self-events from our own writes] → mutations deliver the
  notification diff themselves and no-op reloads emit nothing (D1);
  serialization under the same mutex prevents clobbering.
- [Snapshot-blind races (invalid files, debounce window, slug
  collisions)] → no-clobber installs make occupied targets fail
  `duplicate_name` regardless of snapshot state (D2/D3), with racing
  external writes tested deterministically.
- [Env values visible on the API] → the guard hardens to trusted-Host
  on every request (D8) on top of loopback binding; values round-trip
  file content the user authored, and the docs recommend referencing
  environment-provided values when tokens are involved.
- [Skill dir/name mismatch in hand-authored trees] → edits keep the
  author's directory; rename normalizes; the loader's first-wins
  duplicate rule is surfaced as a load error rather than silently
  resolved by the API.
- [Compensated rename leaves a documented residual] → if rollback
  itself fails, disk truth is published and the retry is idempotent
  (D3); the residual state is visible, not silent.

## Migration Plan

Additive: no data moves, no schema changes, existing files and
`config.yaml` untouched; existing broader file names and `.yml`
manifests remain loadable and manageable, with managed naming
required only of API-created entries (D2/D3). One behavioral note:
requests carrying a non-loopback Host header are now rejected (D8) —
curl, the CLI, the desktop shell, and the dev proxy are unaffected.
Rollback is reverting the commit — the daemon simply returns to
read-only registry plus fixture Settings.

## Open Questions

None — probe deadline (10s), tool-list cap (256), and skill body cap
(256 KiB) are tunable constants, not architectural unknowns.
