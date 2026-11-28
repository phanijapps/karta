# Session Handoff — Karta, 2026-11-27 (next: soak, measure, then S2 + routing)

> Written at the end of the 2026-11-26/27 session. Everything the next
> session needs to continue cold. Delete after reading. Decision records:
> `docs/architecture/run-quality-research.md` (§8 = what landed for Q2b),
> `docs/architecture/intent-analysis-enhancement-plan.md` (the analyst, S1
> done; S2–S4 open), `run-legibility-plan.md` (shipped). The archived
> change: `openspec/changes/archive/2026-11-27-engine-adk-spine/` (proposal,
> design with decisions D1–D9, findings G1–G8 + the measurements, signoff.md,
> decisions.html).

## 1. Where everything stands

**The engine runs on the Eino ADK spine** (`engine_loop: adk` is the
default; `engine_loop: legacy` keeps the hand-rolled loop selectable for
one soak cycle, after which a follow-up change deletes it). Five commits
on `feature/provider-plane-and-model-catalog` since the last handoff:
proposal (D1–D9) → slice 0 (the spine swap, revertible) → slice 1 (the
context budget) → slice 2 (delegation on the spine) → live verification.
Every slice gated by `zbot-reviewer` on gpt-6.1-terra (backend) and
gpt-6.1-sol (frontend); findings G1–G8 in the change's findings.md.

What the spine gives a run today (all test-pinned, all observable):
- **Tool-result caps** (`reduction`, 12 KB): head + tail preview + a path;
  original bytes offloaded under a per-run `os.Root`, re-read in byte
  ranges via `karta_read_file`. Delegation fold-backs and skills exempt.
- **Skills on demand** (`karta_skill`): the analyst's recommendations load
  when called; nil analysis = no tool; `tools: []` = no engine extras.
