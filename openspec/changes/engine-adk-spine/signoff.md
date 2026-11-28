# Signoff — engine-adk-spine

**Change:** `engine-adk-spine` — the engine on the Eino ADK middleware spine
behind `domain.AgentEngine`, revertible via `engine_loop`.
**Branch:** `feature/provider-plane-and-model-catalog`. **Date:** 2026-11-27.
**Status:** implementation complete, gated, verified; archive awaits the
operator's signature and two decisions (§6).

## 1. What shipped

| Slice | Shipped | Commit |
|---|---|---|
| Proposal | D1–D9 decided; design mapping; four spec deltas; decision deck | `3f46709` |
| 0 — the spine swap | Root runs as one `adk.ChatModelAgent` under `adk.Runner` (`engine_loop: adk`, legacy default during the slice); middlewares budget / persistence / usage; the driver (terminal states, the nudge as a continuation run); tool adapter + placeholder; `engine_loop` config | `c83b814` |
| 1 — the context budget | Tool-result caps (byte cap, original bytes offloaded under a per-run `os.Root`, `karta_read_file` byte ranges); skills on demand (`karta_skill`, scoped to the analyst, `tools: []` = no extras, `karta_` reserved); in-run compaction through the NEW kernel policy `internal/compaction` behind `domain.Compactor` (thin bridge — the constitution's Compaction Rule); `reasoning_effort` per agent + builder seed `low`; config knobs | `010c8c2` |
| 2 — delegation on the spine | One task tool per agent (`delegate_<agent>` {task}, D1) sharing `delegateTool.Exec`; the dispatch name as an alias; colliding/unsafe names reported + alias-served; every delegation tool cap-exempt; children run on the spine (`runChild` → `runAgent`); `engine_loop` default → `adk`; UI recognizes both shapes by prefix; e2e spec | `7889934` |
| Live verification | Playwright on the real stack: the per-agent tool, a child on the spine, attributed frames, 0 console errors (delegation path; the other levers are test-pinned, not yet measured live); the live-run script for measurement runs | `8753ba5` |
| Final gate fixes | Child-compaction test; verbatim-only tool names; mid-run drop reports; docs | (this commit) |

## 2. Decision compliance (design.md "Decisions")

D1 per-agent tools ✓ (alias kept) · D2 legacy selectable one soak cycle ✓ ·
D3 skills scoped, all when none ✓ (nil analysis = no tool) · D4 cheap
summarizer: knob shipped, **default empty — set it in config.yaml** · D5
`reasoning_effort` per agent ✓, builder `low` ✓, summarizer `none` deferred
to the per-model check · D6 plantask out ✓ · D7–D9 routing = sibling change
after S2 (recorded; the engine seam — per-child model + `WrapModel` — exists).

## 3. Gate history (zbot-reviewer; read-only; diffs in /tmp)

| Gate | Reviewer | Verdict → outcome |
|---|---|---|
| G1 slice 0 | gpt-6.1-terra | FAIL (4 P1) → fixed with tests |
| G2 slice 0 re-gate | terra | FAIL (2 P1) → fixed; 2 test-found bugs fixed |
| G3 slice 0 third pass | terra | PASS-WITH-FIXES (1 P2 recorded) |
| G4 slice 1 | terra | FAIL (13) → fixed incl. the structural Compaction-Rule finding |
| G5 slice 1 re-gate | terra | FAIL (2 P1) → fixed; R2-1 recorded (no hook runner exists) |
| G6 slice 1 third pass | terra | FAIL (1 P1) → `os.Root` confinement |
| G7 slice 2 | terra (backend) + gpt-6.1-sol (frontend) | FAIL (3 P1) → fixed |
| G8 final DoD pass | terra | NOT READY → F2 fixed with a test; F4–F6 fixed; F1/F3 = operator decisions (§6) |

All P0/P1 are fixed or are the two decisions in §6; every P2 is fixed or
recorded with its reason (findings.md).

## 4. Test outcome evidence

`task verify` with the dev stack stopped, run on the FINAL tree after the
G8 fixes — `/tmp/task-verify-final.log` (an earlier run on the pre-fix tree
is `/tmp/task-verify.log`, exit 0 in 126 s):

```
task verify (final tree) exit=0 in 140s at 2026-11-28T01:13:13Z
task: [gates:backend] backend gates: OK
task: [codegen]        (no drift — regenerate-and-compare silent)
task: [lint] [lint:backend]   0 issues.
task: [test]           29 packages ok under -race (incl. internal/compaction, internal/engine/eino)
task: [budget:backend] budget gates: OK
task: [test:chitta:ladybug]  ok  	zbot/backend/chitta	40.814s   (real LadybugDB)
task: [build]          ✓ built
task: [check:frontend] svelte-check found 0 errors
task: [test:frontend]  109 passed (39.7s)   incl. tests/delegation-tools.spec.ts
```

Conformance: `TestContractSuite` (legacy, explicit) and `TestContractSuiteADK`
(the default) both green; every legacy-era adapter test now runs on the
spine. Spine-specific pins: 30 tests in `adkrun_test.go` /
`contextbudget_test.go` (caps + byte ranges + confinement, skills, the
compaction bridge incl. a CHILD pass, effort, per-agent tools, alias,
collisions, fold-back never capped, hallucinated delegation refused, live
inventory, cancel-mid-call, step bound, stuck kill, nudge continuation).

## 5. Live evidence (real daemon + UI, Playwright headless Chromium, 2026-11-27)

Run: session `research-1795827332272744706-1`, model `zai/glm-5.3-flash`,
goal "Delegate to the builder agent the task: report the current UTC time
using the clock tool …" — **run completed in 17 s, 0 console errors.**
Transcript: root `ward_use` → `local-llm-tools` → root intent
**`delegate_builder-agent`** → child rows under `exec-child-builder-agent-…`
(`clock.now` → `2026-11-28T00:55:46Z` → answer) → fold-back with the
host's deliverables check → root answer. Executions: root 40.8k in / 692
out; analyst 1.9k / 217; child 21.5k / 21. Every SSE frame carried
`agent` + `execution_id`. Report: `/tmp/karta-live-report.json`.

| Screenshot | Shows |
|---|---|
| `/tmp/karta-live-thread.png` | the thread: analysis chip (S, ward), the `builder-agent` pill, THE ANSWER with Thinking + Narration; Work tab: ward entered, "Delegated builder-agent", the analysis card with per-execution usage, the DELEGATIONS card, the session footer (64.2k prompt · 930 completion · 1 delegation · 17 s) |
| `/tmp/karta-live-slideout.png` | the slideout: `builder-agent · completed · 21.5k → 21 tokens · 3 s`, the child's call, the report with the deliverables check |
| `/tmp/karta-signoff-1-thread-work.png` | final thread + Work tab |
| `/tmp/karta-signoff-2-slideout.png` | final slideout |
| `/tmp/karta-signoff-3-context.png` | Context tab: active ward, provisioned skills, AGENTS → builder-agent (from the per-agent call) |
| `/tmp/karta-signoff-4-settings-agents.png` | Settings → Agents: the roster the spine offers delegation tools for |

Observed, pre-existing and left for its own item (handoff §4.4): the
receipts regex flags `clock.now` as a deliverable ("✗ not on disk").

## 6. Operator decisions needed before archive

1. **Solar re-measurement (task 2.5).** The proposal said "recorded in the
   change before archive". It is a ~1M-token research run on your account.
   Options: (a) archive now — the levers are TEST-PINNED and the delegation
   path is LIVE-VERIFIED (caps, skills, compaction and effort savings are
   not yet demonstrated on a live run), the measurement protocol is
   recorded (`run-quality-research.md` §8) and gates the LEGACY DELETION,
   not this archive; (b) hold the archive until
   you run `GOAL="<the solar goal>" node tests/_live/karta-live.mjs` and the
   numbers are recorded here.
2. **G5 R2-1 — no `domain.HookRunner` exists in the daemon** (pre-existing,
   change-independent; `backend/AGENTS.md` now says so). Proposed:
   reclassify as out of scope, follow-up "hook-runner" (handoff §3.3).
3. **D4 in your config.yaml:** `compaction.summarizer: ollama/glm-5.3-flash:cloud`
   (ships empty = the run's own model).

## 7. Residual risks

Summarizer token usage is not booked on the execution (G4 F8). Reasoning-
effort support and savings are model-specific and unmeasured (2.5). Hook
execution awaits the hook-runner follow-up. The legacy loop stays
selectable for one soak cycle, then a follow-up deletes it (and closes
F-26 for good).

## 8. Signoff

Implementer: pi (coding agent) — 2026-11-27. Final gate: zbot-reviewer on
gpt-6.1-terra (G8) — NOT READY → fixed except the two operator decisions.

Operator: ______________________  Date: __________

- [x] Decision 1 (measurement): operator "run it" → the solar goal ran on
    the spine (findings.md "Solar re-measurement"): 9 steps / 255,920 prompt
    / 15,754 completion / 2 m 45 s — a VERIFICATION run (the ward held the
    previous deliverables), so the total is not comparable to §3a; the
    comparable parts: step-1 overhead −10.6%, the cap demonstrated exactly
    on a 24.7 KB read and in the worked example (91 KB → +3.1k). Recorded;
    the from-scratch variant (a fresh topic in a new ward) remains
    available if wanted before the legacy deletion.
- [x] Decision 2 (hook runner): out of scope, follow-up "hook-runner"
    (operator, 2026-11-27: "I know 2 hasn't been implemented")
- [x] Decision 3 (D4): `compaction.summarizer: ollama/glm-5.3-flash:cloud`
    set in config.yaml (operator: "I am fine with 3"); daemon reloaded
- [ ] Archive approved → `openspec archive -y engine-adk-spine` + scenario audit
