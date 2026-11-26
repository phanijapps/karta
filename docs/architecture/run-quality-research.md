# Run Quality — Research Record

> Evidence: the INR run `research-1795639221673033347-1` (2026-11-25,
> glm-5.3-flash via Ollama cloud as the ward's orchestrator): one root
> run, three delegations, 1.3M prompt tokens, one fabricated report.
> Companion to `run-legibility-plan.md` (surfaces) and
> `intent-analysis-enhancement-plan.md` (the analyst). Two questions:
> why the research agent produced fiction, and why the run cost 1.3M
> prompt tokens.

## 1. What the run did

| Execution | Steps (model calls) | Prompt tokens | Completion | Tools |
|---|---|---|---|---|
| intent-analyst | 1 | 1,911 | 317 | — |
| root | 20 | **721,214** | 36,832 | 57 calls (read ×8, shell ×6, brave ×4, write ×4, ward, memory) |
| research-agent (child) | **1** | **1,102** | **7,961** | **0** |
| builder-agent (child #1) | 9 | 284,089 | 16,814 | 14 (read ×6, shell ×4, write ×4) |
| builder-agent (child #2) | 10 | 298,755 | 38,113 | 20 |

The root's own answer: *"A delegated research agent returned a
detailed-looking report whose deliverables didn't exist on disk and
contradicted verified data (claimed the Fed cut twice and the rupee was
at 86.5 — both false). I discarded it entirely, re-verified every fact
myself."*

## 2. Why the research agent fabricated

### 2.1 The mechanism: prose tool-use, accepted as the answer

The research agent's report opens: *"I'll start by reading the existing
evidence base, the search script, and prior raw results… ## Step 1: Read
existing evidence base and tooling — **Read `sources/evidence-inr-usd-2026-11-25.md`:** …"*
and goes on to narrate searches and files (`sources/research-raw-2.json`,
`sources/evidence-attribution-2026-11-25.md`, `sources/articles-attribution.json`)
that were never created. Its execution shows **zero tool calls**. It
had the tools — children get the ward's `read`/`write`/`list`/`shell`
(`wardtools_bridge.go:27-37`) — and the task spelled out the method
(*"read it FIRST"*, *"apps/ddg_research.py (read it first…) Reuse or
adapt it"*). The model **described** the work instead of doing it.

The engine then did what it does for every run: the first completion
without tool calls is the final answer (`adapter.go:389`). A child that
"answers directly" on step 0 is indistinguishable from a child that
finished its work. Nothing nudged it.

The report's "evidence" was not invented from nothing: its prompt was
1,102 tokens (task ≈ 900 + the agent's 1.1 KB body) — no file content
could have been in it — yet it quotes the known facts (88.80 record
low, 86.5 close, the $14B November inflow). Those numbers are in the
ward's canonical page, which rides the `## Active Ward` packet in every
ward-hosted run (`wardtools_bridge.go:195`). The agent **regurgitated
the ward packet as if it had read the evidence base**, then extended it
with fabricated process and deliverables. This is the same failure
agentzero's lessons doc records for its intent agent — "the model
writes prose instead of calling the tool" — here in a delegated child.

### 2.2 What made it likely

- **The model.** glm-5.3-flash (Ollama cloud) is fast and cheap and, on
  this evidence, prone to narrating tool use under a long, imperative
  task text (3.4 KB) with 13+ tool schemas. The intent-analyst research
  (plan §3.7) already noted instruction-tuned models are robust to
  *format* constraints; tool-call reliability is a different axis.
- **The task was research, the tool plane was not.** Children are
  never offered the MCP plane (`adapter.go:352` — `!in.isChild &&
  a.mcpTools != nil`): the research agent had no web search; the only
  route to the web was a Python script through `shell`. The root, by
  contrast, ran `brave_web_search` ×4 itself later — the delegation
  that was supposed to do the research could not search.
- **The agent prompt states a method, not a proof obligation.** The
  research-agent seed says *"Search with the available research tools…
  Preserve source URLs… state meaningful uncertainty"* — nothing forbids
  narrating steps or requires that every cited artifact exist.
- **Nothing checks a child's claims.** The delegate tool folds the
  answer back verbatim; the root verified deliverables on disk by its
  own judgment (and paid steps for it).

## 3. Why the run cost 1.3M prompt tokens

Every model call re-sends the whole prompt. Cost = Σ over steps of
(fixed overhead + accumulated history).

### 3.1 The fixed overhead, paid on every step

| Ingredient (root run) | Size | ≈ tokens/step |
|---|---|---|
| Persona sections (role, soul, os, research-protocol, tools) | 5.2 KB | ~1,300 |
| **`## Skills` — the two provisioned SKILL.md bodies, unbounded (G8)** | **18.8 KB** | **~4,700** |
| `## Active Ward` packet (AGENTS, canonical page 5.4 KB, spec, plan, layout) | ~8 KB | ~2,000 |
| Tool schemas (builtins 2, chitta 7, ward 6, brave 2, delegate 1 ≈ 18 tools) | — | ~3,000–4,000 |
| `## Task Analysis` + `## Context` | ~2 KB | ~500 |
| **Total fixed** | | **≈ 11,500–12,500 tokens per step** |

× 20 root steps ≈ **230–250k tokens** — a third of the root's bill,
before any history. Children pay the same ward packet + tool schemas +
their agent body on every step (~6–8k × 9–10 steps ≈ 60–80k each).

### 3.2 The history, growing with verbatim tool output

- Root: 20 steps, final history ≈ 24k tokens; the large contributors are
  four file reads (3.9–8.4 KB each), the 26 KB delegation report (the
  fabricated one — paid for twice: once to produce, once to read), four
  Brave results (3–4 KB each), three shell outputs (2.5–4 KB). Σ ≈ 290k
  tokens of history (chars/4 underestimates JSON-heavy content; the
  real figure is higher).
- Builder #1: 61 KB of rows over 9 steps — one 34 KB `read` and a 5.8 KB
  read that stays in context for every later step; builder #2 reads the
  same 6.8 KB and 8.4 KB files again (no sharing between children).
- **Children have no compaction.** The root's history is compaction-aware
  (`HistoryWindow`, 128k threshold — never reached, 0 compaction events);
  a child's context is a fresh `childMsgs` list that only grows
  (`adapter.go:318`).
- **Tool results flow back verbatim.** A 34 KB read is 8.5k tokens on
  every subsequent step of that child.

### 3.3 We cannot see it exactly

`messages.prompt_tokens` is NULL on every engine row — the engine
tracks usage only per execution. The table above is reconstruction from
content sizes; the fixed-overhead share is an estimate. The first fix
for a cost problem is measuring it per step.

## 3a. Measured (solar run, 2026-11-26 — first run with per-step usage)

The solar-manufacturing run (`research-1795696099774376404-1`): root 23
steps, builder child 3 steps, research agent unused (the ward's log.md
lesson steered the root to do the research itself).

| | root | builder |
|---|---|---|
| Prompt tokens | 982,769 | 75,115 |
| Step-1 prompt = fixed overhead + task | **13,608** | 9,274 |
| Final context | 78,753 | 33,029 |
| Completion tokens | 45,322 | 23,789 |
| … of which visible output (files, narration) | ~11k | 485 chars + a 5.4 KB script |
| … of which model reasoning (thinking) | ~84k chars ≈ 20k tokens | **60k chars ≈ 18k tokens** |

- Fixed overhead (13.6k × 23) = **313k = 32%** of the root's prompt
  bill: persona, the 12 KB duckduckgo SKILL.md (3k/step, 69k total —
  the root used Brave instead), the ward packet, ~18 tool schemas.
- Context growth was five shell dumps (25–38 KB each; `| tail -40`
  bounded lines, not bytes — single JSON lines): ≈38k tokens re-sent on
  every later step ≈ **350k+ tokens**.
- 80% of completion tokens was reasoning; the builder thought ~18k
  tokens to write 127 lines — nothing sets a reasoning effort for
  mechanical agents on Ollama cloud.

So the levers below are now numbers, not estimates: delegation
(isolate the dumps in a child) ≈ 500k; tool-result caps ≈ 300k; skills
budget ≈ 70k; reasoning effort ≈ 20k on this run.

## 4. Levers (ranked by expected effect / risk)

### For fabrication (Q1 — truthfulness)

1. **A zero-tool-answer guard for delegated children** (engine, small):
   when a child's completion carries no tool calls on its FIRST step,
   the engine sends one bounded nudge — *"You answered without using
   any tools. If the task required reading files, running commands, or
   searching, do that now and report only what you actually did; if you
   could not, say so explicitly."* — and accepts the next answer either
   way. Cost for a genuinely tool-free task: one extra completion. Opt-in
   per agent via a frontmatter field (`require_tool_use: true` on
   research-agent / builder-agent) keeps it precise.
2. **The honesty contract in the agent seeds** (prompt, free): *"Never
   describe a step you did not take. Every claim cites a path you read
   or a command you ran in this run. If you could not access evidence,
   return the gaps — do not infer."* The research-agent seed already
   asks for uncertainty; it needs the anti-narration rule.
3. **Offer the MCP plane to children** (engine, small): a delegated
   research agent must be able to search. Scope it by the agent's
   allowlist (`tools:`) so a builder does not inherit search.
4. **Deliverable receipts on the fold-back** (engine, medium): when a
   task names output paths, the delegate tool result appends a host
   verification line — *"deliverables on disk: sources/evidence-attribution… ✗,
   sources/articles-attribution.json ✗"* — so the root reads the truth
   next to the claim (it did this by hand here; the guard makes it
   automatic).
5. **Stricter: refuse the first tool-free answer** for agents with
   `require_tool_use` — more tokens, more latency, and wrong for
   synthesis tasks. Not recommended by default.

### For cost (Q2 — context budget)

1. **Persist per-step usage** on engine rows (engine, tiny): the
   columns exist; fill them. Makes every other lever measurable.
2. **Tool-result caps in the model-facing history** (compaction/engine,
   medium — S3's G8 family): a result over N KB keeps the head + a
   pointer (*"… 34 KB total; re-read with a range"*); the `read` tool
   gains `offset`/`limit`. Biggest history lever: the 34 KB read alone
   is ~8.5k tokens × every later step of that child.
3. **Skills injection budget** (S3 G8): 18.8 KB of skill text on every
   step — inject the skill's relevant section (or a summary + the full
   body as a readable file) instead of the whole SKILL.md ×2.
4. **Child compaction / history caps** (engine): children get the same
   compaction-aware window the root has, or a hard per-child context
   cap with the same head+pointer discipline.
5. **Smaller tool menu per role**: the root in a ward research run does
   not need 7 chitta tools + builtins on every step (it used
   `memory_remember` once). Mount tools from the analyst's
   `recommended_capabilities` (S3 G9, already planned) — fewer schemas,
   fewer tokens, fewer wrong-tool temptations.
6. **Provider prefix caching**: the history is append-only, so prompts
   are cache-friendly — whether Ollama cloud / z.ai cache prefixes is
   unverified; measure before relying on it.

## 5. Slices

| Slice | Covers | Size |
|---|---|---|
| **Q1 — Truthfulness guard**: `require_tool_use` + the first-answer nudge; the honesty clause in the research/builder seeds; MCP tools for children by allowlist; deliverable receipts on fold-back | §2, levers 1–4 | M |
| **Q2a — Measure**: per-step usage on engine rows; a cost line on the delegation card and the session footer per step | §3.3 | S |
| **Q2b — Context budget** (= S3's G8/G9): tool-result caps + ranged reads; skills injection budget; role-scoped tool menus; child compaction | §3.1–3.2 | L |

Q1 first: it stops the run from producing fiction. Q2a is a
prerequisite for tuning Q2b. Interleaves with S2 (contract v2) — the
analyst's `recommended_capabilities` feeds lever 5.

## 6. Decisions (operator, 2026-11-25)

1. **Tool plane: sub-agents get the same tools as the root** — the
   MCP plane included (not allowlist-scoped beyond the agent's own
   `tools:`); which MCP to use is the model's judgement, guided by the
   analyst's recommendations and the agent prompts.
2. **Nested delegation: not today.** Sub-agents spawning sub-agents is
   expected later; until then the nesting count is held by a config
   knob, `delegation.max_depth` (default **1**: the root delegates,
   children never — regardless of any agent's `max_depth` grant). The
   engine's cap stops being a constant.
3. The first-answer nudge ships **opt-in per agent** (`require_tool_use:
   true` on the research and builder seeds).
4. The honesty clause ships in the seeds and upgrades **unmodified
   copies only** (the persona-v2 pattern).
5. Tool-result cap size: still open (Q2b).

Shipped as `delegation-truthfulness` (Q1 + Q2a + the depth knob).

## 7. Sources (repository evidence)

- `backend/internal/engine/eino/adapter.go:318` (child context grows
  unbounded), `:352` (MCP root-only), `:389` (first tool-free answer =
  final)
- `backend/internal/engine/eino/wardtools_bridge.go:27-37` (children's
  ward tools), `:195` (the Active Ward packet)
- `backend/internal/config/seedassets/agents/research-agent.example.md`
  (the method contract without a proof obligation)
- `backend/internal/sysprompt/sysprompt.go:151` (`## Skills` bodies,
  unbounded); `~/.zbot/skills/*/SKILL.md` sizes
- Run data: `executions`, `messages` for `research-1795639221673033347-1`
- agentzero lessons 2026-09-08 (prose instead of tool calls) —
  `intent-analysis-enhancement-plan.md` §0
