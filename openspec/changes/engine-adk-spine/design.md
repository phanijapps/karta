# Design

## Context

The current loop (`adapter.go: runLoop`, ~220 lines) hand-rolls: tool
offering per step (allowlist + ward + MCP + delegate, `tools: []` = explicit
no-tools), progress-aware budget (soft/hard/extensions, `progress.Tracker`),
cancellation checks, final-answer detection (`len(out.ToolCalls) == 0`), the
`require_tool_use` nudge, per-step usage persistence, delegation
(`delegation.go` — recursive `runLoop` per child with fold-back + receipts),
history assembly (`messages.go` — root: store `RootWindowFull`; child: fresh
`childMsgs`), and durable-first event emission (attempt-append → publish →
stream, the F-15 posture).

eino v0.9.23 ships the ADK (`adk/` in the module cache, verified):

- `adk.NewChatModelAgent` + `adk.Runner` — ReAct loop over
  `compose.Graph`; final answer = model response with no tool calls (same
  semantics as ours); `MaxIterations` bounds the loop; `ToolsConfig`
  (incl. `EmitInternalEvents` for sub-agent events); `GenModelInput`
  customizes the system-prompt assembly.
- `ChatModelAgentMiddleware` (`adk/handler.go:139`) — the interface:
  `BeforeAgent` (instruction + tools), `AfterModelRewriteState` /
  `BeforeModelRewriteState` (state.Messages + state.ToolInfos — the source
  of truth), `WrapInvokableToolCall` / `WrapEnhancedInvokableToolCall`
  (per-tool-call wrappers — only called for the interface a tool
  implements), `WrapModel`. `AfterAgent` fires only on success (not on
  error/cancel). `adk.SendEvent` + run-local storage
  (`SetRunLocalValue`, gob-serializable, `schema.RegisterName` for custom
  types).
- Built-ins: `summarization` (in-run history compression; emits its own
  events via `TypedSendEvent`), `skill` (Backend interface = `List` +
  `Get(FrontMatter/Skill{Content, BaseDirectory})`; inline/fork context
  modes), `reduction` (truncation + clearing; Backend = one `Write`
  method), `plantask`, `filesystem`, `agentsmd`, `patchtoolcalls`,
  `dynamictool/toolsearch`.
- `adk.NewAgentTool` — agent-as-tool: tool name = agent Name, description =
  agent Description, single `request` string param, result = the child's
  final answer (fold-back); `EmitInternalEvents: true` surfaces child
  events in the parent's stream.
- eino-ext openai model: `WithReasoningEffort(level)` is an exported
  `model.Option` a `WrapModel` wrapper can append per call.

Operator defaults (accepted): full `adk.Runner`/`ChatModelAgent` migration
over a hybrid; first slice = summarization, skill, reduction, WrapModel;
`delegation.max_depth = 1` stays the hold.

## Goals / Non-Goals

Goals: every run executes on the ADK spine behind `domain.AgentEngine`;
every measured lever from §3a becomes a middleware or wrapper; all
contracttest groups (including delegation) pass on the new path; the swap is
revertible via config; the solar-run numbers are re-measured and recorded.

Non-Goals: `plantask`/plan events (S2 `intent-contract-v2`); model routing
as part of intent (sibling change `intent-model-routing` after S2 — slice 2
only prepares the seam: per-child model resolution in `BeforeAgent` + one
`WrapModel` per child); the `filesystem`
middleware (not ward-aware; our ward tools stay ours); `agentsmd` (the
`## Active Ward` packet stays in the instruction); chat/quick-chat (its lean
plane never mounted these); streaming model output (runs stay per-step
non-streaming); Resume (stays declared not-yet); fixing F-23/F-24/F-26
(run-ownership follow-up owns them — the new path preserves the pinned
characterizations).

## The mapping — proven needs → ADK mechanics

