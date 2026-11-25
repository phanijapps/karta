# Tasks

## 1. Contract and event kinds

- [x] 1.1 Contract: `TimelineEventKind` drops `intent_analysis`, adds
  `analysis_started/completed/bypassed/failed`; `IntentAnalysisPayload`
  gains `status: started`, `model`, `tokens_in`, `tokens_out`,
  `duration_ms`, `attempts`, `mode`, `context_window`, `window_source`,
  `trimmed`; `Model`/`ModelEntry` gain `context_window`; paths
  `/api/intent-analysis` (GET settings+effective / PUT) and
  `/api/intent-analysis/test` (POST). `CONTRACT_CHANGE=1 task codegen`;
  commit. Verify: codegen zero drift.
- [x] 1.2 Domain kinds + payload (`EventAnalysisStarted/Completed/
  Bypassed/Failed`, `IntentStatusStarted`, accounting fields); store
  `LatestEventOfKinds`; server replay translates legacy rows by status;
  `GetSessionState` reads the latest terminal kind. Verify:
  domain/server tests (`TestReplayTranslatesLegacyIntentAnalysis`,
  `TestSessionStateLatestTerminalAnalysis`).

## 2. Model limits (providers + store)

- [x] 2.1 `providers.Model{ID, ContextWindow}`; `Client.DiscoverWindows`
  (native root = base_url minus `/v1`; `/api/version` gate; per-model
  `/api/show` → `model_info.*.context_length`; 10s bound). Verify:
  httptest `TestDiscoverWindowsOllama` + `TestDiscoverWindowsNonOllama`.
- [x] 2.2 Limits table: built-in entries + `model_limits.yaml` (prefix →
  window, longest wins, file overrides); reserved stem skipped by
  `loadProviderFiles`, rejected by `ValidManagedName`; loaded in
  `reloadLocked`; `ResolveWindow` chain + `FallbackContextWindow`.
  Verify: `TestLimitsFileOverridesBuiltIn`, `TestResolveWindowChain`,
  `TestModelLimitsStemReserved`.
- [x] 2.3 Catalog: `Lookup(provider, id)`, windows through `Set/LoadEntry`,
  server discovery paths call `DiscoverWindows` before `Set`; store
  persists `[{id, context_window}]` and decodes legacy string lists.
  Verify: `TestCatalogCarriesWindows`, `TestCatalogPersistsWindowsRoundTrip`,
  `TestCatalogLoadLegacyStringIDs`.
- [ ] 2.4 Docs: `providers/AGENTS.md` (limits file + discovery),
  `store/AGENTS.md` (catalog shape, analysis row). Phase gate
  (gpt-6.1-terra): fix P0/P1. Verify: `go test ./internal/providers/...
  ./internal/store/...` green.

## 3. The intent-analyst agent

- [x] 3.1 `registry.IsSystemAgent` (reserved `intent-analyst`); engine
  `delegateDescription` + `Exec` skip system agents (typed
  `ErrNotDelegatable`). Verify: `TestDelegateSkipsSystemAgents`.
- [x] 3.2 Seed `seedassets/agents/intent-analyst.md` (frontmatter: name,
  description, display_name, model "", tools [], max_depth 0, prompt
  intent-analysis; body = override note) under marker
  `.intent-analyst-seeded` in `seedExamples`. Verify:
  `TestSeedIntentAnalystOnce`, seed parses under the strict loader.
- [ ] 3.3 Docs: `registry/AGENTS.md`, `engine/eino/AGENTS.md`,
  `config/AGENTS.md`. Phase gate (gpt-6.1-terra): fix P0/P1. Verify:
  `go test ./internal/config/... ./internal/registry/...
  ./internal/engine/eino/...` green.

## 4. Analyzer: decision, usage, limits

- [ ] 4.1 `Analyzer.Analyze` returns `Decision{Analysis, Attempts,
  TokensIn/Out, PromptChars, Trimmed, MaxTokensDropped}`; optional
  `UsageCompleter` seam; `Window` field drives `max_tokens` + the valve
  (`Packet.Ranked` with `Caps`, estimate chars/4, budget window−4k,
  floors 2/2/2/4); 400 → retry once without `max_tokens`. Verify:
  `TestAnalyzeSendsMaxTokensWindow`, `TestValveShrinksCapsOnTinyWindow`,
  `TestValveSilentOnNormalWindow`, `TestMaxTokensDroppedOn400`,
  `TestDecisionAccountsUsageAcrossAttempts`.
