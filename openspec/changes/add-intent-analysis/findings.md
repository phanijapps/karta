# Gate Ledger — add-intent-analysis

Phase gates with two independent reviewers (gpt-6.1-terra +
gpt-6.1-sol, read-only zbot-reviewer). Every P0/P1 fixed in-change;
P2s fixed or dispositioned.

## Phase-1 gate — Contract + router + prompts (2026-11-24)

- gpt-6.1-terra: FAIL — 4×P1 + 4×P2 (all fixed or dispositioned).
- gpt-6.1-sol: FAIL — 4×P1 + 1×P2 (converging with terra; all fixed).

| # | Pri | Finding (both unless noted) | Disposition |
|---|-----|-----------------------------|-------------|
| I1 | P1 | Roster-marked existing homes never received the persona seeds → composition would fail on upgrade | FIXED: persona seeds carry their own marker (`.persona-seeded`); roster pair seeds as a unit under its marker; upgrade + lifecycle tests (the fix's first cut had a shared-marker ordering bug — agents' marker write skipped skills — caught by tests before commit) |
| I2 | P1 | Strict decode accepted null/`{}`/partial/trailing JSON as a valid analysis | FIXED: `ParseIntentAnalysis` rejects trailing data and requires primary_intent, complexity, approach, ward action + ward name; malformed responses now take the retry/degrade path instead of influencing execution |
| I3 | P1 | Parseable-but-unusable user templates (missing slots, `{{.Unknown}}`, empty, contract-free prose) were selected and failed the run | FIXED: candidate must parse + carry all four resource slots + render successfully; any failure falls back to the embedded default (five cases tested) |
| I4 | P1 | Blank/whitespace role body produced a heading-only empty prompt | FIXED: blank required role = typed SectionError; blank optional = skip |
| I5 | P2 (terra) | Unreadable optional prompt file indistinguishable from absent | DISPOSITIONED: read/parse failures surface through the registry's existing load-error channel (API/UI observability); composition treats them as absent per the spec's optional-missing scenario — no parallel error path added |
| I6 | P2 (terra) | use_existing ward degradation unreported | FIXED: dropped list gains `ward(use_existing not in index):<name>` |
| I7 | P2 (terra) | Fence extraction could discard a valid response (unrelated fences, same-line fences) | FIXED: brace-matching string-aware scan, fence-agnostic |
| I8 | P2 (sol) | SectionError did not name the file | FIXED: File field (registry path when known, derived `config/prompts/<name>.md` otherwise), asserted in tests |
| I9 | P2 (terra) | Task 1.4 verification gaps (persona lifecycle, seeded composition, ordering, OS portability) | FIXED: TestPersonaSeedUpgradeAndLifecycle, TestSeededPersonaComposes (bootstrap → registry.LoadAll → Compose), explicit-OS + custom-order tests |

Verification after fixes: golangci-lint 0 issues; 19 internal packages
green; task gates:backend OK.

## Phase-3 gate — Middleware + engine plumbing (2026-11-24)

- gpt-6.1-terra: FAIL — 7×P1 + 1×P2 (all fixed in commit 8b091aa).
- gpt-6.1-sol: FAIL — 9×P1 + 2×P2 (converging; all fixed or
  dispositioned below).

| # | Pri | Finding (both unless noted) | Disposition |
|---|-----|-----------------------------|-------------|
| I16 | P1 | Analysis event neither persisted nor published (channel-only; handler discards it); relay could block | FIXED: Persist sink (store.AppendEvent text+payload + bus.Publish, attempted-durable-first); session id learned from the run's first event; ctx-safe forwarding; CreateGoal drains the channel daemon-side |
| I17 | P1 (sol F2) | SSE replay/live paths dropped the intent payload (text only) | FIXED: sseEvent carries intent_analysis on BOTH paths (replay unmarshals text+payload with legacy-flat fallback; live maps the payload); TestIntentAnalysisPayloadStreams |
| I18 | P1 (sol F3) | Undrained stream could leak/backpressure the engine | FIXED: daemon-side drain goroutine in CreateGoal (durable path is persist+publish) |
| I19 | P1 | Empty analysis model always degraded (lookup with empty provider fails) | FIXED: per-run resolution — config model → the run's model → typed degrade (wrapper analysisTarget) |
| I20 | P1 | Resource index maintained but never consumed; MCP validation unwired | FIXED: wrapper RecallResources (index recall, listing fallback on cold/empty) + KnownMCPs from the index wired in the daemon |
| I21 | P1 | ## Context recalled memory only (no knowledge graph) | FIXED: one fused recall split BY LANE — graph-lane hits render as knowledge (both chitta domains, one query) |
| I22 | P1 | Chat silently ran persona-less on composition failure | FIXED: persona error FAILS the exchange before the model call; TestChatPersona |
| I23 | P1 | Named root runs lost all middleware sections | FIXED: adapter APPENDS the composed dynamic sections to the agent body (identity preserved); wrapper composes dynamic-only for agent runs; test |
| I24 | P1 (terra) | Scheduler bypassed the middleware | FIXED: scheduler.New receives the wrapped engine |
| I25 | P2 (sol F10) | Verify claims exceeded tests | FIXED: order/persistence/concurrency/persona/agent-run tests added; adapter budget-CLAMP conformance scenario remains open (recorded: 3-line clamp asserted by wrapper budget forwarding; the tier-2 loop-bound scenario follows with the contracttest budget case) |
| I26 | P2 (sol F11) | OnChange ran full chitta sync inline (blocks the watcher) | FIXED: coalescing worker channel; callback only signals; initial signal replaces the boot sync (no seam gap) |

