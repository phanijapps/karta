# Session Handoff — Karta, 2026-11-26 (next: the ADK middleware spine)

> Written at the end of the 2026-11-25/26 session. Everything the next
> session needs to continue cold. Delete after reading. Decision records:
> `docs/architecture/intent-analysis-enhancement-plan.md` (the analyst, S1
> done; S2–S4 open), `run-legibility-plan.md` (shipped), and
> `run-quality-research.md` (measured: fabrication + token anatomy).

## 1. Where everything stands

**Zero active OpenSpec changes.** Eight archived since the last
handoff, all verify-green and pushed
(`origin/feature/provider-plane-and-model-catalog` at the handoff commit):

| Change | What it shipped |
|---|---|
| `intent-analyst-agent` (S1) | the analyst as a seeded registry agent; execution row + `analysis_*` events; model-derived limits (Ollama `/api/show` → `model_limits.yaml` → 32k); `max_tokens = window`; one valve at window−4k; Settings → Advanced → Intent Analysis card + Test |
| `intent-analysis-detail` | the analyst's full v1 decision on the event/state/probe (hidden intents, solution path, explanation, ward reason) |
| `intent-analysis-test-detail` | the Test result shows the whole decision + raw JSON |
| `run-legibility` | `agent` + `execution_id` on every row/frame; report cards; Thinking/Narration; `delegation_started/completed`; session state executions; the attributed Work tab + session footer |
| `delegation-pills` | delegation pills (from `delegate_agent` intents — legacy sessions too) + the slideout (calls, cost, report) |
| `harden-delegation-surfaces` | open-delegation turn tracking; task gists; focusable panels (axe clean) |
| `delegation-truthfulness` (Q1+Q2a) | children share the root's tool plane (MCP included); `require_tool_use` first-answer nudge; deliverable receipts; per-step usage on rows; `delegation.max_depth` knob (default 1 = the hold on nesting); honesty-contract seeds + unmodified-copy upgrade |

Dev stack: `task dev` stack is running (air on :8787, vite on :9245).
`task` is `~/go/bin/task`. Tree clean at the handoff commit.

## 2. Operator decisions (binding)

1. Analyst = a **validated single decision**, never a tool loop; **no
   token knobs**; limits = the model's own window; ward orchestrator
   model wins; Plan events in S2.
