# Research Protocol

Figure out what kind of request this is BEFORE acting. One assessment,
every turn.

## Assess first

- **Trivial** (greetings, quick facts, one-shot answers, small edits):
  answer directly. No wards, no planning, no tools beyond what the
  answer needs. Do not over-organize.
- **Research / domain work** (analysis, comparison, investigation,
  anything with sources and a durable answer): work in a WARD — wards
  are reusable domain workspaces (stock analysis, india analysis, macro
  research). Never task-specific.

## The ward decision (research requests only)

1. Call `wards` — see the vault index.
2. A ward covers this domain → `ward_use(name)` with the EXACT listed
   name. Never invent a near-duplicate.
3. No ward fits → `ward_create(name, archetype)` with a REUSABLE
   domain name (kebab-case: `market-research` good;
   `amd-q3-earnings` bad) and the matching archetype
   (`research` for evidence work, `generic` otherwise).
4. Work INSIDE the ward: read the canonical page first (it is the
   accumulated knowledge), respect `AGENTS.md` (the ward's own rules),
   write findings to the canonical page or the layout's declared roles,
   append material operations to `log.md`.

## Ward discipline

- The ward's `AGENTS.md` and layout govern your work there. The layout
  is structure data, not a suggestion.
- Sources go to `sources/`; notes to `pages/`; the synthesis
  accumulates on the canonical page.
- A ward is reused across many tasks in its domain. Do not scatter:
  one domain, one ward.

<!-- Seeded default (Karta, 2026-11; shape from the operator's first-turn protocol + INSTRUCTIONS warm/cold paths). User-owned after first write — edit freely. -->
