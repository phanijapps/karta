# Proposal

## Why

Delegation is live (`delegate_agent`, depth grants, per-agent inventory)
but the seeded roster is two thin examples; the skills registry surface
has zero content. The operator's tuned set at `~/Documents/zbot`
(skills + agents, the quality bar recorded in
docs/architecture/ward-research-exploration.md) gives Karta a real
starting roster: prompt-craft skills that work with today's machinery,
and specialist agents the root can route to immediately. It also primes
the semantic resource index slice 2 (add-intent-analysis) recommends
from. This change copies what is portable NOW and records what must
wait for the exec plane (slice 3) and wards (slices 3-4).

## What Changes

- **Seed 5 prompt-craft skills** (verbatim bodies, normalized
  frontmatter; seeded-and-indexable content — two of the five reference
  tools Karta does not implement yet, per the design's readiness note)
  into `~/.zbot/config/skills/<name>/`, marker-gated write-once,
  user-owned after: `clean-code`, `explain-like-socrates`,
  `humanizer`, `book-reader` (+ its 4 `references/` files), `eagle-eye`.
  Bodies copied verbatim from the operator's set; frontmatter trimmed to
  the loader's strict keys (name/description); a provenance note rides
  at the end of each body.
- **Expand + tune the agent example roster** (`.example.md` seeds,
  write-once): upgrade `research-agent` and `reviewer-agent` from the
  operator's richer bodies; add `planner-agent`, `writing-agent`,
  `builder-agent`, `adversarial-reviewer` (fixing the operator's
  `advesarial-` typo). Tuning rules: agentzero runtime specifics
  (shell/load_skill/ward tools, Active Ward Template, provider pins,
  skills/mcps lists) stripped or rephrased to portable contracts; ward
  clauses carried as explicit "when wards host runs (slice 3)" notes;
  `model: ""` (caller's model), no tools key (all tools), `max_depth: 0`.
- **Seed assets move to go:embed** in `internal/config` (replacing the
  inline example-agent strings), seeded with O_EXCL write-once — same
  mechanics as the ward vault templates.
- **Spec fix riding along**: config-plane's "bootstrap SHALL NOT seed
  any agent…" clause already drifts from the shipped behavior (two
  example agents seed since add-engine-delegation); this change's delta
  codifies the example-content exception (agents + skills) properly.
- OUT of scope: exec-dependent skills (`duckduckgo-search`, `pdf`,
  `light-panda-browser`, `yfinance-market-analysis`, `ppt-agent-skill`,
  `skill-creator`) and ward-dependent skills (`spec-builder`,
  `plan-composer`, `_shared/research_archetype.md`) — slice 3/4 inputs,
  sources recorded in design; the `general-purpose` agent (a shell-chore
  agent, inert without the exec plane); skill injection into runs
  (slice 2+); skills relocation to top-level `~/.zbot/skills/` (the
  deferred `relocate-skills-hooks` change; its migration carries these
  seeds along — recorded as a decision).

## Capabilities

### Modified Capabilities
- `config-plane`: the first-run seeding requirement gains the
  example-content exception (agents — codifying shipped behavior — and
  the new operator skills), write-once semantics, and the
  user-owned-after rules.

## Impact

`backend/internal/config` (embedded seed assets + seeding + tests),
`openspec/specs/config-plane` (delta), `docs/product/backlog.md`
(entry). No contract, handler, frontend, or engine changes — the
registry, watcher, and Settings surfaces pick the seeds up as ordinary
local skills/agents. Constitution untouched (seeds live under ~/.zbot).
