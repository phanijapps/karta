# Skill marketplace & import — design review record (pre-implementation)

## gpt-6.1-sol design review (zbot-reviewer subagent)

Verdict: **BLOCK — 7 P1, 5 P2**; direction, contract-first ordering,
package placement, registry-not-eventbus reasoning, zip-bomb
double-enforcement, third-delta preservation of predecessor fixes,
and the D7 serial-archive approach (with the rebase correction) all
confirmed correct. All 12 findings folded:

- **P1 preview→install TOCTOU** → previews issue opaque capabilities
  binding repo + IMMUTABLE commit + skill paths + per-file digests;
  installs consume capabilities only; expired ones re-fetch the
  pinned commit and verify digests (typed changed-upstream on
  mismatch); no install-without-preview (design D2).
- **P1 multipart vs the JSON guard** → the import endpoint is the
  single exemption: trusted-Host (unchanged) + Origin enforcement
  replaces the content-type barrier there; exactly one `archive`
  file part, pre-parse size bound; every other route unchanged;
  guard matrix tested (D5).
- **P1 tarballs lacked the safety envelope** → ONE SafeBundle policy
  for uploads and repo tarballs: bounded download/decompression
  (headers AND streaming), entry/file caps, regular-files-only
  (symlink/hardlink/special reject), shared path validator, nested
  archives opaque, pasted URLs parse to canonical GitHub identities
  (D3).
- **P1 path validation underspecified** → single raw-name validator
  (absolute/drive/UNC, `..`, backslash, NUL) + separator-aware
  containment (sibling-prefix escapes), normalized duplicates and
  file/dir collisions, re-applied inside InstallSkill for hostile
  direct calls (D4).
- **P1 staging lifecycle incomplete** → 256-bit bearer capabilities
  resolved via map (never path-joined), possession authorizes
  (single-user trust model stated), synchronous expiry, one-shot
  commits, startup reclamation, ≤8 concurrent bound (D8).
- **P1 multi-file install not atomic/validated** → validate-then-
  stage-then-atomic-rename primitive: loader-exact SKILL.md
  validation at preview AND install (accepted frontmatter fixed),
  private .stage build, directory no-clobber rename, name-duplicate
  and slug-collision refusals incl. snapshot-invisible occupants,
  mode subset 0o644/0o755 (executable bits survive) (D7).
- **P1 untrusted markdown rendering** → INERT preview: no raw HTML,
  no remote resource loading, safe schemes only, untrusted label;
  hostile fixtures prove inertness (D9).
- **P2 provenance/hash semantics** → versioned bounded schema,
  reserved path (incoming copies reject; unrelated .karta/* left
  alone), hash over payload tree EXCLUDING the sidecar, malformed →
  origin local, descriptive-not-authentic stated (D6).
- **P2 bundle DTO lacked modes** → SkillBundle{Path, Mode, Data} with
  the validated mode subset; executable round-trip tested.
- **P2 API determinism** → multipart single-part rule, capability +
  subset + per-skill outcomes, full typed status/code matrix incl.
  upstream-rate-limited with guidance (api-contract delta).
- **P2 scenario gaps** → extension/layout equivalence, boundary caps,
  stale-index-meets-reality, cache freshness defined, import-origin
  durability, forged/malformed provenance, nested archives (spec).
- **P2 preflight fallback swaps the product** → preflight is a HARD
  GATE: pin endpoint + schema + fixtures or STOP and revalidate the
  fallback with updated expectations before proceeding (D1, task
  1.1); P2 dialog guarantees → modality + async-binding inherited and
  tested BY NAME (D9, task 4.4).

Post-fold `openspec validate` green.

## Implementation review (task 5.1) — gpt-6.1-sol

Verdict: **BLOCK — 7 P1, 8 P2**; boundaries, eventbus reasoning, the
implemented safety pieces (zip duplicate/symlink rejection, one-shot
archive commits, startup reclamation, staging rename, inert rendering,
async reconciliation), and default loopback posture confirmed. All 15
findings fixed:

- **P1 subset not authorized** → install validates the ENTIRE nonempty
  bounded subset against the capability BEFORE any fetch; unrecorded
  names 400 without writes; CompareDigests verifies executable mode
  too.
- **P1 repo capabilities never consumed** → ConsumeRepo is one-shot
  (replay 404, tested); the BINDING outlives the preview cache (24h)
  so expired-preview installs re-fetch the pinned commit and verify
  digests (changed-upstream tested through the handler with a
  content-changed fake).
- **P1 path validation gaps** → raw archive names validated BEFORE
  wrapper stripping (rawEntryName), directories validated, duplicate
  entries reject, order-independent file/directory collision checks on
  both parsers, reserved provenance path enforced SKILL-RELATIVELY in
  both tar detection and zip parsing.
- **P1 tar decompression envelope** → the decompressed stream is
  bounded and COUNTED (tar headers and padding included via
  countLimitReader), declared aggregate sizes pre-checked before body
  reads, actual streamed bytes re-verified.
- **P1 wrapped-layout zip round-trip** → ONE canonical skill-relative
  file list built before staging/digests; unrelated files outside the
  wrapper reject; wrapped-layout round-trip test added.
- **P1 InstallSkill didn't parse SKILL.md** → validateBundle parses via
  ParseSkillMD and requires name/description to MATCH the frontmatter
  (authoritative); collision checks are order-independent.
- **P1 commit response violated the contract** → commit returns the
  contracted SkillInstallOutcome always; skill update/detail responses
  carry origin (+source, +ref).
- **P2 staging busy/cleanup** → lazy TTL sweep on every stage; the 9th
  upload gets the observable staging_busy (no live eviction); tests.
- **P2 multipart bound** → body capped at exactly the contracted 10 MiB
  (413 boundary-tested).
- **P2 provenance** → bounded (4KiB) + semantic validation (hash
  64-hex, timestamps, source+commit required for marketplace); ref
  exposed contract-first (Skill/SkillDetail gain ref).
- **P2 preview async binding** → a generation counter binds every
  repo/archive preview flow; stale completions ignored; dismissal
  invalidates.
- **P2 focus containment** → real opener capture + restoration; Tab
  wraps from the card container and both ends.
- **P2 preview digests + entry caps** → per-file sha256 in the preview
  contract (regenerated); archive entry cap reconciled to 512 across
  spec/contract/tests.
- **P2 GitHub parser strictness** → https/github.com exact (optional
  /tree/<ref>), http/credentials/deeper paths reject (tested).
- **P2 coverage** → handler tests: replay 404, fresh-preview duplicate,
  subset authorization, changed-upstream, multipart extra-part +
  oversize; skillhub tests: wrapped round-trip, busy bound + sweep,
  streaming bomb (deflated zeros), strict parsing, exec-mode digest.
  Frontend: Local badge shown for every skill. Contract security
  declaration documents the exposure posture.

## UI amendment (post-review, same change)

User refinement: the Installed view's Browse-marketplace button
duplicated the Installed|Browse toggle and manual Add-skill creation is
not the workflow (skills arrive by import/marketplace). Changed: the
Installed view offers a single **Import skill** action (the file
picker); Add-skill and Browse-marketplace buttons removed; marketplace
search results paginate (8/page with Previous/Next). Spec delta
amended accordingly (create scenario → edit scenario + pagination
scenario); the manual-create Playwright flow rewritten as edit+rename;
pagination and installed-import-button tests added. All gates re-run
green.
