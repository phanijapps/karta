# MCPs and skills — design review record (pre-implementation)

## gpt-6.1-sol design review (zbot-reviewer subagent)

Verdict: **BLOCK** — 4 P1, 5 P2; architectural direction endorsed
(engine isolation, dependency direction, contract-first, FSD, D7's
no-eventbus argument, dual-transport delta coherence, scope discipline
all confirmed correct). All 9 findings folded into the artifacts:

- **F1 (P1) authored `path` redirects destructive skill CRUD** —
  `SkillSpec.Path` is YAML-decodable today, so a hand-authored
  `SKILL.md` could point rename/delete at an arbitrary directory.
  Folded: frontmatter decoded into a DTO, mutations derive targets as
  `<skills-root>/<slug>`, regression scenario + tests
  (design D3, skill-management spec, task 2.2).
- **F2 (P1) snapshot-only duplicate checks race the filesystem** —
  invalid files, watcher debounce windows, and assets-only dirs are
  invisible to snapshots; checked-then-written creates could clobber.
  Folded: no-clobber installs everywhere, occupied path = `duplicate_name`,
  capacity `mcp_limit`/`skill_limit` (design D2/D3, mcp/skill specs,
  api-contract, tasks 2.1/2.2).
- **F3 (P1) rename/partial-delete failure protocol undefined** — two-step
  commits with no rollback story could leave snapshot ≠ disk. Folded:
  disk-truth publishing after any failure, rename compensation with
  documented idempotent retry, partial-delete reconciliation
  (design D1/D2/D3, config-registry delta, tasks 2.1/2.2).
- **F4 (P1) loopback binding alone doesn't justify env/spawn exposure** —
  guard skips GETs and accepts same-host origins (DNS-rebound). Folded:
  trusted-Host validation on every request as part of this change, hostile
  Host/Origin test matrix (design D8, proposal, tasks 4.3/6.2).
- **F5 (P2) "last-good snapshot" ≠ registry semantics** — registry swaps
  despite errors; delta promised last-good. Folded: swap semantics
  retained and stated (errors describe the current snapshot), valid →
  invalid → repaired → deleted test walk (config-registry delta, task 2.3).
- **F6 (P2) watcher self-events misclassify post-mutation notifications** —
  create seen as "changed", delete silent. Folded: mutations deliver
  their own diff; no-op settles emit nothing (design D1, config-registry
  delta, tasks 2.1/2.3).
- **F7 (P2) bounds lacked overflow semantics** — folded: create-time
  capacity 409s, overflow as load errors (never silent truncation),
  tool-list cap with `truncated` flag, byte-based body cap, bounded
  load errors, ListTools pagination in preflight (design D4/D5,
  api-contract, mcp-management, tasks 1.1/1.2/2.1/2.2/3.1).
- **F8 (P2) skill failure-status matrix inconsistent** — folded: PATCH is
  full-replace (PUT semantics) with a complete identical matrix across
  both groups incl. 400-on-edit and 500 `io_error`; clearing both
  transports is a 400 (design D5, api-contract, task 1.1).
- **F9 (P2) legacy stems/`.yml` compatibility unacknowledged** — folded:
  managed names required only of API create/rename; existing entries
  manageable in place via their trusted loaded source path; rename
  normalizes (design D2/D3, mcp-management spec, tasks 2.1/2.2,
  migration note).

Post-fold `openspec validate` green; artifacts re-checked against the
cited code (guard.go GET gap and watcher diff-vs-published-snapshot
verified before folding).

## Manual smoke (task 4.2) — throwaway home

Daemon `/tmp/zbotd` on `ZBOT_HOME=/tmp/zbot-smoke-0pLg`, port 9787
(dev daemon owns 8787). Verified end-to-end over curl:

- MCP create (dual-transport stdio+http) → 201; list shows both
  transports; edit full-replace → 200; rename → 200; duplicate → 409;
  delete → 204.
- Probe: missing bare binary → 200 `{"reason":"spawn_failed"}` (fixed a
  real mapping bug the smoke run caught: `exec.Error` was classified
  protocol_error; now `spawn_failed` with a regression test); a REAL
  stdio server (built echoServer) listed its two tools over the API.
- Skills: create → 201 (body round-trips on detail), rename → 200,
  delete → 204 (directory gone).
- Broken hand-edit (`broken.yaml`, no transport) surfaces as
  `load_errors: ["broken.yaml"]` while valid servers still list.
