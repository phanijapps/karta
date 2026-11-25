# Run Legibility — Review and Plan

> Evidence: the INR research run `research-1795639221673033347-1`
> (2026-11-25, after intent-analyst-agent + intent-analysis-detail).
> Operator report: "I didn't see agent delegation, and the final dump is
> put in one place — I don't know which is thought, sub-agent response,
> and final response." Companion to
> `intent-analysis-enhancement-plan.md` (S2–S4 live there).

## 1. What the run actually did

| | |
|---|---|
| Analysis | completed, 1,911 in / 317 out — full contract on the event |
| Root run | 57 tool calls, **3 delegations**, 4 `brave_web_search` (MCP), `memory_remember`, 2.3 KB final answer |
| Delegations | `research-agent` → a 26 KB report · `builder-agent` → charts · `builder-agent` → a brief |
| Tokens | root **721k** prompt / 37k completion; children 284k + 299k prompt — ≈1.3M prompt tokens total |
| Events | 57 `tool_call`, 40 `agent_step`, **0 `delegation`**, 0 reasoning captured |

The run worked. The surface could not show its shape.

## 2. Gap register (verified against the code)

| # | Gap | Evidence |
|---|---|---|
| **R1** | **Delegation is invisible on the wire.** The engine attributes child events on the live bus (`Agent`, `Kind=delegation` for child tool calls) but the durable row is always `kind:"tool_call"` with payload `{tool, output}` and no agent; the SSE frame omits `agent`; the frontend timeline store never parses it. After the first replay — and even live — nothing says "research-agent did this". | `adapter.go:222-234` vs `:436-439`; `sse_events.go:47`; `timeline.svelte.ts:59-70` |
| **R2** | **Sub-agent reports render as answers.** A child's final row is a plain `assistant` row without tool calls; the thread renders every such row as an answer. The thread therefore shows the 26 KB research report → two builder summaries → the root's 2.3 KB answer, styled identically, with nothing marking the last one as THE answer. `ChatMessage` carries no `execution_id`/`agent`, so the UI cannot tell them apart. | messages seq 20/45/90 vs 100; `Chat.svelte:117`; `contracts/openapi.yaml` ChatMessage |
| **R3** | **The report is stored twice.** The child's answer is persisted as its assistant row AND as the `delegate_agent` tool result (same 26 KB): 52 KB of one text in the transcript; the UI shows the first copy as an answer and the second as a folded tool result. | messages seq 20 and 21 |
| **R4** | **"Thought" is narration, not reasoning.** `messages.reasoning` is empty for every row — the engine adapter never writes `out.ReasoningContent` (the lean chat path does). glm-5.3's thinking is discarded; the thread's "latest thinking" is the agent_step narration, which is why thought / sub-agent / final blur: they are all "assistant content". | `adapter.go:405-420`; `store.go:34` |
| **R5** | **A delegation has no events of its own.** No `delegation_started/completed{agent, task, tokens, ms}`; the Work tab nests child steps under the `delegate_agent` step by transcript ORDER (a user row after a delegate intent), not by attribution. | `Chat.svelte:105-108`; `WorkSteps.svelte:67,89` |
| **R6** | **Plan progress is invisible.** The run's structure (analysis → ward → research → build charts → build brief → finalize) can only be inferred from the tool list. (= intent plan G12 — S2's `plan_step_*`.) | — |
| **R7** | **The run's cost is opaque.** The analyst's tokens are on the event now; the root's 721k and the children's 600k live only in the `executions` table. | `executions` rows above |
| **R8** | **MCP recommendations are moot, not inert.** The analyst's `brave-mcp` was dropped (not indexed) — and the root used `brave_web_search` ×4 anyway, because the retrieval plane mounts every configured MCP on root runs. G9 is therefore a SCOPING gap (mount only what the plan needs → smaller tool menu, less context), not a mounting gap. | tool rows seq 53-60 |

## 3. The target flow (what the surface should show)

```
Thread (dialogue only)
  user: "Research the rupee…"
  ┌ 🔬 Task Analysis chip ─ (i) full decision ─────────────────────┐
  ▸ Research · research-agent          [sub-agent report · 26 KB · 1102→7961 tok · 84 s]
  ▸ Build charts · builder-agent       [sub-agent report · 1.9 KB]
  ▸ Write brief · builder-agent        [sub-agent report · 1.5 KB]
  ✦ Final answer (root)  — the ONLY row styled as an answer
        └ thinking ▸ (the model's real reasoning for this step, collapsed)

Work tab (attributed, ordered by events — not transcript order)
  1 Analysis          🔬 started → completed (cost)
  2 Ward              entered india-analysis
  3 Plan              ① pull FX series ② research pressures ③ compare peers ④ build charts ⑤ write brief   ✓✓◐…
  4 Research          👥 research-agent · 1102→7961 tok · 84 s   ▸ 0 tool calls
  5 Build charts      👥 builder-agent · 284k→17k tok · 6 min    ▸ 14 tool calls (read, shell, write)
  6 Write brief       👥 builder-agent · 299k→38k tok · 6 min    ▸ 20 tool calls
  Root tools          🔧 brave_web_search ×4 · read ×8 · shell ×6 · write ×4 · memory_remember
  ─────────────────  run: 1.3M prompt tokens · 3 delegations · 14 min
```

