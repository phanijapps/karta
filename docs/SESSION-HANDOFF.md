# Session Handoff — Intent Analysis Enhancement (next slice)

> Written at the end of the 2026-11-24 session. Everything the next
> session needs to start S1 cold. Delete after reading.
> The decision document: `docs/architecture/intent-analysis-enhancement-plan.md`
> (read it first — it is the spec of record for this work).

## Where everything stands

**Zero active OpenSpec changes.** Seven archived today, all
verify-green: add-intent-analysis, relocate-skills-hooks, add-chat-modes,
intent-context-packet, ward-v2-full-session, harden-mcp-and-ux,
add-ward-hosting. Working tree clean at `f371441`.

**Parked in the backlog (operator direction — do NOT start):** Phase 2
channels (Telegram/WhatsApp), the Wails desktop shell, and the recorded
P2s (Leave on wardens, /v1 streaming error messages, per-ward model
niceties, MCP http transports, shell sandboxing). See
`docs/product/backlog.md` → "Parked".

**The next slice: intent-analysis enhancement** — S1 first (below).
Research is DONE (three studies + live-verified sources; the plan's §7).
Do not re-research; implement.

## The operator's decisions (binding — recorded in the plan)

1. **"Separate agent" = separately configured / observed / budgeted
   decision step, NOT a tool loop.** agentzero's intent agent is a
   validated single decision (its tool-loop iteration was reverted).
   Do not build a ReAct analyst.
2. **No token knobs on the Settings card.** Input and output = the
   SELECTED MODEL's own limits, no analyst-side pruning (structured
   output — the validator is the real bound). Resolution: Ollama
   `/api/show` → `<family>.context_length` (verified live: every model
   reports it; NONE reports a separate output cap) → user-editable
   `model_limits.yaml` for cloud providers → **32k fallback**. Analyst
   sends `max_tokens = window`. One safety valve: shrink packet caps
   only if the composed prompt would exceed `window − 4k`.
3. **Pending decisions with recommended defaults** (the operator said
   "Love it" to the example — confirm or accept defaults in the new
   session): agent shape = registry `AgentSpec` + dedicated execution
   row + events (yes); model precedence = the WARD's orchestrator model
   wins when a ward orchestrates (yes); plan steps as Timeline events
   in S2 (yes).

## The flow to build (the example the operator approved)

research session created → **intent-analyst agent** (router pre-check →
host builds the ranked catalog packet → ONE validated decision, ≤2
correction retries → bounds + catalog-id validation → ward
canonicalization → events `analysis_started/completed{tokens, ms,
attempts, mode}`) → **the full v2 packet handed to the orchestrator**
as bounded untrusted-advisory JSON (plan with owners + deliverables,
constraints, expected_outputs, recommended_capabilities, context_items
with reasons, confidence) + `## Plan` + budgeted `## Skills` + MCP
tools MOUNTED from the capability grants → orchestrator runs with
`plan_step_started/done` events → final response.

## The 17 gaps (plan §1) — the ones S1/S2 close

- G1 no token config → model-derived limits + card (S1)
- G2 not a separate agent → `intent-analyst` AgentSpec + execution row
  + token accounting + events (S1)
- G3 thin contract → v2 (plan[], constraints, expected_outputs,
  recommended_capabilities, confidence, context_items) (S2)
- G4 `DisallowUnknownFields` → versioned contract, unknown fields
  logged not fatal (S2)
- G6 **the packet is thrown away** → hand the orchestrator the full
  advisory (S2 — the biggest quality lift)
- G7 two contradicting ward protocols → wrapper instruction wins; edit
  the research-protocol seed (S3)
- G8 skill bodies unbounded → injection budget (S3)
- G9 MCP recommendations inert → mount from capabilities (S3)
- G12 no trackable plan → Plan events (S2)
- G13 no output-mode ladder (Ollama) → S4
- G15 no warm-ward "delegate the whole task" path → S3
- G16 no ward canonicalization → S3

## Key files (read in the new session)

```
docs/architecture/intent-analysis-enhancement-plan.md   # THE plan
docs/product/backlog.md                                # parked list
backend/internal/intent/{analyzer,packet,wrapper,router}.go
backend/internal/domain/intent.go                      # the contract (DisallowUnknownFields!)
backend/internal/sysprompt/sysprompt.go                # Compose + TaskAnalysis (names only today)
backend/internal/config/config.go                      # IntentConfig{enabled,model} only
backend/internal/providers/client.go                   # Model{ID} — no limits cached yet
backend/internal/server/distillation_handlers.go       # THE PATTERN for the settings endpoint
backend/distiller/distiller.go (FileSettings)          # daemon-managed JSON, atomic write
frontend/src/features/settings/advanced/DistillationCard.svelte  # THE PATTERN for the card
backend/cmd/zbotd/main.go                              # the intent.Wrapper wiring (~L228-325)
backend/internal/registry/agents.go                    # AgentSpec — the analyst lives here
~/projects/agentzero/gateway/gateway-execution/src/middleware/intent/  # reference (read-only)
```

## House rules this session learned the hard way

- `task verify` MUST run with the dev stack STOPPED (`pkill -f "air -c";
  pkill -f tmp/air/zbotd; pkill -f vite`) — inotify instance
  exhaustion otherwise. Restart after: `cd backend && setsid nohup env
  CGO_ENABLED=1 ~/go/bin/air -c .air.toml > /tmp/air.log 2>&1 < /dev/null &`
  and `cd frontend && setsid nohup pnpm dev > /tmp/vite.log 2>&1 < /dev/null &`.
  `task` is at `~/go/bin/task` (add to PATH).
- Contract-first ALWAYS; `CONTRACT_CHANGE=1` when the contract changes
  (the gate demands it); regenerate + commit before verify.
- Spec sync when archiving: a MODIFIED delta must KEEP the original
  requirement's scenarios — the merge script replaced whole
  requirements once; audit scenario sets after every sync.
- Budget gate is 30s (recorded reason in scripts/backend-gates.sh).
- Playwright: kill stale Vite/Chromium if suites slow
  (`pkill -9 -f chromium`); `tests/__probe.mjs` is the ad-hoc pattern.
- Reviewers: `zbot-reviewer` (read-only) with `gpt-6.1-terra` (backend)
  + `gpt-6.1-sol` (frontend) as phase gates, then a final pass; fix
  every P0/P1, record P2s in the change's findings.md.

## What to do first

1. Read the plan (§0 framing correction, §1 gaps, §2 flow, §3.4 token
   decision, §4 slices, §5 decisions).
2. Confirm the three defaults (or accept them).
3. `openspec` change `intent-analyst-agent` (S1): contract
   (`GET/PUT /api/intent-analysis`, `analysis_*` event kinds,
   `ModelLimits` on the catalog model), the analyst AgentSpec seed +
   execution row + events, model-limits resolution (Ollama → table →
   32k), the Settings card (picker + read-only derivation + Test
   button). Gates, verify, archive.
4. S2 (`intent-contract-v2`): the v2 contract + packet handover + Plan
   events. S3/S4 follow per the plan.
