# Model catalog verification (tasks 5.1)

Environment: real daemon (rebuilt) + real ollama; UI via the running dev
server. Screenshot: `evidence-live-picker.png`.

```
$ curl -s localhost:8787/api/models                      # fresh daemon
{"models":[]}                                            # empty start (manual mode)

$ curl -X POST -H 'Content-Type: application/json' \
    localhost:8787/api/providers/ollama/models/refresh
{"models":[{"id":"glm-5.3:cloud"},{"id":"deepseek-v4.1-flash:cloud"},…]}   # real ollama

$ curl -s localhost:8787/api/models | jq '.models | length, .[0]'
63                                                      # catalog updated
{"provider":"ollama","id":"glm-5.3:cloud"}              # flat entries + refreshed_at

# Idle daemon issues no provider requests: 10s window with no traffic to
# port 11434 (and /api/models answers are instant snapshot reads).

$ curl -X POST …/providers/ghost/models/refresh → 404    # unknown provider typed
```

UI (Playwright against the live stack): header picker rendered the
`ollama` group with 23 visible options (63 catalog entries incl. other
providers' groups), selected `glm-5.3:cloud`; the "Simulated" badge
labels the selection honestly. Failed-refresh-keeps-list and
non-destructive behavior are covered by hermetic handler tests
(`TestRefreshProviderModels/failed_refresh_keeps_the_previous_catalog_entry`).

## Review (task 6.1) — gpt-6.1-sol

Verdict: **BLOCK — 3 P1, 3 P2**. All six findings fixed (none deferred):

- **P1 selection identity**: option values are provider-qualified
  (`provider/model`) so selection preserves which provider was chosen;
  duplicate ids across providers tested.
- **P1 stale selection**: pickers reconcile the bound selection against
  the latest catalog — a removed model clears to the placeholder instead
  of silently mismatching; removal case tested end-to-end (refresh →
  selection cleared).
- **P1 catalog vs provider lifecycle**: `GET /api/models` reconciles
  entries and timestamps against the CURRENT provider set at read time
  (covers API deletes, renames, watcher changes, hand edits); in-flight
  refreshes of deleted providers are not catalogued; Settings
  create/update/delete now announce `karta:models-refreshed` so mounted
  pickers re-read; lifecycle test covers delete-dropout.
- **P2 overlapping loads**: request-generation fencing in the picker —
  only the latest load commits models/error/loading state.
- **P2 weak failure coverage**: backend table-test proves all typed
  refresh failures preserve the exact list AND timestamp; browser test
  covers refresh typed-failure guidance + catalog retention.
- **P2 unstubbed daemon in one shell test**: the sidebars test pins
  `GET /api/models` to a deterministic empty catalog.

Post-fix verification (task 6.2): `task verify` green — codegen
byte-stable, golangci-lint 0 issues, race tests green, svelte-check 0/0,
build clean, 30/30 Playwright tests.

## Archive-time note (2026-11-21)

The `specs/mock-chat/` delta was removed at archive time: its target
requirement ("Model and action previews are honest") was retired by
add-simple-chat's archive minutes earlier (the mock chat was superseded by
the real chat; mock-chat main spec deleted). The delta's surviving intent
lives in `openspec/specs/simple-chat/spec.md` ("Chat renders safely and
honestly"); the header model-picker/catalog UX belongs to the settings-shell
deltas of the not-yet-archived later changes. Order-dependency recorded here
so the history explains the gap.
