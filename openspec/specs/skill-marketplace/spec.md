# skill-marketplace Specification

## Purpose

Searchable, previewable, safe installation of skills from the open marketplace (skills.sh index + GitHub) and from local `.skill`/`.zip` archive files, with provenance recorded for future updates — the daemon dials out only at explicit user action, and nothing executes at install time.

## Requirements

### Requirement: Marketplace search

The daemon SHALL offer a search over the open skills.sh index at the
user's explicit action: a query returns results with each skill's
name, description, `owner/repo` source, and install count when the
index provides it. Search SHALL be proxied by the daemon (clients
never dial the index directly), short-lived-cached (five minutes), and
fail typed (index unreachable) rather than degrading to fabricated
results. A GitHub `owner/repo` (or repository URL) SHALL be accepted
by the preview flow directly, bypassing search.

#### Scenario: Search and see results

- **WHEN** the user searches the marketplace for "pdf"
- **THEN** matching skills are listed with name, description, source
  repo, and install counts from the index

#### Scenario: Index unreachable is typed

- **WHEN** the skills.sh index cannot be reached
- **THEN** the search returns a typed index-unreachable failure with
  retry guidance and never fabricated or stale-listed results

#### Scenario: Stale index entries meet reality

- **WHEN** a search result points at a repository whose skill no
  longer exists
- **THEN** the preview fails typed (not found / no skills) rather
  than installing something else

#### Scenario: Cache freshness is defined

- **WHEN** a result cache is fresh it serves; when expired the daemon
  re-queries synchronously
- **THEN** there is no background refresh and no stale success

### Requirement: Mandatory preview

No skill SHALL install without an explicit preview the user saw first:
for every source (marketplace result, GitHub paste, imported file) the
daemon SHALL return the parsed SKILL.md (name, description, full
body), the complete bundle file list with sizes, the license when
present, and the resolved source and ref. Preview SHALL be
server-parsed (the daemon is the authority on what will land on disk)
and SHALL NOT execute anything.

#### Scenario: Preview before install

- **WHEN** the user selects a marketplace result or submits an archive
- **THEN** the preview dialog shows the SKILL.md content and every
  bundled file before any install action is offered

### Requirement: Preview capability binds the reviewed bytes

A preview SHALL resolve the repository's default branch to an
immutable commit and return, beside the preview payload, an opaque
server-issued capability binding the canonical repository identity,
that commit, the discovered skill paths, and each skill's per-file
digests. Installing SHALL consume the capability (with the chosen
skill subset) — never arbitrary source/ref inputs — and there SHALL
be no install path without a prior preview. If the capability's cache
expired, the daemon re-fetches the PINNED COMMIT and verifies the
digests before writing; any mismatch is a typed changed-upstream
failure directing the user to re-preview. A branch moving between
preview and install cannot change the installed bytes.

#### Scenario: Branch moves after preview

- **WHEN** a repository's default branch advances after a preview and
  before the install
- **THEN** the install fetches the preview's pinned commit and the
  installed files match the previewed digests exactly

### Requirement: One safety envelope for all remote content

Repository tarballs and uploaded archives SHALL pass the SAME safety
envelope: bounded download (≤50 MiB, deadline), bounded decompression
(≤50 MiB total, ≤512 entries — enforced from archive headers and
re-enforced while streaming), per-file ≤10 MiB, regular files and
directories only (symlink, hardlink, device, and special entries
reject the whole archive), and the shared path validator on every
entry name (raw): absolute paths (including Windows drive and UNC
forms), any `..` component, backslash separators, and NUL reject;
after normalization, sibling-prefix escapes (`dirx` vs `dir`),
normalized-name duplicates, and file/directory collisions reject.
Nested archives are opaque bundle files, never recursively unpacked.
Pasted URLs SHALL parse into a canonical GitHub `owner/repo` identity,
never an arbitrary fetch destination.

#### Scenario: A hostile repository tarball rejects

- **WHEN** a fetched tarball contains a symlink entry or a `../`
  escape
- **THEN** the preview rejects with the specific violation and
  nothing is staged or installed

#### Scenario: Lying headers reject during streaming

- **WHEN** archive headers understate sizes beyond a cap
- **THEN** streaming enforcement rejects the archive mid-read

### Requirement: Marketplace install

