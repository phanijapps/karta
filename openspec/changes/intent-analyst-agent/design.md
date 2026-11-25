# Design

## Context

The analysis today is one `Completer` call inside `intent.Wrapper.RunGoal`
(`analyzer.go`), emitted as a single `intent_analysis` event after the
engine's first event. `providers.Model` is `{ID}` only; the catalog
persists `model_ids` as a JSON string list. Providers are all one type
(`openai-compatible`), so "Ollama" is a property of the endpoint, not of
the configuration. The operator's decisions bind the shape: validated
single decision (no tool loop), no token knobs, the model's own limits,
ward orchestrator model wins, Plan events in S2.

## Goals / Non-Goals

**Goals**
- The analyst is a registered, seeded, user-editable agent that runs as
  an observed execution row with token accounting and a started/terminal
  event trail.
- Limits derive from the selected model through one resolution chain
  (Ollama-discovered → table → 32k) with exactly one input safety valve.
- A Settings card (picker + read-only derivation + Test analysis).

**Non-Goals** (S2–S4 of the plan)
- Contract v2 fields, the advisory packet handover, `## Plan` + plan
  events, MCP grants, skill injection budget, ward canonicalization,
  the warm-ward path, the Ollama output-mode ladder, adaptive confidence.

## Decisions

### D1 — The agent is a registry spec bound by a reserved name
`intent.AnalystAgentName = "intent-analyst"`; `registry.IsSystemAgent(name)`
holds the reserved set (the one name today). The wrapper resolves the
spec from the snapshot each run. **Rubric** resolution: the spec body
when it carries all four slot markers (a user who inlines the rubric
wins) → the prompt named by `spec.Prompt` → the `intent-analysis` prompt
→ the embedded default. The seed references `prompt: intent-analysis`
and its body is a comment explaining the override, so existing installs
keep their (possibly edited) prompt file as the rubric — no regression.
The seed runs under its own marker (`.intent-analyst-seeded`) so marked
 homes receive it once. *Alternative rejected:* seeding the rubric as the
body — it would silently override a user-edited prompt file on upgrade.

### D2 — Not delegable, not in its own catalog
`delegateDescription` and `delegateTool.Exec` skip system agents (typed
`ErrNotDelegatable`); `Wrapper.buildPacket` and `knownAgents` skip them
too. The analyst is a host step, not a worker. *Rejected:* filtering by
`tools: []` + `max_depth: 0` — the planner seed has the same shape and is
a delegable worker.

### D3 — Dedicated execution row + accounting seams
`store.CreateAnalysisExecution(sessionID, agent, task)` calls
`EnsureSession(sid, research)` (the root row must exist for the FK —
idempotent when the research path already created it) and inserts
`exec-intent-<unix-nano>` with `parent_id = exec-root-<sid>`, `agent`
and `delegation` = the agent name, `status running`, `task` = the goal
(bounded). The existing `FinishExecution` rolls tokens in. The wrapper
gets two seams: `Config.BeginAnalysis(sid, goal, model) execID` and
`Config.FinishAnalysis(execID, status, err, in, out)`; nil seams = no
row (tests, the Test endpoint). Skipped when the session id is empty
(the engine assigns one later — nothing to attach to).

### D4 — Event kinds and emission order
Domain: `analysis_started/completed/bypassed/failed`; the payload stays
`IntentAnalysisPayload` in the `intent_analysis` property (status `started`
joins the enum; kinds and status are 1:1). `Config.Emit(ev)` (renamed
from `Persist`) persists + publishes. With a known session id both the
started and the terminal event are emitted synchronously BEFORE
`Engine.RunGoal`, so the timeline reads started → terminal → run steps.
With an empty session id the terminal event is still emitted after the
engine's first event reveals the id (today's behavior); the started
event is dropped (no session to attach it to). Durable replay translates
a legacy `intent_analysis` row to the terminal kind by status
(`dbSessionEvents`); `GetSessionState` reads the latest of the three
terminal kinds (`store.LatestEventOfKinds`).

