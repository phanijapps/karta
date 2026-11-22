# skillhub

The daemon-side skills marketplace plane: the pinned skills.sh index
client (`GET /api/search?q=`, five-minute cache, typed unreachable),
the GitHub repo fetcher (default-branch → immutable commit → codeload
tarball), the ONE SafeBundle extraction envelope shared by tarballs and
`.skill`/`.zip` uploads (50 MiB download/uncompressed, 512 entries,
10 MiB per file, headers AND streaming enforcement, regular files and
dirs only — symlink/hardlink/special entries reject, shared path
validator via registry.ValidBundlePath, nested archives stay opaque),
skill-folder detection (skills/*/, .agents/skills/*/, root, single
top-level dir) with loader-exact SKILL.md parsing
(registry.ParseSkillMD — preview never accepts what the registry would
drop), and the capability store binding installs to previewed bytes.

- Capabilities are 256-bit opaque bearer tokens (possession authorizes
  on the single-user loopback daemon): repo previews hold source +
  pinned commit + per-file digests; archives stage on disk under
  ~/.zbot/tmp/skill-import/<random-token> (0o700, TTL 15 min, ≤8
  stages with evict-oldest, one-shot commits with synchronous expiry,
  startup reclamation). Install re-fetches the PINNED commit and
  verifies digests (CompareDigests) — changed-upstream is typed.
- Boundaries: imports `registry` (types, ParseSkillMD, InstallSkill)
  and stdlib; sits below `server`; never engine/*, never clients. The
  daemon dials out only at explicit user action; tests run entirely on
  httptest fixtures and in-test archives (zero network).
- Key entrypoints: `HTTPIndexClient.Search`, `GitHubRepoFetcher.
  Preview/ExtractCommit`, `ParseSource`, `Store.NewStore/RegisterRepoPreview/
  LookupRepo/StageArchive/CommitArchive`, `CompareDigests`. Envelope
  constants at the top of skillhub.go.
