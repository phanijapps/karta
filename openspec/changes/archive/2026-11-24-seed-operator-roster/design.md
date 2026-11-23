# Design

## Context

The operator's tuned set lives at `~/Documents/zbot` (agentzero): 13
skills + a shared research-archetype contract, and a 7-agent roster
(config.yaml + AGENTS.md per agent). Checked against Karta's machinery:
the skill loader is strict (frontmatter `name`/`description`/`path`
only — the loader rejects `metadata`/`license`/`allowed-tools`), skills
load into the registry but are not injected into runs (slice 2+), and
there is no exec/MCP-runtime tool plane (slice 3) or Active Ward
Template/ward tool (slice 3). So only prompt-craft content is portable
today; the rest is recorded below as a deferred ledger.

## Investigation findings

- Operator skills by dependency: **prompt-craft** (clean-code,
  explain-like-socrates, humanizer, book-reader + references, eagle-eye
  — zero exec-script references in their bodies: no scripts/, pip,
  python, or node invocations), **exec-dependent**
  (duckduckgo-search, pdf, light-panda-browser, yfinance-market-analysis,
  ppt-agent-skill, skill-creator — bodies invoke scripts/pip/node;
  skill-creator is additionally agentzero-format-specific), and
  **ward-dependent** (spec-builder, plan-composer,
  `_shared/research_archetype.md` — reference the Active Ward Template
  packet, `ward(action=…)`, digests, `role_not_declared`).
  Readiness honesty: two of the prompt-craft seeds still reference
  tools Karta does not implement — book-reader's references discuss
  ward tooling and eagle-eye names `multimodal_analyze` (with its own
  text-only-model fallback note in the verbatim body). The shelf is
  seeded-and-indexable content, NOT executable capability; nothing
  here claims those tools exist.
- Operator agents carry agentzero runtime specifics: providerId/model
  pins, `skills:`/`mcps:` lists, shell/read/write_file/load_skill tool
  names, and ward clauses. `general-purpose` is a shell-chore agent —
  inert without the exec plane.
- Karta's seeding precedent: `seedExampleAgents` (inline strings,
  WriteFile-if-absent) and the ward vault templates (go:embed +
  O_EXCL write-once). The second is the better mechanic.
- config-plane's "SHALL NOT seed any agent…" clause already drifts from
  shipped behavior (two example agents since add-engine-delegation);
  this change's delta codifies the exception instead of widening it.

## Goals / Non-Goals

**Goals:** a real seeded roster (6 agents) the delegation routing table
can offer; a seeded skills shelf (5 skills) visible in Settings and
ready for slice 2's index; write-once seeding that respects user
ownership; the tuning policy written down so slices 3-4 know exactly
what was stripped and what returns.

**Non-Goals:** exec-dependent and ward-dependent content (deferred
ledger below), skill injection into runs, the `general-purpose` agent,
relocation of skills to top-level `~/.zbot/skills/` (deferred change
`relocate-skills-hooks`; its one-time migration carries these seeds —
they are ordinary files), any loader widening.

## Decisions

1. **Verbatim skills, tuned agents.** Skill bodies copy verbatim from
   the operator's set with only: frontmatter normalized to strict keys,
   a provenance footer (`<!-- seeded from ~/Documents/zbot/skills/…
   (operator set, 2026-11); user-owned after first write -->`), and
   descriptions kept within the 512-char bound the mutation API would
   enforce on any future edit. Agents are translated
   (config.yaml+AGENTS.md → one Karta `.example.md`) and tuned per the
   roster table below; tuning is subtractive and documented so slice 3
   can restore the ward clauses from source.
