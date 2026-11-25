# Session Handoff — Intent Analysis Enhancement (S2 next)

> Written at the end of the 2026-11-25 session (S1 shipped). Everything
> the next session needs to start S2 cold. Delete after reading.
> Decision document: `docs/architecture/intent-analysis-enhancement-plan.md`
> (the spec of record — read §0, §3.1, §3.3, §4 first).

## Where everything stands

**Zero active OpenSpec changes.** Archived today, verify-green:
`intent-analyst-agent` (S1) — its `findings.md` is the review ledger
(one disputed P1 recorded: the plan's 20/20/20/64 packet caps stay).

**S1 shipped (committed, live-checked against the dev daemon):**
- The **intent-analyst** is a real registry agent
  (`~/.zbot/agents/intent-analyst.md`, seeded under
  `.intent-analyst-seeded`; `registry.IsSystemAgent` → never delegable,
  never in its own packet) making ONE validated decision per research run
  on a dedicated execution row (`exec-intent-*`, tokens rolled in) with
  the trail `analysis_started` → `analysis_completed|bypassed|failed`
  (legacy `intent_analysis` rows translate on replay).
- **Limits = the model's own**: Ollama `/api/show` `*.context_length` at
  refresh → `config/providers/model_limits.yaml` (built-in `glm-5.3*`
  128k) → 32k fallback (`providers.ResolveWindow`, one chain); the
  analyst sends `max_tokens = window`; the ONE valve shrinks packet caps
  only past `window − 4k`; a provider 400 on the full window retries once
  without `max_tokens` (design D5 — the operator may veto).
- Model precedence: ward orchestrator → Settings card
  (`config/intent-analysis.json`) → agent file → config.yaml → run model
  → named agent's model.
- Settings → Advanced → **Intent Analysis** card (`GET/PUT
  /api/intent-analysis`, `POST /api/intent-analysis/test`): picker with
  windows, read-only derivation, Test analysis.
- The wrapper is ALWAYS installed; the kill-switch only disables
  analysis.

**Parked (operator direction — do NOT start):** Phase 2 channels, the
Wails shell, the recorded P2s in `docs/product/backlog.md`.

## Two plans on the table (read both, pick the next slice)

**Shipped since the last handoff:** `intent-analyst-agent` (S1),
`intent-analysis-detail`, `intent-analysis-test-detail`, and
`run-legibility` (L1+L2+L3) — all archived 2026-11-25, verify-green.
Next candidates: **S2** (contract v2 + packet handover + Plan events —
the mockup's Plan row), then L2's follow-ups/S3.

- `docs/architecture/run-legibility-plan.md` — the INR run review
  (2026-11-25): delegation is invisible on the wire (SSE drops `agent`,
  durable rows lose it, ChatMessage has no agent/exec), sub-agent
  reports render as answers, the 26 KB report is stored twice, reasoning
  is never persisted, no delegation/plan/cost events. Slices L1
  (attribution), L2 (reasoning + roles), L3 (delegation + cost events).
  Recommended order: **L1 → S2 → L2/L3 → S3**; G9 is reframed as MCP
  *scoping* (tools are already mounted on root runs).
- `intent-analysis-enhancement-plan.md` S2 — below.

## S2 — `intent-contract-v2` (plan §3.1 + §3.3, G3–G6, G12)

Contract-first and ADDITIVE: `IntentAnalysisPayload` gains `plan[]`
(step, owner root|agent|ward:<name>, deliverable, depends_on[]),
`constraints[]`, `expected_outputs[]`, `recommended_capabilities[]`,
`confidence` + `clarification_question`, `context_items[]`, and
`solution_path` becomes a list; parse moves off `DisallowUnknownFields`
to a versioned contract (unknown future fields logged, never fatal — G4);
bounds enforced in code (G5). Then the biggest quality lift: hand the
orchestrator the **full advisory packet** as bounded untrusted JSON in
`## Task Analysis` (G6) + `## Plan` with `plan_step_started/done` events
(G12 — the operator approved Timeline events). Design notes to carry:
the run channel's forwarding (P2 #7 in the ledger) is the natural place
for the plan events; the `IntentAnalysisPayload` status enum already
carries `started`.

## Key files (read in the new session)

```
docs/architecture/intent-analysis-enhancement-plan.md   # THE plan (§3.1, §3.3)
openspec/specs/intent-analysis/spec.md                  # living spec (S1 synced)
openspec/changes/archive/2026-11-25-intent-analyst-agent/{design,findings}.md
backend/internal/domain/intent.go                       # the contract (DisallowUnknownFields still)
backend/internal/domain/event.go                       # kinds + payload (accounting fields)
backend/internal/intent/{analyzer,caps,analyst,wrapper,analysis,packet}.go
backend/internal/sysprompt/sysprompt.go                 # TaskAnalysis renders NAMES only (G6 to fix)
backend/internal/providers/{ollama,limits}.go           # the window chain
frontend/src/features/settings/advanced/IntentAnalysisCard.svelte
frontend/src/entities/timeline-event/analysisText.ts    # the trail's row formatter
frontend/src/features/chat-stream/Chat.svelte           # stream opens at session open; pending flag
```

## House rules (unchanged, learned again this session)

- `task verify` with the dev stack STOPPED (`pkill -f "air -c"; pkill -f
  tmp/air/zbotd; pkill -f "node.*vite"` — a bare `pkill -f vite` kills
  your own shell). Restart: `cd backend && setsid nohup env CGO_ENABLED=1
  ~/go/bin/air -c .air.toml > /tmp/air.log 2>&1 < /dev/null &` and `cd
  frontend && setsid nohup pnpm dev > /tmp/vite.log 2>&1 < /dev/null &`.
  `task` is at `~/go/bin/task`. `backend/zbotd` is a TRACKED 40 MB build
  artifact — `go build ./cmd/...` rewrites it; never commit the change
  (`git checkout <base> -- backend/zbotd` before committing).
- Contract-first; `CONTRACT_CHANGE=1` on every task run after a contract
  edit; YAML plain scalars cannot contain `: ` (use `>-`).
- Phase gates: `zbot-reviewer` with `gpt-6.1-terra` (backend) +
  `gpt-6.1-sol` (frontend) — they are read-only: write the diff to
  `/tmp/*.diff` for them; then the final `zbot-reviewer` pass; fix every
  P0/P1, fix or record P2s in `findings.md`.
- Spec sync at archive: snapshot `### Requirement`/`#### Scenario`
  headers before, `comm` after — MODIFIED deltas must keep every
  original scenario (this archive lost nothing).
- Playwright SSE fixtures: the research stream opens at session open;
  fixtures must deliver PAST events before the send and the run's events
  after, or a replayed old terminal event masquerades as the new one.