| Current (file:line) | ADK mechanism | Notes |
|---|---|---|
| Tool plane assembly (adapter.go `offered` block; `tools: []` semantics; ward/MCP/allowlist; delegate tool offering) | Assembled once per run at agent construction (`offeredTools` → `ToolsConfig.Tools`) — the `BeforeAgent` equivalent; slice 2 moves the depth grant here unchanged | One assembly per run instead of per step; the allowlist binds execution identically. Unknown names go to the ToolsNode's unknown handler → the typed `ErrToolNotOffered` text, recorded + emitted like the legacy `execOffered` refusal. Delegate offering is grant-gated → depth refusal stays invisible by construction. |
| Root history (messages.go `RootWindowFull`, compaction summary) | Run driver seeds `Runner.Run(ctx, msgs)` from the store; summary rides as the first input message; instruction = system prompt | The store stays the source of history at run START; in-run state is the agent's. Cross-run compaction (durable `domain.Compactor`) is untouched. |
| Child fresh context (adapter.go:318) | Child = its own `ChatModelAgent` (isolated state by construction; task = input) | Same "no inherited transcript" semantics. |
| Step budget / extensions / tracker kill (adapter.go tracker block) | `MaxIterations` = today's absolute ceiling (`hardBudget + 3×softBudget`); the budget middleware (`mw_budget.go`): `BeforeModelRewriteState` appends the transient nudge and strips it in `AfterModelRewriteState` (never persisted — legacy parity), `emit` for extension events, the typed `errStuck` for the stuck kill | Tracker (`progress.Tracker`) is framework-free — reused as-is inside the middleware. |
| Near-ceiling tool suppression (adapter.go `step < steps-1`) | Budget middleware empties `state.ToolInfos` near the ceiling; the tool wrapper refuses any call that still arrives (typed text, recorded) | A TOOL-LESS run (`tools: []`, or an allowlist matching nothing) stays on the ReAct path through `placeholderTool` — never offered, never executed — because the ADK otherwise degrades to a single call where a hallucinated call becomes the "answer" (the contract suite's DepthRefused case). |
| Cancellation (ctx checks; cancel-after-generate) | ctx cancellation + the Runner's cancel safe-points | Terminal states handled in the run driver from the event stream (`AfterAgent` is NOT called on error/cancel). |
| Model call + usage + reasoning effort | `WrapModel` — accumulates usage, appends `openaiext.WithReasoningEffort` per agent | Option is typed to the eino-ext openai client we construct (the only client the engine builds). |
| Final answer detection (adapter.go:389) | The ReAct branch (no tool calls → terminal) — identical semantics | The driver reads the terminal (tool-free) message from the runner's event stream and persists the FINAL row itself (only it knows a tool-free response is THE answer, vs. the nudged non-answer); `AfterAgent` is not used (it never fires on error termination). |
| `require_tool_use` nudge (first tool-free completion, once) | Run-driver re-run: if the run ended with a single tool-free completion and `require_tool_use`, start one continuation run seeded with [task, answer, nudge] | Same row shapes as today (answer row with `ToolCalls "[]"`, nudge as a user message); one extra completion, same as today. Re-run lives under the SAME execution record. |
| Assistant rows (intent/final, reasoning, per-step usage) | Intent rows: the persistence middleware's `AfterModelRewriteState` (runs inside the model call, before the event reaches the driver — durable-first holds); final + nudge rows: the driver | Row identity: same fields, same IDs scheme. |
| Tool rows + `tool_call` events + attribution | `WrapInvokableToolCall` wrapper (persist row, emit event, tracker.Record) | Our `tools.Tool` adapter implements `InvokableTool`, so the hook fires for every tool. |
| Durable-first emit + bus + channel + attribution | Run driver: consumes the Runner's event iterator → AppendEvent → publish → stream (same order, same F-15 posture) | Message ROWS persist in middlewares; EVENT rows persist in the driver/wrappers. Both attempted-durable-first. |
| Delegation (delegate tool, lifecycle events, receipts, fold-back) | Per-agent `adk.NewAgentTool` (D1); our tool decorator creates the child execution, emits `delegation_started/completed`, appends `deliverableReceipts`, folds back | `EmitInternalEvents: true` → child steps surface in the parent's stream (translated with child attribution). Cancel propagates via ctx. |
| Delegation depth cap (`max_depth=1`) | `BeforeAgent` grant logic (unchanged semantics: root delegates, children never) | The hold is not lifted by this change. |
| Ward packet / ward guard on delegate desc | Instruction composition (unchanged this change) + tool description guard on the per-agent tools | Mid-run ward-switch packet staleness is pre-existing; noted as a follow-up (transient-injection middleware). |
| `## Skills` bodies (18.8 KB/step) | `skill` middleware with our Backend over `~/.zbot/skills/**`, scoped to the analyst's recommended skills (D3) | sysprompt stops composing bodies for engine runs (`skills.injection`); the `Skills provided in context:` line stays. Chat keeps bodies. |
| Tool-result caps (head + pointer, ranged re-read) | `reduction` middleware (truncation; Backend = one `Write` bridged to a session-scoped offload dir) + `offset`/`limit` on wardtools read | Placeholder text is the middleware's; our read tool gains range params so the pointer is actionable. |
| Child compaction + root in-run compaction | `summarization` middleware (both roots and children; model = run's own unless `compaction.summarizer`) | Emits its own events (verified `TypedSendEvent`) — the driver maps them to timeline compaction events so rule 4 (no silent work) holds. |
| Role-scoped tool menus (G9) | Phase 1: the existing per-agent allowlist at `BeforeAgent` (already role-scoped); analyst `recommended_capabilities` mounting lands with S2/S3 | `toolsearch` deferred — ~18 tools don't need a search meta-tool yet. |
| Plan events (S2) | Out of scope (`plantask` later, in S2) | — |

## Incremental order

Each slice is independently verified (contracttest + characterization tests
green) and measured (per-step usage rows exist — savings are exact).

Live inventory (slice 0, as built): the compiled ADK node's tools are name
slots only — the model-facing list is re-derived every call and every call
executes against the CURRENT plane (`execOffered` over a fresh inventory,
via the wrapper for compiled names and the unknown-tool handler for the
rest). A tool removed, replaced or added mid-run behaves exactly as on the
legacy loop's next step; the delegate tool re-reads the registry per call
and per invocation.

**Slice 0 — the spine swap (behavior-preserving).** New run path in
`engine/eino` selected by `engine_loop` (default `legacy` until slice 2
completes, then `adk`): the `einotool.go` adapter (`tools.Tool` →
`tool.Invokable`, reusing `paramsOneOfFromJSON`), the `toolplane` middleware
(`BeforeAgent` assembly incl. the legacy delegate tool), the `persistence`
middleware (assistant rows), the `budget` middleware (tracker, nudges, stuck
kill, near-ceiling tool shrink), the `reasoning/usage` model wrapper, the run
driver (seeding, event translation, terminal states, the nudge re-run), and
the legacy loop retained behind the flag. Children still run the legacy loop
(the delegate tool's Exec calls it — both paths coexist in the package).

**Slice 1 — the four pay-now middlewares.** `reduction` (+ ranged reads,
config `compaction.tool_result_cap`), `skill` (+ sysprompt seam + config
`skills.injection`), `summarization` (+ timeline events, config
`compaction.summarizer`), reasoning effort (frontmatter `reasoning_effort`,
Ollama-cloud verification task). Gate: gpt-6.1-terra phase review + an
operator measurement run recorded in the change.

**Slice 1 — as built (after gate G4).** `RunOptions`
(`SetRunOptions`) carries the daemon's knobs; `runOnce` composes the
middlewares in hook order reduction → compaction bridge → persistence →
budget → skill → usage (`contextbudget.go` is the glue). Reduction: byte
cap, head + tail previews + path (Eino's `<persisted-output>` notice),
the ORIGINAL bytes offloaded to a per-run temp dir (0700; writes confined,
no symlink following) and removed with the run; `karta_read_file` serves
BYTE ranges (the solar dumps were single 25–38 KB lines) confined to that
root; `delegate_agent` and `karta_skill` exempt from both phases. Skills:
the `karta_skill` tool over the registry's provisioned skills, scoped to
`GoalRequest.Skills` (the intent wrapper sets it when the analysis ran and
`skills.injection: progressive` on the ADK loop; nil = no tool, empty =
all — decision D3); the tool is foreign to the user's plane, captured once
(`runState.foreign`) and passed through to its own endpoint; `tools: []`
gets no engine extras. In-run compaction is the constitution's thin
bridge (`mw_compaction.go`) over the NEW kernel policy
(`internal/compaction` behind `domain.Compactor`: instruction kept, old
messages summarized by a `Summarizer` bound to the run's model or the cheap
`compaction.summarizer`, previous summaries folded, the tail verbatim and
token-bounded, never starting on a tool result; PreCompact hooks honored);
each pass is one durable `compaction_triggered` event. Effort:
`reasoning_effort` on the agent spec rides every root call via
`WrapModel`; the builder seed declares `low` (once-per-home upgrade of
byte-identical copies). Open for the operator: the Ollama-cloud
`reasoning_effort` verification per pinned model (2.4), the solar
measurement run (2.5), and setting `compaction.summarizer` in config.yaml
(D4 — the shipped default is "the run's own model").

**Slice 2 — delegation on the spine.** Per-agent AgentTools (or dispatch
tool per D1) with the lifecycle/receipts decorator; `EmitInternalEvents`
translation; child runs get the full middleware chain (compaction included);
depth grant in `BeforeAgent`. Gate: contracttest delegation group green on
the ADK path; `engine_loop` default flips to `adk`.

**Slice 3 — soak, measurement, docs, flip.** Re-run the solar scenario;
record per-step fixed overhead / context growth / completion split against
§3a; update `engine/eino/AGENTS.md`, this design's mapping (mark landed),
`run-quality-research.md` addendum; final zbot-reviewer + `task verify`;
archive. The legacy path's deletion is a separate follow-up change.

## Loop behaviors that must survive (the contracttest floor)

Attribution (`agent`/`execution_id` on rows + frames, incl. child→parent
surface as delegation-kind events); durable-first event posture (F-15);
close-exactly-once on done/error/cancel; the delegation lifecycle
(started/completed with cost, stored-status wins); the depth cap hold;
the shared tool plane (children incl. MCP, bounded by allowlist); the
first-answer nudge (once, first completion only, `ToolCalls "[]"` row);
deliverable receipts; the progress tracker + budget/extensions; cancel
propagation to children; per-step usage on rows; the `## Active Ward`
packet; `tools: []` = explicit no-tools inventory; `MaxSteps` clamping;
typed errors (unknown agent, not delegatable, tool not offered); pinned
characterizations F-23/F-24/F-26 (preserved, not fixed here).

## Risks

1. **Behavior parity drift** (highest): the framework's loop differs in
   small ways (tool execution order — set `ExecuteSequentially`; message
   shapes; error text). Mitigation: characterization tests run against BOTH
   paths during the transition; contracttest is the gate; the legacy path
   stays switchable.
2. **Progress nudges on the ADK state** (today: transient per-step). Resolved
   in slice 0 as parity, not a delta: the budget middleware appends the nudge
   before the model call and strips it in `AfterModelRewriteState` — pinned
   by `TestADKProgressNudgeIsTransient` (never persisted, never emitted).
3. **Skills-as-tool changes model behavior**: the model must CALL the
   `skill` tool where today the body is in-context. A model that doesn't
   call it loses the skill (quality risk). Mitigation: the middleware's
   design (names + descriptions in the tool description), the analyst's
   recommendation line stays in the prompt, and the solar re-measurement
   must show the skill actually loading. Revert: `skills.injection: bodies`.
4. **Summarization costs + observability**: the summary is an extra model
   call per compaction; the built-in's events must map to timeline events
   (rule 4). If the built-in's event surface proves thin, wrap or fork it.
5. **The nudge re-run** re-sends the answer as context (paid twice — same
   as today's next-step resend); the re-run must land on the SAME execution
   record or usage/cost attribution breaks.
6. **Delegation tool surface change** (D1): per-agent tools change the
   model-visible contract; seeds/prompts mentioning `delegate_agent` become
   advisory. Zero-churn alternative: keep the single dispatch tool.
7. **`compose.ProcessState` internal use** (v1.0 removal caution): we
   consume only the stable ADK surface (agent, runner, middleware
   interface); pin eino v0.9.x and re-evaluate on v1.0.
8. **LOC budget**: the package gains ~6 new files (each < 500 LOC, gates
   enforce); `adapter.go` shrinks as the loop moves out.

## Decisions (operator, 2026-11-26 — all as recommended)

- **D1 — delegate surface: per-agent task tools** (ADK-native,
  better discoverability, kills the agent-name-typo failure class).
- **D2 — legacy retention: keep `engine_loop: legacy` for one soak cycle**,
  delete in a follow-up change (revert = config flip).
- **D3 — skills scoping: the analyst's recommended set only**, all skills
  when it recommends none. Revert: `skills.injection: bodies`.
- **D4 — summarizer model: a cheap model by default**
  (`compaction.summarizer: ollama/glm-5.3-flash:cloud`); empty = the run's
  own model. Children inherit the global.
- **D5 — reasoning effort per agent**: frontmatter
  `reasoning_effort: low | medium | high | none`, unset = provider
  default. Seeds: builder `low`; summarizer `none`; judgement agents unset.
  Verified: Ollama ≥ 0.31 honors `reasoning_effort` on `/v1` (installed
  0.34.2); eino-ext's option is a string type so `none` rides the same
  `WrapModel` seam. Per-model verification matrix = task 2.4.
- **D6 — `plantask`/plan events stay OUT** (S2 `intent-contract-v2` owns
  them).
- **D7 — model routing, root precedence** (sibling change
  `intent-model-routing` after S2; this change only prepares the seam):
  the model selected for the send beats the analyst's root assignment;
  the analyst routes the root only when none was selected (which also
  makes model-less `/v1/goals` valid).
- **D8 — tier rule for Ollama: cloud-first** (operator-set). The host
  hands the analyst the Ollama catalog pre-sorted cloud → local (ties by
  tier, then id); the rubric picks the best cloud model for the job, local
  only when the run itself is local (explicit selection or pin), the ward
  declares local-only, or cloud is unreachable (the failover list is
  `[assigned, local fallback]`). The tier guard protects the
  explicit-local run from being raised to cloud. Knob:
  `providers[].prefer_tier` (default `cloud`, strict decode).
- **D9 — routing scope: a sibling change after S2** (`IntentAnalysis v2`
  gains `model_assignments {root, agents{}, reason}`; host-validated,
  never fatal).

### Context for D7–D9 (the routing design, in brief)

Routing is one more field in the analyst's single validated decision,
 disposed by the host. Precedence per role: explicit config (the model
selected for the send / the ward's pin for the root; the agent's
frontmatter `model:` for a child) → the analyst's assignment → today's
default (inherit / configured default). Host validation, never a prompt
promise: known id, tool-capable for the role, reachable, tier rule (D8);
an invalid pick falls through with a drop reason on the analysis card
(the wrapper's `drops` list already exists). Two layers of facts feed the
analyst: the derived per-model profile (`/api/show` capabilities — tools /
thinking / vision — parameter size, local-vs-cloud, context window; zero
YAML) and the scorecard from our own rows (narrated-first-answer rate,
error rate, reasoning share, overhead/step; cold start n<3 routes on the
profile alone). The root cannot pick models mid-run in v1 (no `model`
argument on delegation tools). Decided here because slice 2 prepares the
engine seam (per-child model resolution + one `WrapModel` per child).

## Migration and revert

One package (`engine/eino` — the only eino importer, constitution), two run
paths behind `engine_loop` (`legacy` | `adk`), both passing the full
conformance suite during the transition. Config flip = revert. The daemon
wires the flag at engine construction (`cmd/zbotd/main.go` alongside
`SetDepthCap`). After soak: a follow-up change deletes the legacy loop and
the flag (default `adk` becomes the only path).

## Measurement plan

Per-step usage rows exist (delegation-truthfulness). Slice gates record:
step-1 prompt (= fixed overhead) root and child; final context; completion
split (visible vs reasoning); skill tool call count; truncation/offload
count; compaction events. Baseline: solar run §3a (root 13,608 step-1 /
983k prompt / 45k completion, 80% reasoning; builder 9,274 / 75k / 24k).
Target: step-1 fixed ≤ ~6k root (skills 4.7k → ~0.3k, menus trimmed);
prompt total ≤ ~500k on the same scenario (caps + delegation absorbing
dumps); reasoning effort visible on the builder path. The operator runs the
scenario (no live calls in tests); numbers recorded in this change before
archive.