### D5 — Analyzer returns a `Decision`
`Analyze` returns `Decision{Analysis, Attempts, TokensIn, TokensOut,
PromptChars, Trimmed, MaxTokensDropped}`. The model seam gains an
optional `UsageCompleter` (`CompleteUsage(ctx, provider, model, system,
user, extra) (text, Usage, err)`); the daemon's `llmCaller` implements it
from `ChatResponse.Usage`; plain `Completer` fakes keep working with zero
usage. `max_tokens = window` rides `extra`. **Robustness exception**: if
the call fails typed-malformed with a 400 (a provider whose output cap
is below the window rejects the request), the same attempt is retried
once without `max_tokens` and the decision records `MaxTokensDropped`.
This is not a budget — a rejected request is an outage, and without the
guard every model on the 32k fallback with a smaller output cap (e.g.
DeepSeek's 8k) would lose analysis entirely.

### D6 — The one safety valve
`estimateTokens(chars) = ceil(chars/4)`. The analyzer composes the
system rubric + the user block for the default packet; if the estimate
exceeds `window − 4000`, `Packet.Ranked` is re-run with scaled caps
(`Caps{Skills, Agents, MCPs, Wards}`; ratio = budget/estimate, floors
2/2/2/4) and re-measured until it fits or the floors are hit; `Trimmed`
is reported. Catalog caps are otherwise untouched (20/20/20/64).

### D7 — Window resolution lives in `providers`
`providers.Model{ID, ContextWindow}`; `providers.ResolveWindow(m, limits)`
→ `(window, WindowOllama|WindowTable|WindowFallback)` with
`FallbackContextWindow = 32000`. The wrapper cannot import `providers`,
so the daemon wires `Config.Window func(provider, model) (int, string)`
over `catalog.Lookup(provider, id)` + `provStore.Limits()`. The same
function serves the settings derivation and the Test endpoint (one
chain, three consumers).

### D8 — Ollama discovery at refresh
`Client.DiscoverWindows(ctx, p, token, models)`: derive the native root
by stripping a trailing `/v1` from `base_url`; `GET /api/version` (200 ⇒
Ollama; one short request for non-Ollama providers) then
`POST /api/show {"model": id}` per model, taking the single
`model_info.*.context_length` value (suffix match). The whole pass is
bounded by a 10s context and per-model failures leave the window absent.
Called from the server's two discovery paths (`ListProviderModels`,
`RefreshProviderModels`) before `catalog.Set`. *Rejected:* a provider
`type: ollama` — a contract + config change the operator did not ask
for; detection is one cheap request.

### D9 — The limits file and persistence
`~/.zbot/config/providers/model_limits.yaml`:
`models: { "glm-5.3*": 128000, ... }` (longest matching prefix wins;
built-in table first, file entries override). `loadProviderFiles` skips
the reserved stem and `ValidManagedName` rejects `model_limits` as a
provider name; the store loads it in `reloadLocked` (hot-reloaded by the
existing watcher; a broken file keeps the last good table). The catalog
persists `[{id, context_window}]` and decodes legacy string entries.

### D10 — Settings file, endpoint, and test
`~/.zbot/config/intent-analysis.json` (`intent.Settings{Provider, Model,
ReasoningEffort, DeadlineSeconds, MaxAttempts}`; defaults low-for-glm-5.3*
else medium, 45, 3). `GET /api/intent-analysis` returns the settings
plus `effective{model, context_window, window_source, output_reserve,
packet_tokens, trimming, enabled}`; `PUT` validates provider+model like
distillation and partial-updates; `POST /api/intent-analysis/test`
(`{goal?}`, default sample goal) runs `Wrapper.TestAnalysis` through a
`server.IntentTester` seam — a real packet + decision with accounting,
no session, no execution row, no timeline events (an operator probe,
like Test connection). The wrapper reads the card's model per run via
`Config.Settings func() (provider, model string)` so a save applies
without restart.

### D11 — Model precedence
`resolveTarget`: ward orchestrator model (`OrchestratorModel(sid)`, after
`applyOrchestrator`) → card → `spec.Model` → `intent_analysis.model` →
run model. `Analyzer.Provider/Model` (dead config) are removed.

## Risks / Trade-offs

- **`max_tokens = window` vs provider output caps** — mitigated by D5's
  one-retry guard; recorded in the completed event when it fires.
- **Ollama probe on every refresh** — one `GET /api/version` per refresh
  for non-Ollama providers; bounded and cheap.
- **Replay translation** keeps old sessions rendering but means the
  store holds a legacy kind; a future migration could rewrite rows.
- **Empty-session goal runs lose the started event** — only the ad-hoc
  goals endpoint; the research path always has a session.