2. **Seeded roster** (all `model: ""` = caller's model; no tools key =
   all tools; `max_depth: 0` = no sub-delegation):

   | Seed | Source | Tuning |
   |---|---|---|
   | research-agent.example.md | research-agent | keep research contract (question/scope/evidence bar, primary sources, cross-check, provenance, uncertainty, synthesis); drop Active-Ward-Template output rules → one note: ward-hosted outputs arrive with slice 3 |
   | reviewer-agent.example.md | reviewer-agent | keep read-only review contract; sharpen from the operator body (validating implementation quality, spec alignment, reported outputs) |
   | adversarial-reviewer.example.md | advesarial-reviewer (typo fixed) | code-review discipline copied near-verbatim (findings need file:line + Fix:, severity about the change, what-not-to-flag, rationalizations table, never auto-edit, never declare done) — the strongest portable body in the set |
   | planner-agent.example.md | planner-agent | keep planning contract (verbatim request authoritative; live agent catalog only; recommended agent + capabilities separate; no silent fallback → bounded replan; plan lives in the run); drop spec-builder/plan-composer/template-digest machinery → slice-3/4 note |
   | writing-agent.example.md | writing-agent | near-verbatim (step contract, ground claims in inputs, preserve provenance, no invention, state gaps); ward template clauses → slice-3 note |
   | builder-agent.example.md | builder-agent | keep delegation modes + step-executor contract + destructive-op discipline + clean-code skill pointer (the skill ships in this change); tools section → "tools registered for this execution"; ward snapshot clauses → slice-3 note |

3. **Embed layout** under `backend/internal/config/seedassets/`:
   `agents/*.example.md`, `skills/<name>/SKILL.md`, and
   `skills/book-reader/references/*.md`. `seedExampleAgents` is replaced
   by one embed-walking seeder publishing each file crash-atomically
   (temp dotfile + os.Link — link never replaces, so an interrupted
   process cannot strand a partial destination). Resurrection is
   prevented by a per-home MARKER (`.example-roster-seeded`), not by
   file absence: seeding runs once — fresh homes and pre-marker
   upgrades gain their missing examples; after the marker exists,
   bootstrap never seeds again, so user deletions stick. Concurrent
   bootstraps are safe: publishes are atomic, and the marker lands only
   after a full successful walk.
4. **Loader stays strict.** Normalization happens at seed time; the
   registry's strict frontmatter decode is a feature, not friction.
5. **Deferred-content ledger** (recorded in the change summary and
   docs/product/backlog.md when the unblocking slice lands):
   - Slice 3 (exec plane + ward tool): duckduckgo-search, pdf,
     light-panda-browser, yfinance-market-analysis, ppt-agent-skill,
     skill-creator (scripts ride along as bundled assets), and the
     general-purpose agent; restores ward clauses in agent bodies.
   - Slice 4 (research orchestration): spec-builder, plan-composer,
     `_shared/research_archetype.md` as the flow skills (they are the
     evolved, template-driven form of the backlog's "spec skill →
     spec.md" idea).
6. **Skills seed into the current tree** (`~/.zbot/config/skills/`) —
   the only location the registry loads today. When
   `relocate-skills-hooks` lands, its migration moves seeds like any
   user file (write-once seeds never fight it).

## Risks / Trade-offs

- [Seeds without a runtime consumer] → the shelf is inert until slice 2
  injects skill recommendations; standalone value is Settings
  visibility, marketplace coexistence (origin `local`), and a primed
  index — recorded honestly in the backlog entry.
- [Operator bodies mention tools Karta lacks] → tuned per the roster
  table; verbatim policy applies only to skills whose bodies have zero
  exec references (verified by grep during investigation).
- [Two reviewer-shaped agents could confuse routing] → distinct
  descriptions: reviewer-agent = validate implementation vs spec and
  reported outputs; adversarial-reviewer = code-review defect hunting
  with the findings discipline.

## Migration Plan

Additive. Fresh homes get the full seed set; EXISTING (pre-marker)
homes get their missing examples ONCE on the next bootstrap (their
research/reviewer `.example.md` seeds from add-engine-delegation are
already present, so only the four new agents + five skills appear;
user edits everywhere are untouched — the link-publish never replaces).
After the marker, bootstrap never seeds again. Rollback = revert;
seeds are inert user files.

## Open Questions

- None blocking. (The `~/.zbot/skills` top-level relocation remains a
  separate, deferred change by operator direction.)
