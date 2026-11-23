# Design

## Context

The operator hand-moved the entire skills tree to `~/.zbot/skills/`
(all 14 operator skills, raw frontmatter included, `_shared/` bundle,
scripts/references/assets subdirectories) and emptied
`config/skills/`. The registry reads `config/skills/` — skills are
currently invisible. Hooks follow the same layout directive
(`~/.zbot/hooks/`).

## Investigation findings

- Registry `Dirs` carries Skills/Hooks as plain strings wired from
  `paths.ConfigDir("skills"/"hooks")` at two zbotd sites (LoadAll +
  Watch); the loader itself is root-agnostic.
- The strict SKILL.md frontmatter decode (`ParseSkillMD`,
  `skillFrontmatter` with the `path` sink) rejects the operator set's
  `license`/`metadata`/`allowed-tools` keys — 9 of the 14 hand-moved
  skills would load-error even with the root fixed.
- The skill seeder targets `paths.ConfigDir("skills")`; seeds must
  follow the root. Prompt seeds (persona + intent-analysis) stay in
  `config/prompts/` — prompts are daemon config, not user skills.
- `skipEntry` covers dotfiles/backups; `_shared/` (no SKILL.md) is
  naturally skipped as an assets-only directory.
- Precedent: `relocate-agents-dir` moved `agents/` top-level with a
  one-time `migrateLegacyAgents`; the marker-free seed machinery
  (roster + persona markers) never overwrites.

## Goals / Non-Goals

**Goals:** the loader follows the layout; classic installs migrate
once; the hand-moved state is recognized as done; raw operator skills
load (sink keys); seeds follow the root; specs/constitution reflect
the tree.

**Non-Goals:** moving prompts or mcps; changing the skill FORMAT
beyond sink keys; the exec plane for script skills (slice 3's tool
work); ward machinery.

## Decisions

1. **Roots, not indirection**: `config.Paths` gains `SkillsDir` and
   `HooksDir` (`~/.zbot/skills`, `~/.zbot/hooks`); `Subdirs` drops
   skills+hooks; `ensureDirs` creates the new roots; zbotd's two Dirs
   wirings switch to the new paths. No fallback reading of the legacy
   roots (migration covers it; dual-reading would resurrect deleted
   skills).
2. **Migration is hand-move-aware and one-time**: at bootstrap, for
   each legacy entry under `config/{skills,hooks}/`: if the same name
   exists at the top-level root → leave the legacy copy in place
   (never overwrite; it stays inert); else move (rename) it up. The
   operator's state (top-level populated, legacy empty) is a no-op.
   No marker: the legacy dirs' emptiness (or absence) IS the state —
   migration is naturally idempotent. After migration, empty legacy
   dirs are left in place (inert; deleting user dirs is not ours).
3. **Sink keys on the loader**: `skillFrontmatter` grows
   `License`/`Metadata`/`AllowedTools` sink fields (decoded, ignored,
   documented). Strictness for everything else stays (a typo'd key
   still errors — the sink set is closed and named).
4. **Seeds follow the root**: the skill seed walk targets
   `paths.SkillsDir`; prompt seeds unchanged. The persona/roster
   markers are untouched (homes seeded before keep their markers; the
   skills marker simply points its walk at the new root — a marked
   home with a hand-moved tree stays as the user arranged it).
5. **Constitution + spec text**: rule 1's tree listing gains the
   top-level `skills/` + `hooks/`; the config-plane delta (this
   change) updates relocation/bootstrap/self-heal and adds the
   sink-keys requirement.

## Risks / Trade-offs

- [Operator's raw set loads with scripts/assets that cannot execute
  yet] → acceptable: skills are context; the exec plane is slice 3.
  The shelf shows them; intent analysis can recommend them.
- [Legacy copies left behind on conflict] → inert and visible;
  documented (the user deletes at will).
- [No fallback read] → a user who moves skills back to config/ loses
  them from the registry — migration moves them up again at next
  bootstrap, which is the desired behavior.

## Migration Plan

Bootstrap-ordered: ensureDirs (new roots) → legacy migration (rename
up, skip conflicts) → seeds (skill walk at the new root; markers
govern) → config.yaml. Rollback = revert; user data untouched.

## Open Questions

- None blocking. (Hooks content is empty today; the root exists for
  layout symmetry and the hooks loader that ships with the hooks
  plane.)
