# Design

## Context

The skills registry (`config/skills/<dir>/SKILL.md` + opaque bundles,
capacity 256, proven write paths) has no distribution story. The
ecosystem settled on: skills.sh (Vercel-run open directory, install
counts, `owner/repo/skill` identifiers) + files on GitHub + the
vercel-labs `skills` CLI, and the agentskills.io SKILL.md standard we
already load. Anthropic's own skills are a plain GitHub repo
(`anthropics/skills`, Apache 2.0). Packaged `.skill`/`.zip` archives
are how individuals share skills directly. Three unarchived changes
now touch settings-shell's requirement; this one archives strictly
last, and its rebase compares the FULL requirement against the
then-current main spec (its delta carries no RENAMED section — the
predecessors own the rename).

## Goals / Non-Goals

**Goals:** searchable marketplace (daemon-proxied), GitHub-direct
install bound to previewed bytes, `.skill`/`.zip` file import, one
mandatory inert preview for all sources, provenance for future
updates, zero execution at install, one shared safety envelope for
all remote/archive content.

**Non-Goals:** update checks/pinning UI (v1.5 — provenance lands
now); private repos (tokens); skill enable/disable (deferred to
runtime wiring); script sandboxing (Code Wards); authoring/publishing;
clients dialing out directly (never).

## Decisions

### D1: Preflight is a HARD GATE, not an adapter swap

Before any contract work, a preflight task pins the skills.sh
endpoint by reading the vercel-labs/skills CLI source, then exercises
it live from the daemon's HTTP client: URL, response schema, timeout
behavior, rate limits, and captured fixtures recorded in this design's
notes. If the endpoint is unusable, work STOPS: the GitHub-search
fallback is validated live and the proposal/spec/API/UI expectations
are updated and re-reviewed before proceeding — no silent
substitution of a different search product. Live probes live outside
unit tests (fixtures only).

### D2: Preview issues a capability; installs consume it (no TOCTOU)

Preview (marketplace card or GitHub paste) resolves the repo's default
branch to an IMMUTABLE commit SHA, fetches the tarball at that SHA,
applies the shared safety envelope (D3), detects skill folders, and
returns: the preview payload PLUS an opaque server-issued capability
binding the canonical repo identity, the commit SHA, the discovered
skill paths, and each skill's per-file digests. The capability is
cached server-side (15 minutes, in-memory, bounded). INSTALL takes
the capability and the chosen skill subset — never arbitrary
source/ref. On cache expiry the daemon re-fetches the PINNED COMMIT
(immutable) and verifies the digests before writing; any mismatch is
a typed changed-upstream failure telling the user to re-preview.
There is no install-without-preview path. The branch moving after
preview cannot change what lands.

### D3: One safety envelope for tarballs and uploads alike

GitHub hosting is not an extraction-safety boundary — repository
content is attacker-controlled exactly like an upload. One
`SafeBundle` policy applies to both: download ≤ 50 MiB with a
deadline; decompressed total ≤ 50 MiB and entries ≤ 512 (enforced
from archive headers AND re-enforced during streaming — headers
lie); per-file ≤ 10 MiB; REGULAR FILES AND DIRECTORIES ONLY —
symlink, hardlink, device, and other special entries reject the
whole archive (tar carries all of these; reject them); every entry
name passes the shared path validator (D4). Nested archives are
opaque bundle FILES, never recursively unpacked. Pasted URLs parse
into a canonical GitHub identity (https, github.com host, `owner/
repo` shape) — never an arbitrary fetch destination.

### D4: One path validator, applied raw and again at install

A single portable validator runs on RAW entry names BEFORE any
normalization: reject absolute paths (leading `/`, Windows drive
letters, UNC), any `..` component, backslash separators (zip/tar
names are `/`-separated; `\` is an evasion), NUL bytes, and the
reserved `.karta/provenance.yaml` path (D6). After normalization,
containment is separator-aware (component-wise, never a string
prefix: `skills-dirx` must not pass a `skills-dir` check), and
normalized-name duplicates (raw `a/b` + `a//b`) and file/directory
collisions (`x` and `x/y`) reject. `InstallSkill` re-validates every
file key with the same validator — hostile direct calls to the
primitive get the same refusals.

### D5: Multipart has exactly one guarded exception

The import-preview endpoint is the ONLY route accepting
`multipart/form-data`, replacing the JSON-only content-type barrier
there with a stricter combination: trusted-Host validation
(unchanged, every request) plus Origin enforcement — browsers send
Origin on cross-origin multipart POSTs, so a hostile site's form
post carries a foreign Origin and rejects; same-host/loopback
Origins and Origin-less CLI/desktop clients pass. The multipart body
itself is bounded (10 MiB pre-parse) and must contain EXACTLY one
file part named `archive` and no other parts. Every other mutation
keeps the JSON-only rule. Guard tests: hostile Origin, rebound Host,
absent-Origin curl, `text/plain` form post (still 415), oversized,
and multi-part bodies.

### D6: Provenance is versioned, reserved, and hash-excluded

`.karta/provenance.yaml` is RESERVED: an incoming bundle containing
that path rejects (no provenance forgery), and unrelated user
authored `.karta/*` files are untouched. Schema (bounded, versioned):
`apiVersion: karta/v1`, `origin` (`marketplace`|`import`), `source`
(`repo`, `ref`, `commit` — marketplace only), `installedAt`,
`contentHash`. The hash covers the CANONICAL PAYLOAD tree excluding
the sidecar itself: sorted relative paths, deterministic
`path\0size\0sha256` tuple encoding, content only (mode bits are a
separate allowed-subset policy, not hashed). Loader semantics: parse
the sidecar narrowly (strict decode); a missing sidecar OR a
malformed one yields origin `local` — the skill still loads;
provenance is descriptive metadata, never authenticity proof. Update
checks (v1.5) recompute and warn on drift.

### D7: `InstallSkill` is validate-then-stage-then-atomic-rename

The registry primitive takes a validated `SkillBundle{Files
[{Path, Mode, Data}], Provenance}`: (1) validate EVERY file path
(D4) and mode (allowed subset: 0o644/0o755 only — executable bits
survive for scripts, special bits never); (2) validate the SKILL.md
with the LOADER's exact rules (frontmatter name+description
required, strict decode with the path sink — accepted frontmatter
fields are exactly `name`, `description`, and the ignored `path`
sink; anything else rejects AT PREVIEW so the registry can never
drop a previewed skill); (3) build the tree in a private sibling
`.stage-<random>/` inside the skills root (0o700); (4) atomically
install via the existing directory no-clobber rename machinery
(renameat2 NOREPLACE) — frontmatter-name duplicates AND occupied
slug paths (including snapshot-invisible occupants) refuse by the
rename; (5) publish, notify, and on any failure remove the stage and
publish disk truth. Multi-skill installs run per-skill independent
(MCP-import semantics: per-skill created/skipped/failed outcomes).

