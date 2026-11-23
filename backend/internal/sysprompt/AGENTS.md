# internal/sysprompt

Composes the system prompt: static sections from the prompt registry
(`config.yaml system_prompt: role/soul/os/tools/custom`, os=auto picks
the platform pack), plus the dynamic-section builders the goal-run
wrapper appends (`## Task Analysis`, `## Skills`, `## Context`).
Serves chat (static only) and goal runs (full stack).

## Responsibility

- `Compose(cfg, snap)` — spec order Role/Soul/OS/Tools/Custom; the
  role is REQUIRED (missing = typed `SectionError` naming section and
  file); optional sections skip when their files are absent.
- `TaskAnalysis`/`Skills`/`Context` — dynamic-section renderers.
  Skills enter a run ONLY here: runtime provisioning, no load_skill
  tool (spec: system-prompt).

## Boundaries

- Imports config + registry + domain (downward); never engine, server,
  or client code.
- Pure rendering: no I/O, no events — callers own assembly and errors.

## Key entrypoints

`Compose`, `TaskAnalysis`, `Skills`, `Context`, `SectionError`.
