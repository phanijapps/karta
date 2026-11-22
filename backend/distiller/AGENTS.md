# distiller

The evolution loop's write path: finished sessions from the
conversation store are distilled (LLM extraction through the provider
plane) into chitta through the trusted seam — `source: distiller`,
transcripts never persisted, re-runs idempotent (contribution identity).

## Boundaries

- Plugin-zone composition component: imports kernel seams
  (`internal/domain`, `internal/store` via the cmd adapter,
  `internal/providers` via the cmd adapter) AND the chitta public
  surface; nothing imports it but `cmd/zbotd`.
- chitta never imports the store; the store never imports chitta; the
  loop closes here.
- Checkpoint markers live in chitta's metadata (`distilled:<session>`),
  NOT in zbot.db (append-only, conversations only).
- Dead-letter after 3 failed extraction attempts (observable in logs).

## Entrypoints

- `distiller.New(chitta, source, llm, settings, log)` → `Run(ctx)` —
  serial background worker (5-minute ticker; one session at a time).
- `distiller.FileSettings{Dir}` — reads daemon-managed
  `distillation.json` + `distillation-prompt.md` overrides each pass.

## Testing

`go test ./distiller/` — fake chitta (OpenInMemory), fake LLM, fake
source; no network, no disk (temp dirs only).
