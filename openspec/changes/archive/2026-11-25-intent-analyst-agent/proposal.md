# Proposal

## Why

Pre-execution intent analysis is a silent, unbudgeted bare completion: it
has no token configuration (G1), is not an observable agent step (G2 — no
execution row, no token accounting, one terminal event, no started event),
and the model's own limits are nowhere on the catalog. The decision
document (`docs/architecture/intent-analysis-enhancement-plan.md`, §0,
§3.2, §3.4) settles the shape: the analyst is a **separately configured,
observed, and budgeted decision step** — a validated single completion,
never a tool loop — whose limits derive from the selected model, with no
analyst-side pruning. This is slice S1 of that plan.

## What Changes

- **The intent-analyst becomes a real agent**: a seeded `AgentSpec`
  (`~/.zbot/agents/intent-analyst.md`: `tools: []`, `max_depth: 0`,
  `prompt: intent-analysis`, model inherit) that shows in Settings →
  Agents, carries the rubric by prompt reference, and can override the
  model per-agent. Every analysis runs as a **dedicated execution row**
  (`agent: intent-analyst`, parented under the root run) with token
  accounting rolled into it.
- **Event trail (BREAKING)**: the single `intent_analysis` event kind is
  replaced by four kinds — `analysis_started`, `analysis_completed`
  (tokens in/out, ms, attempts, mode, model, window, trimming),
  `analysis_bypassed` (reason), `analysis_failed` (reason, attempts). The
  payload (`IntentAnalysisPayload`, carried in the `intent_analysis`
  property) keeps its shape and gains the accounting fields; a `started`
  status joins the enum. Durable replay of pre-existing rows translates
  the legacy kind by status so old sessions still render.
- **Model-derived token limits**: catalog entries gain `context_window`,
  discovered for Ollama through `/api/show` (`*.context_length`) at
  refresh, otherwise looked up in a user-editable
  `~/.zbot/config/providers/model_limits.yaml` (model-id prefix → window,
  hot-reloaded, with a small built-in table), else the 32,000 fallback.
  The analyst sends `max_tokens = window`; the composed prompt is measured
  and the per-kind packet caps shrink **only** when it would exceed
  `window − 4k` (one safety valve). No token knobs.
- **Settings card** (Settings → Advanced): provider/model picker fed by
  the catalog (with windows), a read-only derivation line
  ("model · window (source) · analyst: full window · packet N tokens · no
  trimming"), and a **Test analysis** button that runs one real analysis
  for a sample goal and reports status, tokens in/out, ms, attempts, and
  whether the valve fired. Backed by the daemon-managed
  `~/.zbot/config/intent-analysis.json` through new endpoints
  `GET/PUT /api/intent-analysis` and `POST /api/intent-analysis/test`.
- **Model precedence** (operator decision): the ward's orchestrator model
  wins when a ward orchestrates; otherwise Settings card → agent spec
  `model` → `intent_analysis.model` (config.yaml, kept for compatibility)
  → the run's model.
- **System agents are not delegatable**: a reserved system-role agent
  (the analyst) never appears in the delegate routing table and never in
  the analyst's own catalog packet.

## Capabilities

### New Capabilities

_None — every behavior lands in an existing capability._

### Modified Capabilities

- `intent-analysis`: observable, budgeted analysis (four event kinds,
  dedicated execution row, token accounting); the analyst as a registered
  system agent; model-derived token limits with the single safety valve;
  the model precedence chain.
- `model-catalog`: entries carry a context window (Ollama-discovered,
  table, or absent), persisted with the catalog entry.
- `settings-shell`: the live Intent Analysis card (picker, derivation,
  Test analysis).
- `engine-delegation`: system-role agents are never offered for delegation.
- `chat-modes`: the research pipeline emits the `analysis_*` event kinds
  (the `intent_analysis` kind no longer exists).
- `config-plane`: bootstrap seeds the intent-analyst agent once per home
  under its own marker.

## Impact

- **Contract**: `contracts/openapi.yaml` — `TimelineEventKind` enum
  (BREAKING: `intent_analysis` removed, four `analysis_*` kinds added),
  `IntentAnalysisPayload` gains `started` + accounting fields,
  `Model`/`ModelEntry` gain `context_window`, new paths
  `/api/intent-analysis` (GET/PUT) and `/api/intent-analysis/test` (POST).
  Regenerated via `task codegen` (`CONTRACT_CHANGE=1`).
- **Backend**: `internal/domain` (event kinds + payload),
  `internal/providers` (model windows, Ollama `/api/show`, limits file,
  resolution), `internal/store` (catalog limits persistence, the analysis
  execution row, latest-of-kinds), `internal/registry` (system-agent
  rule), `internal/config` (seed), `internal/intent` (analyst binding,
  accounting, valve, events), `internal/engine/eino` (delegate exclusion),
  `internal/server` (settings + test handlers, replay translation,
  session state), `cmd/zbotd` (wiring).
- **Frontend**: `shared/api/client.ts` (three calls), the SSE timeline
  store and research chat (new kinds), a new
  `features/settings/advanced/IntentAnalysisCard.svelte`.
- **Docs**: package `AGENTS.md` files in touched packages,
  `docs/product/product-context.md`, the plan's slice table.