### D8: Staging capabilities have a full lifecycle

Staging IDs are 256-bit crypto-random base64url OPAQUE BEARER
CAPABILITIES: possession authorizes commit (single-user loopback
daemon — the explicit trust model), and IDs are NEVER joined into
filesystem paths; an in-memory map resolves id → staged dir +
expiry + preview payload. Commit: synchronous expiry check, ONE-SHOT
(a replayed id is expired), serialized with installs by the registry
lock, cleanup on success AND failure. Startup reclamation: on boot,
any `~/.zbot/tmp/skill-import/*` directory without a live mapping is
removed (crash leftovers cannot outlive a restart). Aggregate
bounds: ≤ 8 concurrent staged uploads (evict-oldest with a typed
busy error), TTL 15 minutes checked at use, not only by the sweeper.

### D9: Preview renders untrusted markdown inertly

The preview dialog renders third-party SKILL.md with NO raw HTML, NO
automatic remote resource loading (images/frames never fetch),
script/dangerous URL schemes rejected, and external links explicit
(safe schemes only, open-in-new-tab). A minimal server-agnostic
renderer escapes everything and formats a safe subset (headings,
bold, code, lists); links render as text unless http(s). The dialog
labels third-party content as UNTRUSTED — preview and provenance do
not neutralize prompt injection. Hostile-HTML/script-URL/remote-
image fixtures prove inertness. The dialog inherits the predecessor's
modality and async guarantees: focus entry/restoration, Tab
containment, Escape closes only the preview, and delayed responses
bind to the initiating dialog identity (successful installs
reconcile the list even after dismissal).

### D10: Sequencing

Archives strictly after both predecessors; before archiving, compare
this delta's MODIFIED requirement text against the then-current main
spec and rebase (the predecessors own the rename; this change owns
only its additions).

## Risks / Trade-offs

- [skills.sh is community-run] → index-only dependency: installs
  fetch from GitHub; the paste path works with the index down; D1
  gates the endpoint with a stop-and-revalidate fallback.
- [GitHub unauthenticated 60/hr] → typed upstream-rate-limited
  failures with retry guidance; interactive use only.
- [Skills are prompt-injection surfaces] → inert preview (D9),
  provenance + origin badges (D6), no execution anywhere before
  Code Wards.
- [Staging on disk] → 0o700, bearer capabilities, one-shot commits,
  startup reclamation, aggregate bounds (D8).
- [Three unarchived changes, one requirement] → D10 ordering; each
  delta validates independently today.

## Migration Plan

Additive: endpoints, seams, one registry primitive, UI views.
Existing local skills gain origin `local` with zero file changes.
Rollback reverts the commit.

## Open Questions

None blocking — caps (50/10 MiB, 512 entries, 15-min TTL, 8 stages)
are tunable constants; the skills.sh endpoint is pinned by the D1
hard gate with a stop-and-revalidate fallback.

## Preflight record (task 1.1 — gate PASSED)

Pinned from vercel-labs/skills CLI source (`src/find.ts`:
`SEARCH_API_BASE = 'https://skills.sh'`, `searchSkillsAPI`):

- Endpoint: `GET https://skills.sh/api/search?q=<query>[&owner=<owner>]`
- Response (verified live, ~500ms): `{"query", "searchType": "fuzzy",
  "searchVersion": "algolia", "skills": [{"id" (owner/repo/skill
  slug), "source" (owner/repo), "skillId", "name", "installs"}]}`
- Unauthenticated, plain JSON, CORS-open web API; no published rate
  limits observed (handled as typed upstream failures regardless).
- CLI behavior noted: multi-term queries retry per-term and merge
  (exact-first) — we adopt the same exact-first merge for our proxy.
- Fixture captured in `internal/skillhub` tests from this response
  shape; live probes stay out of unit tests.

Fallback (GitHub search) NOT needed; the gate closed in favor of the
pinned endpoint.
