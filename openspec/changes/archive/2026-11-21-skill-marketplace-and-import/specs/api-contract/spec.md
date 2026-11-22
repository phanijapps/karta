# Spec Delta

## ADDED Requirements

### Requirement: Skill marketplace endpoints

The contract SHALL define daemon-proxied marketplace endpoints:
search (query → results with name, description, source, install
count; typed index-unreachable failure), preview (a canonical GitHub
`owner/repo` identity → the parsed SKILL.md, complete file list with
sizes, license, the resolved immutable commit, AND an opaque preview
capability binding repo, commit, skill paths, and per-file digests),
and install (the capability plus the chosen skill subset → per-skill
created/skipped/failed outcomes, MCP-import semantics). Clients never
dial the index or GitHub directly. The failure matrix SHALL be typed
end to end: index-unreachable, upstream-unreachable, upstream
rate-limited (with retry guidance), repo-not-found, no-skills-found,
archive violations, invalid-skill, changed-upstream, duplicate,
capacity, io_error.

#### Scenario: Search through the daemon

- **WHEN** the search endpoint is called with a query and the index is
  reachable
- **THEN** it returns 200 with the index's results and no client-side
  fetch of the index occurs

#### Scenario: Preview binds and install consumes

- **WHEN** the preview endpoint is called for a public repository and
  the install endpoint follows with the capability and a skill subset
- **THEN** preview returns the content, file list, commit, and
  capability, and install returns one outcome per chosen skill

### Requirement: Skill archive import endpoints

The contract SHALL define a two-step file import: a preview request
carrying the `.skill`/`.zip` archive as multipart/form-data with
EXACTLY one file part named `archive` (no other parts, body ≤10 MiB
enforced before parsing) — the only endpoint exempt from the
JSON-mutation rule, protected instead by trusted-Host plus Origin
enforcement; and a commit request carrying the opaque staging id.
Preview rejections (no SKILL.md, traversal entries, symlinks, cap
violations, corrupt archive, extra parts, reserved provenance path)
SHALL be typed 400s naming the violation; commit of an unknown,
expired, or already-consumed staging id SHALL 404; exceeding the
concurrent staging bound SHALL be a typed busy failure.

#### Scenario: Preview then commit

- **WHEN** a valid archive is previewed and its staging id committed
- **THEN** the skill installs and both responses carry the skill's
  parsed content and origin

#### Scenario: Staging expires or replays

- **WHEN** a commit arrives for a staging id past its TTL or already
  consumed
- **THEN** it returns 404 and nothing installs

### Requirement: Skill provenance fields

Skill list and detail responses SHALL carry an `origin`
(`local` | `marketplace` | `import`); marketplace and imported skills
SHALL expose their source repository and ref when recorded.

#### Scenario: Origins in listings

- **WHEN** skills of each origin are installed
- **THEN** the list distinguishes them and the marketplace entry names
  its source repository