- [ ] 4.2 Analyst binding (`analyst.go`): `AnalystAgentName`, spec
  lookup, rubric resolution (body-with-slots → spec.Prompt →
  intent-analysis → embedded), `resolveTarget` precedence (ward → card
  → spec → config → run); drop dead `Analyzer.Provider/Model`. Verify:
  `TestRubricResolution`, `TestModelPrecedence*`.

## 5. Wrapper: execution row, events, settings, test

- [ ] 5.1 `store.CreateAnalysisExecution` (EnsureSession root, id
  `exec-intent-<nano>`, agent + delegation = analyst, task = goal);
  wrapper seams `BeginAnalysis/FinishAnalysis`; emit started + terminal
  before the run (empty-session fallback keeps the terminal-after-first-
  event path); `Config.Emit` replaces `Persist`; `Config.Settings`
  read per run. Verify: `TestWrapperEmitsStartedThenTerminal`,
  `TestWrapperBypassEmitsNoRow`, `TestWrapperExecutionRowTokens`.
- [ ] 5.2 `Wrapper.TestAnalysis(ctx, goal)` (real packet + decision,
  accounting, no row/events) and `intent.Settings` +
  `ReadSettings(dir)` with defaults. Verify:
  `TestTestAnalysisReportsCost`, `TestSettingsDefaults`.
- [ ] 5.3 Docs: `intent/AGENTS.md`. Phase gate (gpt-6.1-terra): fix
  P0/P1. Verify: `go test ./internal/intent/...` green.

## 6. Daemon wiring and endpoints

- [ ] 6.1 `cmd/zbotd`: `llmCaller.CompleteUsage`; wire `Window`,
  `Settings`, `BeginAnalysis/FinishAnalysis`, `Emit`; remove the dead
  analysisProvider/analysisModel; `SetIntentTester`. Verify: daemon
  builds; a research send against the dev stack shows started → completed
  with tokens on the Timeline and an `exec-intent-*` row.
- [ ] 6.2 Handlers: `GET/PUT /api/intent-analysis` (effective block via
  `ResolveWindow` + packet estimate; PUT validates provider/model,
  atomic write) and `POST /api/intent-analysis/test`. Verify:
  `TestIntentAnalysisHandlers` (GET defaults, PUT persists + rejects
  unknown provider, test reports cost).

## 7. Frontend

- [ ] 7.1 `client.ts`: `getIntentAnalysis`, `putIntentAnalysis`,
  `testIntentAnalysis` + types; timeline store listens to the four
  kinds; `Chat.svelte` applies live analysis on terminal kinds; Task
  Analysis card shows tokens/ms when present. Verify: `pnpm check`.
- [ ] 7.2 `IntentAnalysisCard.svelte` (picker with windows, derivation
  line, Test analysis result) mounted in `Advanced.svelte`. Verify:
  `pnpm check` + `pnpm build`; a Test against the dev stack renders
  tokens/ms/attempts. Phase gate (gpt-6.1-sol): fix P0/P1.

## 8. Documents

- [ ] 8.1 `docs/product/product-context.md` (the analyst agent, model-
  derived limits, the card), plan §4 slice table (S1 done), delete
  `docs/SESSION-HANDOFF.md` (read-and-delete). Verify: docs mention the
  card and the four kinds; handoff gone.

## 9. Review and hardening

- [ ] 9.1 Run the read-only `zbot-reviewer` subagent over the change
  scope (contract, backend, frontend, the five boundaries); fix every
  P0/P1; fix P2s or record them in `findings.md`. Verify: reviewer
  report has zero open P0/P1.
- [ ] 9.2 Stop the dev stack (`pkill -f "air -c"; pkill -f tmp/air/zbotd;
  pkill -f vite`), then `CONTRACT_CHANGE=1 task verify` green after the
  final edit; restart the stack. Verify: verify exits zero.
