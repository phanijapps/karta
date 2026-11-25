# internal/intent

The pre-execution intent middleware for ROOT goal runs: a deterministic
router (pure greetings bypass), the **intent-analyst agent** — a
registered system agent making one validated decision per run with token
accounting and a started/terminal event trail — and the engine decorator
that injects the Task Analysis context and the complexity step budget.
Chat and child runs are NEVER analyzed. Decision document:
`docs/architecture/intent-analysis-enhancement-plan.md` (S1 =
intent-analyst-agent).

## The intent-analyst agent (analyst.go)

`AnalystAgentName` is the registry's reserved `intent-analyst`
(`registry.IsSystemAgent`): a seeded, user-owned `AgentSpec` (tools: [],
max_depth: 0) that Settings lists, the engine never delegates to, and
the analyst never catalogues for itself. The spec binds two things per
run: the **rubric** (an inline body carrying all four slot markers →
the prompt the spec references → the seeded `intent-analysis` prompt →
the embedded `default_prompt.md`; any broken candidate falls through)
and the **model override**. `Settings` is the daemon-managed
`~/.zbot/config/intent-analysis.json` (provider, model,
reasoning_effort, deadline_seconds, max_attempts) read PER RUN; no
token knobs.

## The decision (analyzer.go)

`Analyzer.Analyze(ctx, goal, packet, provider, model, Limits)` returns a
`Decision` (contract + attempts + tokens + prompt size + trimming
flags) or a typed `AnalysisUnavailable` — never a tool loop. ≤
`MaxAttempts` CORRECTION-FEEDBACK completions inside the deadline
(defaults 3 / 45s from the settings); validation against the packet's
ID sets with a last-parsed fallback; z.ai glm-5.3* runs with
reasoning_effort=low (an explicit setting goes to every model).

**Limits are the model's own** (`Limits{ContextWindow, Source}`,
resolved by the daemon through providers.ResolveWindow: Ollama-discovered
→ limits table → 32k). The request carries `max_tokens = window`. The
composed prompt is measured (~4 chars/token) and the per-kind packet
caps (`DefaultCaps` 20/20/20/64) shrink ONLY when it would exceed
`window − OutputReserve(4k)` — `Caps.Shrink` scales proportionally to
the floors, then the request echo gives way; `Decision.Trimmed` records
it. One robustness exception: a provider 4xx (`ErrRequestRejected`)
retries the SAME attempt once without max_tokens (`MaxTokensDropped`)
— a provider whose output cap is below the window would otherwise lose
analysis entirely. Usage rides the optional `UsageCompleter` seam (the
daemon's llmCaller); plain fakes read as zero.

## The wrapper (wrapper.go + analysis.go)

`RunGoal`: ward orchestrator model (`applyOrchestrator`, returned) →
persona → analysis or bypass → sanitize → composed prompt → engine.
Model precedence (`analysisTarget`) is ONE chain: the ward's model when
a ward orchestrates, else the Settings card → agent spec `model` →
config.yaml `intent_analysis.model` → the run's model. Events go
through `Config.Emit` (the daemon's durable-first path):
`analysis_started` then exactly one terminal kind —
`analysis_completed{contract, tokens, ms, attempts, trimmed}`,
`analysis_bypassed{reason}`, `analysis_failed{reason, attempts}` —
emitted BEFORE the run when the session id is known (an ad-hoc run's
terminal event waits for the engine-assigned id). The dedicated
execution row opens through `Config.BeginAnalysis` (the daemon's
`store.CreateAnalysisExecution`, parented under the root) and closes
through `Config.FinishAnalysis` with the status and token usage.
`TestAnalysis(ctx, goal)` is the Settings card's probe: the same
pipeline, no session/row/events; `PacketEstimate` sizes the default
packet for the derivation line.

## The context packet

`packet.go` builds the analyzer's context: the FULL host roster
(registry skills/agents/MCPs + ward vault, system agents excluded) with
descriptions, ranked by the semantic recall's per-kind hit order,
capped 20/20/20/64 (`DefaultCaps`), with the user request embedded as
escaped advisory data.

## Boundaries

- Decorates `domain.AgentEngine` from the OUTSIDE; never imports the
  engine package, server, client code, or `providers` (the window
  arrives through `Config.Window`; usage through the Completer seams).
- Analysis never executes tools and never browses.

## Key entrypoints

`Router.Bypass`, `(*Analyzer).Analyze`, `(*Wrapper).RunGoal`,
`(*Wrapper).TestAnalysis`, `Packet.Ranked/Capped`, `ReadSettings`. The
living spec is `openspec/specs/intent-analysis/`.