2. **Sub-agents get the same tool plane as the root** (MCP included;
   the agent's `tools:` allowlist still applies); which MCP to use is
   the model's judgement.
3. **Nested delegation is OFF behind `delegation.max_depth`** (config,
   default 1: root delegates, children never — regardless of any
   agent's `max_depth`). Raise to 2 when nesting is wanted; nothing else
   enables it.
4. The first-answer nudge is **opt-in per agent** (`require_tool_use:
   true` — research/builder seeds declare it).
5. The honesty contract upgraded **unmodified seed copies only**.
6. Approved UI choices: report pills + slideout (not cards), per-step
   collapsed Thinking, "the root's last assistant row is THE answer"
   (research sessions only), the duplicate report stays stored and is
   hidden behind a reference line.
7. Review playbook: phase gates `zbot-reviewer` + `gpt-6.1-terra`
   (backend) / `gpt-6.1-sol` (frontend), final `zbot-reviewer`, fix all
   P0/P1, P2s fixed or recorded in `findings.md`, `task verify` green
   with the dev stack STOPPED, archive with a scenario audit.

## 3. What the last run taught (measured — `run-quality-research.md` §3a)

The solar-manufacturing run: root 23 steps / **983k prompt tokens** /
45k completion; builder child 3 steps / 75k / 24k; research agent unused.

- **Fixed overhead 13.6k tokens per step** (persona + the 12 KB
  duckduckgo SKILL.md the analyst provisioned + ward packet + ~18 tool
  schemas) → 313k = 32% of the bill.
- **Five shell dumps (25–38 KB each) rode every later step** → ~350k.
  `| tail -40` bounds lines, not bytes; the inline `python3 - <<EOF`
  steps printed 30+ full article texts into context.
- **80% of completion tokens was model reasoning** (glm-5.3-flash via
  Ollama cloud thinks; nothing sets an effort for mechanical agents).
- The root did the research itself because the ward's `log.md` recorded
  the previous (tool-less) research agent's fabricated report.
- Delegation is the context-isolation mechanism: a working research
  child absorbs the dumps and returns the evidence file.

## 4. Open items (prioritized)

1. **Fix the operator's research agent** — `~/.zbot/agents/research-agent.example.md`
   still has `tools: []`, which the engine reads as **no tools** (that
   is why the INR report was fabricated). Remove the line, add
   `require_tool_use: true`; then add one `log.md` line in the
   `india-analysis` ward ("research-agent has tools + the honesty guard
   now; delegating research is safe"). The builder-agent copy was
   upgraded (unmodified); the research copy was skipped (modified).
2. **Q2b — context budget** (the big lever, numbers in §3):
   tool-result caps by BYTES (head + pointer, ranged re-read), scripts
   that extract facts instead of dumping texts, the skills injection
   budget (G8), role-scoped tool menus (G9 — mount from the analyst's
   `recommended_capabilities`), child compaction, a per-agent reasoning
   effort (Ollama `think` / `reasoning_effort` — verify the openai-compat
   model on Ollama cloud honors it).
3. **S2 — `intent-contract-v2`** (plan §3.1/§3.3): v2 fields, versioned
   parsing (unknown fields logged, not fatal), bounds in code, the
   advisory packet handed to the orchestrator, `plan_step_started/done`
   events (the mockup's Plan row). Then S3 (ward canonicalization,
   warm-ward path, protocol contradiction), S4 (Ollama output-mode
   ladder, adaptive confidence).
4. Small: receipts regex false positive (`matplotlib.dates ✗ not on
   disk` — require a slash OR a known extension after a write verb);
   runs append to the canonical page + `log.md` but not `plan.md`/
   `spec.md` (research-protocol nudge); the duplicate report storage
   (dedupe needs reference expansion); a cancellation lifecycle test
   for `delegation_completed`; `agent` on legacy `analysis_*`/ward rows
   is empty (pre-attribution; new runs carry it).

## 5. The next phase — the engine on the Eino ADK middleware spine

### Facts (verified in this session)

- `backend/go.mod`: `github.com/cloudwego/eino v0.9.23` (eino-ext openai
  model v0.1.13). The module **ships `adk/`** with
  `TypedChatModelAgentMiddleware` (`adk/handler.go`) and
  `adk/middlewares/`: `agentsmd`, `dynamictool`, `filesystem`,
  `patchtoolcalls`, `plantask`, `reduction`, `skill`, `summarization`.
- The backend uses **none of the ADK today**: `internal/engine/eino/adapter.go`
  hand-rolls the ReAct loop (`runLoop`: tool offering, progress tracker
  + nudges, budget/extensions, cancellation, per-step usage),
  `delegation.go` (delegate tool, lifecycle events, receipts),
  `runhelpers.go`, `messages.go` (history window), `wardtools_bridge.go`.
  `AgentMiddleware` (the struct) is deprecated upstream — do not build on
  it; `ChatModelAgentMiddleware` is the interface (hooks return ctx;
  `BeforeModelRewriteState` is the source of truth for `ToolInfos`;
  `AfterAgent` v0.9; run-local storage + `SendEvent`).
- The original adapter design (`.agents/skills/eino-engine-adapter/SKILL.md`)
  intended `adk.Runner`, `deep.New` (DeepAgents), TaskTool, the Skill
  middleware and a compaction middleware — the shipped loop predates that.
- **New workspace skills (untracked until this handoff's commit)**:
  `.agents/skills/eino-agent` (ADK overview + `reference/middleware.md`,
  `chat-model-agent.md`, `deep-agents.md`, `filesystem.md`,
  `human-in-the-loop.md`, `runner-and-events.md`, `agent-as-tool.md`),
  `eino-guide`, `eino-component` (ChatModel/AgenticModel/Tool/callback
  references), `eino-compose`. Docs:
  https://www.cloudwego.io/docs/eino/core_modules/eino_adk/eino_adk_chatmodelagentmiddleware/

### Why now: every open lever is a middleware

| Our need (measured) | ADK hook / built-in |
|---|---|
| Tool-result caps (head + pointer), history trimming | `BeforeModelRewriteState` over `state.Messages` |
| Child compaction + root compaction (`domain.Compactor` bridge) | `summarization` middleware |
| Skills injection budget (12 KB/step today) | `skill` middleware (load on demand) |
| Role-scoped tool menus / 18 schemas per step | `reduction` (tool search, deferred tools via `DeferredToolInfos`) + `BeforeAgent` (tools per role) |
| Per-agent reasoning effort, per-step usage capture | `WrapModel` |
| Receipts, attribution, `tool_call` events, hooks (`domain.HookRunner`) | `WrapInvokableToolCall` / `WrapEnhancedInvokableToolCall` |
| The narration nudge (`require_tool_use`) | `AfterModelRewriteState` (or a model wrapper) |
| Timeline events (`agent_step`, `delegation_*`) | `SendEvent` + the runner's event stream (replace the hand-rolled emit) |
| Delegation (depth-granted, `delegation.max_depth`) | `TaskTool` / agent-as-tool + `BeforeAgent` grant logic |
| Plan events (S2's `plan_step_*`) | `plantask` middleware |
| Ward constitution injection | `agentsmd` (ward AGENTS.md) ; ward file tools stay ours (`filesystem` is not ward-aware) |

### What the custom loop does that MUST survive a migration

Attribution (`agent`/`execution_id` on rows + frames), durable-first
events, the delegation lifecycle + receipts, the depth cap, the
first-answer nudge, the progress tracker + budget/extensions, cancel
propagation to children, per-step usage, the `## Active Ward` packet,
and the shared conformance suite (`internal/engine/contracttest` — every
`domain.AgentEngine` must pass it; it is the safety net for the swap).

### Suggested order (incremental, behind `domain.AgentEngine`)

1. Read the four eino skills + `eino-engine-adapter`; read
   `adk/handler.go` and `adk/middlewares/*` in the module cache
   (`$(go list -m -f '{{.Dir}}' github.com/cloudwego/eino)/adk`).
2. Decide: full `ChatModelAgent`/`adk.Runner` migration vs. adopting the
   built-in middlewares inside the existing loop (the middlewares are
   typed to the ADK state — a hybrid needs the ADK state shape; the
   clean path is the full runner, with our concerns as middlewares).
   Write it as an OpenSpec change (`engine-adk-spine`) with the
   contracttest suite as the gate; keep `adapter.go` behind an
   interface so the swap is revertible.
3. First middlewares (pay immediately): `summarization` (compaction +
   child compaction), `skill` (injection budget), `reduction` (tool
   menus), a `WrapModel` for reasoning effort + usage, a
   `BeforeModelRewriteState` for tool-result caps.
4. Then move delegation to agent-as-tool/TaskTool with the depth cap,
   and the nudge/receipts/attribution into `Wrap*ToolCall`.
5. Measure each step against the solar run's per-step numbers
   (`run-quality-research.md` §3a) — the per-step usage on rows makes
   every saving exact.

Cautions: `ModelContext.Tools` is deprecated (use
`state.ToolInfos`); `compose.ProcessState[*State]` stops working in
v1.0; `AfterAgent` is not called on error termination; `WrapXxxToolCall`
is only called for the interface the tool implements; register custom
run-local types with `schema.RegisterName`.

## 6. Key files

```
docs/architecture/intent-analysis-enhancement-plan.md   # the analyst plan (S2–S4)
docs/architecture/run-quality-research.md               # fabrication + measured token anatomy + decisions
docs/architecture/run-legibility-plan.md (+ -mockup.html)
backend/internal/engine/eino/{adapter,runhelpers,run,delegation,messages,wardtools_bridge}.go
backend/internal/engine/contracttest/                   # the engine conformance suite (the swap's safety net)
backend/internal/intent/{analyzer,caps,analyst,wrapper,analysis,packet,packetbuild}.go
backend/internal/providers/{ollama,limits}.go            # the window chain
backend/internal/config/seedassets.go                    # seeds + upgrades (persona v2, intent analyst, honesty)
.agents/skills/eino-{agent,guide,component,compose,eino-engine-adapter}/
frontend/src/features/chat-stream/{Chat,steps,DelegationSlideout,WorkSteps}.svelte
frontend/src/app/ContextPanel.svelte                     # Work tab: steps, delegation cards, footer
```

## 7. House rules (learned the hard way, again)

- `task verify` with the dev stack STOPPED:
  `for p in $(pgrep -f "air -c|tmp/air/zbotd"; pgrep -f "vite.js"); do kill $p; done`
  (a bare `pkill -f vite` kills your own shell). Restart:
  `cd backend && setsid nohup env CGO_ENABLED=1 ~/go/bin/air -c .air.toml > /tmp/air.log 2>&1 < /dev/null &`
  and `cd frontend && setsid nohup pnpm dev > /tmp/vite.log 2>&1 < /dev/null &`.
- `backend/zbotd` is a TRACKED 40 MB build artifact: `go build ./cmd/...`
  rewrites it — never commit the change (`git checkout <base> -- backend/zbotd`).
- **Stage explicitly.** A blanket `git add -A` swept the operator's
  `soul.md` edit into an unrelated fix commit; it took a rebase to split.
  `git add <paths>` per concern, always.
- Contract-first: `CONTRACT_CHANGE=1` on every task after a contract
  edit; YAML plain scalars cannot contain `: ` (use `>-`); regenerate
  + commit before verify.
- Reviewers are read-only and cannot run git: write the diff to
  `/tmp/*.diff` for them; cite current files for line numbers.
- `openspec archive -y` once printed only the status without archiving
  — run it again if the "archived as" line is missing; then audit the
  synced specs' `### Requirement`/`#### Scenario` headers (a MODIFIED
  delta must keep every original scenario).
- Seeds upgrade only byte-identical copies; the operator's
  `research-agent.example.md` is MODIFIED (older version) — fix it by hand.
- Playwright SSE fixtures: the research stream opens at session open;
  fixtures deliver PAST events before the send and the run's events after.
- The intent analyst never re-researches: the plan is the spec of record.

## 8. Starter prompt (paste into the new session)

```
Read docs/SESSION-HANDOFF.md first — it has everything from the last
session: what shipped, the binding decisions, the measured token
anatomy, the open items, the house rules, and the file map.

Then read docs/architecture/run-quality-research.md (§3a is measured,
not estimated) and skim the four new workspace skills under
.agents/skills/ (eino-agent — especially reference/middleware.md,
chat-model-agent.md, deep-agents.md; eino-guide; eino-component;
eino-compose) plus eino-engine-adapter. The engine currently
hand-rolls its ReAct loop (backend/internal/engine/eino/adapter.go) and
uses none of Eino's ADK; eino v0.9.23 already ships adk/ with
ChatModelAgentMiddleware and the built-in middlewares (skill,
summarization, reduction, plantask, filesystem, dynamictool, agentsmd,
patchtoolcalls). Doc:
https://www.cloudwego.io/docs/eino/core_modules/eino_adk/eino_adk_chatmodelagentmiddleware/

Plan first (do not implement yet): propose the OpenSpec change
`engine-adk-spine` — moving the engine onto the ADK middleware spine
behind domain.AgentEngine, with our proven needs mapped to hooks and
built-ins per the handoff's §5 table (tool-result caps, child/root
compaction, skills injection budget, role-scoped tool menus, per-agent
reasoning effort + usage, receipts/attribution/hooks in tool wrappers,
SendEvent for timeline events, delegation with the depth cap, plan
events), the list of loop behaviors that must survive, and
internal/engine/contracttest as the gate. Give me the mapping, the
incremental order, the risks, and the decisions you need from me.

Accept these defaults unless I say otherwise: full adk.Runner/
ChatModelAgent migration over a hybrid (revertible behind the engine
interface); the first slice is the four middlewares that pay
immediately (summarization, skill, reduction, a WrapModel for
reasoning effort + usage); keep delegation.max_depth = 1 as the hold.

Playbook as before: phase gates with gpt-6.1-terra (backend) +
gpt-6.1-sol (frontend) per task group, final zbot-reviewer, task verify
green (stop the dev stack first), archive with the scenario audit, fix
every P0/P1, record P2s in findings.md. Stage files explicitly — never
`git add -A` (it sweeps my edits into your commits).

Before you start: `git status` should be clean at the handoff commit.
```