- Legacy `Old-Server.yml` edited in place — same file rewritten,
  command updated to `legacy-v2`.
- Hostile `Host: rebind.attacker.example` → 403 (guard hardening live).

## Manual e2e (task 6.2) — throwaway home, skills bundle lifecycle

Continuing the task 4.2 smoke daemon (`ZBOT_HOME=/tmp/zbot-smoke-0pLg`):

- Skill create → 201; hand-added bundle (`refs.txt`, `scripts/run.sh`)
  survived a description edit (PATCH keeps the bundle verbatim).
- Rename (`PDF Tools` → `PDF Kit`) moved the directory to
  `config/skills/pdf-kit` with the bundle traveling; detail round-tripped
  the authored body (`# PDF tools`).
- Delete → 204 with the skills directory empty afterwards.
- Screenshots (route-mocked UI states mirroring these verified flows):
  `evidence/e2e-mcp-managed-and-probed.png` (dual-transport server,
  per-transport probe results), `evidence/e2e-skill-managed.png`
  (skill detail with body).

Hostile-Host rejection and the spawn-failure probe transcript are
recorded under task 4.2 above.

## Implementation review (task 7.1) — gpt-6.1-sol

Verdict: **BLOCK — 5 P1, 6 P2**; boundaries (engine isolation, FSD,
dependency graph, D7 no-eventbus), guard hardening, contract sync, and
most spec behavior confirmed correct. All findings fixed (none
dispositioned away):

- **P1 atomic SKILL.md writes** — create and rename now commit via
  temp+rename (`replaceFile`); the original stays loadable until the
  replacement commits. Tests: failed in-place edit leaves the original
  intact; rename failure rolls back with the original loadable.
- **P1 SkillDetail raced the lock** — detail is now a pure read
  returning a typed error; no publishing outside `reloadMu`.
- **P1 MCP rename retry dead-end** — a retry now resumes over its own
  byte-identical target (removal-failure residual) and completes;
  unrelated occupants still conflict. Test hand-crafts the both-valid
  residual and proves resume + the distinct-occupant rejection.
- **P1 lossy args/env editing** — the form edits args one-per-line and
  env as verbatim `KEY=value` lines; a no-op edit round-trip test
  proves whitespace-bearing args, empty args, and trailing-space env
  values survive byte-exactly (multiline env values documented as
  hand-author-only).
- **P1 hand-rolled client bindings** — the MCP/skills client section
  now derives every response/body type from generated `operations` and
  pins every route constant with `satisfies keyof paths`: contract
  drift breaks the build. (UI-oriented wrappers stay above that layer.)
- **P2 directory-rename TOCTOU** — `renameat2(RENAME_NOREPLACE)` on
  Linux (one-syscall no-clobber); guarded `os.Rename` fallback
  elsewhere (documented microsecond residual, loopback daemon).
- **P2 absorbed notifications** — `reloadKindLocked` is the single
  publish path and emits the whole-kind diff: external edits absorbed
  by a mutation's reload and failure residuals notify exactly once
  (test covers the absorbed add); no-diff settles stay silent.
- **P2 skill PATCH body semantics** — body is now REQUIRED (contract +
  regen); PATCH is a true full replace and the UI re-sends the body.
- **P2 runtime bounds** — registry validates URL shape, field lengths,
  arg/env counts, and stdio debris; the handler rejects
  present-but-commandless stdio objects; probe tool metadata is
  byte-clamped to the contract's response bounds.
- **P2 silent operation failures in the UI** — delete io_errors and
  probe request failures (404/daemon) render with reconciliation
  guidance (order fixed: set after reload); tests cover delete-500 and
  probe-404 visibility.
- **P2 coverage claims** — added: real UI rename via PATCH, truncation
  assertion (256 + marker), lossless round-trip, delete-500, probe-404,
  MCP field-validation matrix, removal-failure resume, absorbed-edit
  notification, in-place-edit atomicity. Removed the stray `echoServer`
  binary from the tree.

## Final verification (task 7.2)

After all review fixes: eino boundary clean (0 imports outside the
engine adapter), no internal→clients/cmd imports, codegen ZERO_DRIFT,
contract lint clean, `go vet` OK, `golangci-lint` 0 issues,
`go test -race` 10/10 packages ok, `pnpm check` 0 errors, `pnpm build`
clean, Playwright 42/42. `openspec validate` green.