Installing SHALL land each selected skill atomically under
`config/skills/<slug>/` by the registry's rules (slug from the skill
name, capacity, duplicate refusal leaving the existing skill
untouched) through a validate-stage-rename primitive: every file path
and mode (0o644/0o755 only) validated, the SKILL.md validated with
the LOADER's exact rules (accepted frontmatter: `name`,
`description`, and the ignored `path` sink — nothing else), the tree
built in a private staging sibling, and installed by the atomic
directory no-clobber rename. Bundled files copy verbatim (executable
bits preserved for scripts); multi-skill repositories install a
chosen subset with per-skill outcomes. Failures are typed
(unreachable, not found, no skills found, invalid skill, upstream
rate-limited, changed-upstream).

#### Scenario: Install a marketplace skill

- **WHEN** the user previews and installs a skill from a public repo
- **THEN** it appears in the installed list (marketplace-badged) with
  its bundle intact and the connection to agents requiring no restart

#### Scenario: Duplicate refuses without clobbering

- **WHEN** a skill with the same name — or a distinct name whose slug
  collides with an installed or snapshot-invisible occupant — is
  installed
- **THEN** the install refuses and every existing file is untouched

#### Scenario: Multi-skill repositories

- **WHEN** a repository contains several skills
- **THEN** the preview lists them and the user installs a chosen
  subset with one outcome per skill

#### Scenario: An executable script survives

- **WHEN** an installed bundle contains an executable script
- **THEN** its executable bit survives preview, staging, and install
  without ever being invoked

#### Scenario: Registry-invalid skills reject at preview

- **WHEN** a skill's frontmatter carries fields the loader's strict
  decode rejects
- **THEN** the preview refuses it — the registry can never
  subsequently drop a previewed skill

### Requirement: Staging lifecycle

File-import staging SHALL use opaque high-entropy bearer capabilities
(never path components), with possession of an id authorizing its
commit (the single-user loopback trust model). Commits SHALL check
expiry synchronously, be one-shot (replays are expired), serialize
with installs, and clean up on success and failure; daemon startup
SHALL reclaim abandoned staging directories; concurrent staged
uploads SHALL be bounded (oldest evicted with a typed busy error).

#### Scenario: Replay and restart

- **WHEN** a staging id is committed twice, or the daemon restarts
  with staged directories on disk
- **THEN** the second commit is an expired 404 and startup removes
  the orphans

### Requirement: Archive file import

The daemon SHALL import local `.skill` and `.zip` archives (both zip
containers) in two steps: a preview request parses and stages the
archive server-side, returning the same mandatory preview; a commit
request installs the staged skill. Archives SHALL be accepted only
with `SKILL.md` at the root or in exactly one top-level directory.
Safety rules: compressed size ≤ 10 MiB, uncompressed total ≤ 50 MiB,
≤ 256 entries, entry paths must resolve strictly inside the target
(absolute paths, `..`, and symlinked entries reject the archive), and
violations reject with the specific reason. Staged previews SHALL be
cleaned up on a short TTL.

#### Scenario: Import a .skill file

- **WHEN** the user picks a `name.skill` archive whose root holds a
  valid SKILL.md and previews it
- **THEN** the preview shows its content and files, and committing
  installs it into the registry (import-badged) like any skill

#### Scenario: Hostile archives reject with reasons

- **WHEN** an archive contains a `../escape` entry, a symlink entry,
  exceeds a cap, or holds no SKILL.md
- **THEN** the preview rejects with the specific violation and nothing
  is staged or installed

#### Scenario: Extension and layout equivalence

- **WHEN** the same skill arrives as `name.skill` or `name.zip`, with
  `SKILL.md` at the root or inside exactly one directory
- **THEN** both install identically

### Requirement: Provenance

Every skill installed from the marketplace or an imported file SHALL
carry a reserved `.karta/provenance.yaml` sidecar inside its
directory: a versioned, bounded schema recording origin
(`marketplace` or `import`), source repository, ref, and immutable
commit when applicable, install timestamp, and a content hash over
the canonical payload tree EXCLUDING the sidecar itself (sorted
paths, deterministic tuple encoding, content only). An incoming
bundle containing the reserved path rejects (no provenance
forgery); unrelated user-authored `.karta/*` files are untouched. A
missing OR malformed sidecar yields origin `local` (the skill still
loads); provenance is descriptive metadata, never authenticity
proof. Skill listings SHALL expose the origin. Local skills created
in-app remain origin `local` with no sidecar.

#### Scenario: Origin is visible and durable

- **WHEN** a marketplace skill is installed and the daemon restarts
- **THEN** the skill still lists with its marketplace origin, and its
  provenance sidecar still names the source repo, pinned commit, and
  hash

#### Scenario: Forged and malformed provenance

- **WHEN** an upload contains its own `.karta/provenance.yaml`, or an
  installed sidecar is corrupted
- **THEN** the upload rejects, and the corrupted skill loads with
  origin `local`
