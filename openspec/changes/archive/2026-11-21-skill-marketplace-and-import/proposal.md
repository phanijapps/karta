# Proposal

## Why

Adding a skill today means typing name/description/body by hand —
nobody authors skills that way. The ecosystem has settled on two real
distribution paths: the open skills.sh directory (1.5M+ tracked
installs, every skill a GitHub `owner/repo/skill-name`) and packaged
skill files (`.skill` / `.zip` archives) for sharing directly. Skills
settings should be a marketplace: searchable, previewable, and
installable — with local file import for a user's own skills.

## What Changes

- **Skills gets a Browse view.** The Settings → Skills section gains a
  segmented **Installed | Browse** control. Browse offers a search box
  querying the open skills.sh index through the daemon, results as
  cards (name, description, `owner/repo`, install counts), and a
  second entry point: paste any GitHub `owner/repo` (or URL) to fetch
  directly.
- **Preview binds what installs**: every preview returns an opaque capability pinning the repo's immutable commit and per-file digests; installs consume the capability — a branch moving after preview cannot change the installed bytes, and there is no install-without-preview. **Preview is mandatory before anything installs** (trust posture):
  a dialog showing the INERTLY rendered SKILL.md (no raw HTML, no
  remote resource loading — third-party markdown is untrusted), the
  bundle's complete file list, license when present, and the pinned
  source — for marketplace results, GitHub pastes, AND imported files
  alike. Nothing enters the config tree unpreviewed; nothing ever
  executes at install time.
- **One safety envelope for all remote content**: GitHub tarballs and
  uploaded archives pass the same bounded extraction (sizes, entries,
  streaming re-enforcement, regular-files-only, shared path
  validator) — GitHub hosting is not a safety boundary.
- **Marketplace install** fetches the repo tarball from GitHub
  (unauthenticated, explicit user action — the same dial-out rule as
  the MCP probe and provider discovery) at the PREVIEWED immutable
  commit, detects skill folders
  (`skills/*/SKILL.md`, `.agents/skills/*/SKILL.md`, root `SKILL.md`,
  single-directory archives), and lands them atomically under
  `~/.zbot/config/skills/<slug>/` through the existing registry write
  paths.
- **File import**: `.skill` and `.zip` archives (both zip containers)
  via a file picker in the dialog — SKILL.md at the archive root or in
  one top-level directory. Server-side staging as opaque bearer
  capabilities with a full lifecycle (synchronous expiry, one-shot
  commits, startup reclamation, bounded concurrency); the daemon
  parses and validates the archive, returns the preview, and installs
  only on explicit commit. Caps and safety: 10 MiB compressed / 50 MiB
  uncompressed / 256 entries (zip-bomb and zip-slip guards), path
  traversal and symlink rejection.
- **Provenance for every non-local skill**: a small
  `.karta/provenance.yaml` sidecar inside the installed skill
  directory (part of the bundle, self-contained) recording origin
  (`marketplace` | `import`), source repo and ref, install date, and a
  content hash — the foundation for v1.5 update checks that warn on
  drift instead of clobbering local edits. Installed skills carry
  origin badges (local / marketplace / imported) in the list.
- **Local-first unchanged**: the daemon dials out only at the user's
  explicit search/preview/install action; everything installed is a
  local file; no accounts, no telemetry.

**Out of scope (v1):** update checks and version pinning (v1.5 —
provenance lands now so it is possible later); private GitHub repos
(tokens); skill enable/disable (deferred until runtime wiring exists,
mirroring MCP when it lands); executing or sandboxing skill scripts
(install never executes; execution gating belongs to future Code
Wards); skill authoring/scaffolding; publishing to marketplaces.

## Capabilities

### New Capabilities

- `skill-marketplace`: Searchable marketplace discovery (skills.sh
  index via the daemon), GitHub-direct fetch, mandatory preview, the
  archive file-import format and its safety rules, provenance
  recording, and install/uninstall behavior.

### Modified Capabilities

- `api-contract`: Adds the marketplace endpoints (search, preview,
  install), the two-step file-import endpoints (preview + commit), and
  provenance fields on skill responses.

- `settings-shell`: The Skills section gains the Installed | Browse
  views, the search flow, the mandatory preview dialog, file import,
  and origin badges.

## Impact

- **Backend:** new `internal/skillhub` (daemon-side index client +
  GitHub fetcher + archive parser, behind seams for fakes — zero
  network in `go test`), `internal/server` handlers, registry install
  path (skills write path gains provenance + multi-file installs).
- **Contract & codegen:** additive endpoints/schemas.
- **Frontend:** Skills section rework (views, search, preview dialog,
  file picker), client methods, Playwright flows (route-mocked,
  including failure paths).
- **Docs:** package guides, product-context, README.