## 4. Enhancements

### 4.1 Attribution on the wire (R1, R2) — contract-first

- SSE frames carry `agent` (contract `TimelineEvent.agent` already
  exists; the daemon drops it) and the store's `execution_id`; durable
  tool rows persist `agent` in the payload and keep `kind:"tool_call"`
  for children too, with the frontend deriving "delegation" from
  `agent !== root` (one kind, attributed — simpler than the live-only
  kind switch).
- `ChatMessage` gains `agent` and `execution_id` (contract, additive):
  the thread can tell a child's row from the root's. Rule: **the root
  execution's last assistant row is THE answer**; every other
  assistant row without tool calls is a **sub-agent report** — a
  collapsed card ("Research · research-agent · 26 KB · 84 s"), never
  the answer style.
- The timeline store keeps `agent`; the Work tab labels every step
  with its agent and groups children by `execution_id` (events), not by
  transcript order.

### 4.2 Roles made distinct (R3, R4)

- The adapter persists `out.ReasoningContent` into `messages.reasoning`
  (the lean chat already does; the column exists). The thread's
  "thinking" toggle shows the model's real reasoning per step;
  narration (the assistant's content between tools) is labelled as the
  step's narration, not as thought.
- Stop storing the child's answer twice: the child's final row stays
  (its transcript is its own), the `delegate_agent` tool result stores a
  REFERENCE (`report:<exec-id>` + the first ~2 KB) instead of the full
  26 KB — the root still gets the full text in its context; the
  transcript does not carry two copies.

### 4.3 Delegation, plan, and cost events (R5, R6, R7)

- `delegation_started{agent, task, exec_id}` / `delegation_completed
  {agent, exec_id, tokens_in, tokens_out, ms, status}` — a delegation
  becomes a first-class Timeline node (constitution rule 4).
- S2's `plan_step_started/done` (intent plan G12) render the Plan row.
- A `run_completed` session event (or the `session` row's state block)
  carries per-execution cost: root + children + analyst → the Work tab
  footer ("1.3M prompt tokens · 3 delegations · 14 min").

### 4.4 Context discipline (R7's cause; feeds S3)

The root's 721k prompt tokens come from ~26 steps at ~27k context each
— 34 KB file reads and 26 KB tool results flowing back verbatim.
S3's skill-injection budget (G8) and a tool-result cap (first N KB +
"stored at <path>") are the levers; the 26 KB report reference (4.2)
is the first one.

## 5. Slices

| Slice | Covers | Size | Why this order |
|---|---|---|---|
| **L1 — Attribution** | R1, R2, R3 | M | Contract (SSE `agent`/`execution_id`, ChatMessage `agent`/`execution_id`), adapter + SSE + store, thread cards, Work tab labels. The single biggest legibility gain; no model change. |
| **L2 — Reasoning + roles** | R4 | S | Adapter persists reasoning; the thread's thinking toggle; narration relabelled. |
| **S2 — Contract v2 + packet handover + Plan events** | G3–G6, G12 (intent plan) | L | Unchanged; its Plan events feed the Work tab's Plan row. Do L1 first so Plan rows land on an attributed stream. |
| **L3 — Delegation + cost events** | R5, R7 | S | `delegation_*` events, the run cost footer. |
| **S3 — Orchestrator wiring** | G7–G11, G15, G16 — with **G9 reframed**: MCP *scoping* (mount only recommended tools), not mounting | M | After S2. |

## 6. Decisions to confirm before L1

1. **Sub-agent reports in the thread**: collapsed card in the thread
   (my recommendation — the operator sees that a delegation happened
   without reading 26 KB) vs. Work-tab only (cleaner thread, easier to
   miss).
2. **Reasoning visibility**: per-step "thinking" toggle, collapsed by
   default (recommendation) vs. a live thinking stream like the chat.
3. **The answer rule**: "the root execution's last assistant row" vs.
   an explicit `final` flag on the row. I recommend the rule (no new
   field; it is already how the research path decides completion).
4. **The duplicate report**: a reference + 2 KB head in the tool result
   (recommendation) vs. keeping the full copy and hiding it in the UI.

## 7. What this deliberately does not do

No change to the engine's delegation semantics, budgets, or the
progress tracker; no streaming of research runs (the exchange completes
as a unit — a later slice could stream the root's narration, which L2's
reasoning capture makes worth doing); no transcript compaction here
(G8 is S3).