Verification after fixes: full backend suite 24 pkgs green; -race green
on intent/chat/server/engine; golangci-lint 0; CONTRACT_CHANGE=1 task
gates:backend OK.

## Phase-2 gate — Resource index (2026-11-24)

- gpt-6.1-terra: FAIL — 5×P1 + 1×P2 (all fixed below).
- gpt-6.1-sol: FAIL — 4×P1 + P2s converging with terra (all fixed).

| # | Pri | Finding (both unless noted) | Disposition |
|---|-----|-----------------------------|-------------|
| I10 | P1 | History limits above 100 silently clamp to 10 — reconciliation saw one tenth of the index | FIXED: cursor-walked pagination at pageLimit=100; >10-record test (the same trap bit the TEST helper first — tActive now walks too) |
| I11 | P1 | One oversized Remember call (chitta caps batches at 64) failed whole rosters | FIXED: rememberBatched chunks at 64; 80-resource test |
| I12 | P1 | MCP lifecycle broken: Sync retired ALL tools (wantEntries lacked them); IndexTools never reconciled (changed/vanished/empty probes) | FIXED: Sync reconciles skills+agents ONLY; IndexTools reconciles per server (change→revise, vanish→retire, empty→retire server); interleave tests |
| I13 | P1 | Tombstones re-revised on every sync (not idempotent); removed-then-returned resources never revived (chitta ignores replayed content on superseded records) | FIXED: tombstones skipped in removal loops; REVIVAL revises the tombstone (Sync + IndexTools); idempotency + revival tests |
| I14 | P1 (terra) | Reserved kinds leak: agents could read (recall/history) AND WRITE resource records | FIXED (reads landed mid-review: dropResourceHits/Records + leak test; writes now rejected: rejectResourceKinds + ToolRevise kind check + vet fix) |
| I15 | P2 (both) | Task 2.1/2.2 verify clauses overclaimed (watch/boot wiring is group 3; design said 'deletions remove the record') | FIXED: design records tombstone+revival semantics and agent-invisibility; task 2.1 wording matches the actual guarantees |

Verification after fixes: resourceindex + chitta green (incl. pagination,
batch, lifecycle, revival, invisibility tests); golangci-lint 0; gates OK.
Final reviewer re-verifies the batch (roster-change precedent).

## Final review (2026-11-24)

- zbot-reviewer (default model, whole change): FAIL — 1×P1 + 3×P2,
  then PASS after fixes:
  - F-P1: IndexTools had zero production callers (spec: enabled MCP
    tools SHALL be embedded). FIXED: server seam SetMCPToolIndexer fed
    from every successful non-truncated probe (TestMcp handler);
    daemon wires resIndex.IndexTools bounded (10s, WithoutCancel);
    TestMcpProbeFeedsResourceIndex.
  - F-P2a: task 3.2 verify overclaimed — REWORDED (budget forwarding
    asserted; the tier-2 loop-bound conformance scenario recorded as a
    contracttest follow-up).
  - F-P2b: task 4.2 claimed live SSE — REWORDED (replay proven by
    test; live path carries the same field via the bus event; explicit
    live-SSE test rides with the Timeline UI change).
  - F-P2c: proposal said Started/Complete events, shipped is ONE
    outcome event — proposal amended to the single-event model.
- Everything else clean: five boundaries, all I1-I26 mechanisms
  grep-verified, the three operator refinements implemented, seeds
  intact.
- task verify green after the final edit (exit 0).
