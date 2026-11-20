# Provider slice verification (task 6.1)

Environment: local ollama (`http://localhost:11434/v1`, OpenAI-compatible
endpoint), real `zbotd` from `backend/cmd/zbotd`, test home
`/tmp/zbot-ollama-verify` (created fresh), SPA via `pnpm preview` with the
dev-proxy pointing at the loopback daemon. Date: 2026-11-19.

## Daemon leg (command log)

`config.yaml` providers entry:

```yaml
providers:
  - name: ollama
    type: openai-compatible
    base_url: http://localhost:11434/v1
```

```
$ ZBOT_HOME=/tmp/zbot-ollama-verify go run ./cmd/zbotd   # from backend/
2026/11/19 … zbotd dev listening on 127.0.0.1:8787 (home /tmp/zbot-ollama-verify, 1 provider(s))

$ curl -s http://127.0.0.1:8787/healthz
{"service":"zbotd","status":"ok","version":"dev"}

$ curl -s http://127.0.0.1:8787/api/providers
{"providers":[{"base_url":"http://localhost:11434/v1","credential_configured":false,"name":"ollama","type":"openai-compatible"}]}

$ curl -s http://127.0.0.1:8787/api/providers/ollama/models   # truncated
{"models":[{"id":"glm-5.3:cloud"},{"id":"deepseek-v4.1-flash:cloud"},{"id":"glm-5.3-flash:cloud"},
{"id":"glm-5.2:cloud"},{"id":"gpt-oss:120b-cloud"},{"id":"snowflake-arctic-embed:latest"},
{"id":"hf.co/DavidAU/GLM-4.7-Flash-Uncensored-Heretic-NEO-CODE-Imatrix-MAX-GGUF:IQ4_XS"},…]}

$ curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8787/api/providers/nope/models
404
```

## UI leg

Playwright (headed Chromium, dark theme, 1440×900) against
`pnpm preview` + the live daemon:

- Opened Settings → Providers: the real `ollama` provider rendered with
  base URL and no-credential hint (no fixture data involved).
- Clicked **Test connection**: `Connection OK — models` rendered
  **22 model chips**, first id `glm-5.3:cloud` — matching the daemon
  response above.
- Screenshots: `evidence/providers-live-list-dark-1440.png`,
  `evidence/providers-test-connection-dark-1440.png`.

## Result

PASS — configure ollama in `config.yaml`, run `zbotd`, open Karta
Settings → Providers, Test connection lists real model ids. Unit and
interaction tests stay fully offline (httptest fakes; Playwright route
mocks).

## Review (task 7.1)

Read-only constitution review (zbot-reviewer checklist; project roster not
loaded in this session's harness, so the builtin read-only reviewer ran the
identical boundary checklist). Verdict: **OK with notes — 0 P0, 0 P1, 8 P2**;
all five boundary verdicts hold (engine isolation, FSD direction, backend
dependency graph, contract-first, eventbus omission per design D6).

All eight P2 findings were fixed (none deferred):

1. Config validation aligned with contract bounds — Id-pattern names,
   64-provider cap, 128-char `api_key_env` (`config.go`, tests in
   `TestLoadProviderBoundaries`).
2. `design.md` reconciled with shipped behavior (goals/sessions = generated
   501 stubs, not unmounted 404s).
3. Tasks 5.1–5.4 ticked (were implemented, unmarked).
4. `backend/internal/server/apigen/AGENTS.md` added (DoD #4).
5. `go mod tidy` — chi / oapi-codegen-runtime moved to direct requires.
6. Stale in-flight Test-connection results discarded on provider switch
   (`Settings.svelte`).
7. Deterministic `/api` route guards added to the two tests that entered
   Providers unmocked (a11y pair, narrow-settings).
8. Model-list body bounded by `io.LimitReader` (8 MiB → malformed past the
   cap) and models capped at the contract's 1024 maxItems (`client.go`,
   tests in `TestListModelsBounds`).

Final `task verify` green after the fixes (task 7.2): codegen drift-free,
golangci-lint 0 issues, `go test -race` all packages, svelte-check 0/0,
build clean, 14/14 Playwright tests.