- **In-run compaction**: the NEW kernel policy `internal/compaction`
  behind `domain.Compactor` (the constitution's Compaction Rule), bridged
  by `mw_compaction.go`; one durable `compaction_triggered` event per pass.
- **Reasoning effort** per agent (`reasoning_effort: low|medium|high|none`)
  on every model call; the builder seed declares `low`.
- **Delegation**: one task tool per agent (`delegate_<agent>` {task}), the
  dispatch name still callable as an alias; children run on the spine with
  every lever above; `delegation.max_depth = 1` is still the hold.
- **Live-verified** through the real UI (Playwright, headless Chromium):
  `frontend/tests/_live/karta-live.mjs` — a research run with one
  delegation in 17 s, 64.2k prompt tokens, 0 console errors, every frame
  attributed. Screenshots in the change's signoff.md.

`task verify` is green (10 steps: gates, codegen no drift, lint 0, 29
backend packages under -race, budget, LadybugDB suite, build, 109 e2e).
The dev stack is STOPPED (start: see house rules).

## 2. Operator decisions (binding; D1–D9 in the change's design.md)

1–7 as before (analyst = one validated decision; sub-agents share the root's
tool plane; nested delegation off behind `delegation.max_depth`; the nudge
opt-in per agent; honesty contract upgrades unmodified copies; the review
playbook). New in this change:
8. **D1 per-agent delegation tools** (`delegate_<agent>`), the dispatch name
   as an alias. **D2** legacy loop selectable for one soak cycle, then
   deleted. **D3** skills scoped to the analyst's picks, all when none.
   **D4** a cheap summarizer by default — ships EMPTY (the run's own
   model); SET `compaction.summarizer: ollama/glm-5.3-flash:cloud` in
   config.yaml. **D5** `reasoning_effort` per agent; builder `low`;
   summarizer `none` deferred to the per-model verification. **D6**
   plantask out (S2). **D7–D9** model routing as part of intent = a sibling
   change after S2 (`intent-model-routing`): explicit config wins → the
   analyst assigns (`model_assignments {root, agents{}, reason}`,
   host-validated: known id, tool-capable, reachable) → today's default;
   **Ollama cloud-first** (`providers[].prefer_tier`), local only when the
   run is local or cloud is down; the selected model beats the analyst for
   the root. The deck: `openspec/changes/archive/2026-11-27-engine-adk-spine/decisions.html`.

## 3. Open items (prioritized)

1. **2.5 — Measure** (operator): the per-model `reasoning_effort` check on
   Ollama cloud (docs: ≥ 0.31 honors it on `/v1`; 0.34.2 installed; `none`
   turns thinking off) and the solar re-measurement —
   `cd frontend && GOAL="<the solar goal>" node tests/_live/karta-live.mjs`,
   compare against `run-quality-research.md` §3a using §8's protocol.
2. **Soak, then delete the legacy loop**: after clean runs on `adk`, a
   follow-up change removes `runLoop`, `buildMessages`' legacy half, the
   `engine_loop` knob and the `LoopLegacy` suite fixture; closes F-26 for
   good (the ADK path keys models on provider+model+token; add BaseURL).
3. **Hook runner** (`internal/hooks/` implementing `domain.HookRunner` —
   `backend/AGENTS.md` now says it does not exist): `PreCompact`,
   `PreToolUse`, `PostToolUse` are honored by the seams (compaction policy,
   the eino-engine-adapter skill's plan) but nothing executes hooks.
4. **S2 — `intent-contract-v2`** (plan §3.1/§3.3): v2 fields, versioned
   parsing, bounds in code, the advisory packet, `plan_step_started/done`
   (plantask as the emitter if it fits). Then the routing sibling (D7–D9).
5. Small (handoff §4 carried): receipts regex false positive — seen live as
   `clock.now ✗ not on disk` (require a slash OR a known extension after a
   write verb); the operator's `research-agent.example.md` still has
   `tools: []` (= NO tools) — fix by hand; runs append to the canonical
   page + `log.md` but not `plan.md`/`spec.md`; the duplicate report
   storage; the summarizer's usage is not booked on the execution (G4 F8).

## 4. House rules (learned the hard way, again)

- `task verify` with the dev stack STOPPED — but `pgrep -f "vite.js"`
  matches YOUR OWN shell's command line and kills it (exit 143, this
  session). Use the bracket trick so the pattern cannot match itself:

  ```
  for p in $(pgrep -f "[a]ir -c|[t]mp/air/zbotd|[v]ite.js"); do kill $p; done
  ```

  Restart: `cd backend && setsid nohup env CGO_ENABLED=1 ~/go/bin/air -c
  .air.toml > /tmp/air.log 2>&1 < /dev/null &` and `cd frontend && setsid
  nohup pnpm dev > /tmp/vite.log 2>&1 < /dev/null &`; `task` is
  `~/go/bin/task` (the gates script calls bare `task` — put `~/go/bin` on
  PATH).
- `backend/zbotd` is a TRACKED 40 MB build artifact: never commit a rebuild
  (`git checkout <base> -- backend/zbotd`). `task verify`'s build did not
  touch it this session; `go build ./cmd/...` does.
- **Stage explicitly** — never `git add -A`.
- Contract-first: `CONTRACT_CHANGE=1` after a contract edit; YAML plain
  scalars cannot contain `: `; regenerate + commit before verify.
- Reviewers are read-only and cannot run git: write the diff to
  `/tmp/*.diff`; cite current files for line numbers. Phase gates: terra
  (backend) + sol (frontend); the final pass is the DEFINITION-OF-DONE pass
  — expect it to catch open tasks and stale wording, not just code.
- `openspec archive -y` once printed only the status without archiving —
  run it again if the "archived as" line is missing; then audit the synced
  specs' `### Requirement`/`#### Scenario` headers.
- The eino ADK facts that bit (all in `engine/eino/AGENTS.md`): a tool
  endpoint error FAILS the run (convert to model-facing text); a tool-less
  inventory degrades to a single call (the placeholder keeps the ReAct
  path); a tool-free completion IS terminal (the nudge is a continuation
  run); the compiled node's tools are name slots — execute against the
  live plane; `tools: []` must also exclude engine extras.
- Seeds upgrade only byte-identical copies (now also the builder's
  `reasoning_effort: low`, marker `.agent-seeds-effort`).

## 5. Key files

```
backend/internal/engine/eino/{adkrun, runsetup, runstate, einotool,
  mw_budget, mw_persistence, mw_compaction, mw_usage, contextbudget,
  delegation, runhelpers, messages, run, adapter}.go  # the spine (+ legacy loop)
backend/internal/compaction/compaction.go            # the kernel policy (domain.Compactor)
backend/internal/engine/contracttest/                # the conformance suite (both paths)
backend/internal/config/{context_budget, defaults}.go; backend/cmd/zbotd/main.go (wiring)
frontend/src/features/chat-stream/delegation.ts      # the delegate_ prefix rule
frontend/tests/delegation-tools.spec.ts; frontend/tests/_live/karta-live.mjs
openspec/changes/archive/2026-11-27-engine-adk-spine/{design.md (D1–D9, mapping), findings.md (G1–G8 + measurements), signoff.md}
```
