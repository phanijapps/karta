# Findings — intent-analysis-detail

## Phase gates (gpt-6.1-terra backend, gpt-6.1-sol frontend)

No P0/P1. Six P2s:

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P2 | The Test probe discarded `Sanitize`'s drops — an unknown MCP vanished without being named. | **Fixed**: `res.Analysis.Dropped = drops`; the test asserts both the event and the probe name the drop. |
| 2 | P2 | The new wire limits (≤12 hidden intents ×512, strings ≤2048/1024/64) were declared but not enforced on analyst output. | **Fixed**: `Sanitize` clamps the advisory texts to the contract's bounds (trimmed, never a failed decision); `TestSanitizeClampsAdvisoryTexts`. |
| 3 | P2 | Keyed `{#each}` on model-produced strings could throw on a duplicate. | **Fixed**: unkeyed loops (as the pre-existing chips were). |
| 4 | P2 | The MCP "advisory until mounted" qualification was tooltip-only. | **Fixed**: a visible note beside the MCP chips. |
| 5 | P2 | Legacy payloads with skills/dropped now render under a "Resources" heading, and `recommended_agents` becomes visible. | **Intentional, recorded**: the same chips render, grouped under a heading; showing recommended agents is the point of this change (they were analyzed but never surfaced). |
| 6 | P2 | `s[:maxLen]` clamps could split a UTF-8 rune (self-raised during the final pass). | **Fixed**: clamps cut on rune boundaries (`TestClampLenRuneBoundary`). |

## Final reviewer pass (HEAD 2aee7fe → archive commit) — no P0/P1

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P2 | Stale record (the rune fix was not in findings.md). | **Fixed** (this file). |
| 2 | P2 | Session state carried the new fields by construction but no test proved it. | **Fixed**: `TestSessionState` persists and asserts hidden intents / solution path / explanation. |
| 3 | P2 | `ward_archetype` rendering untested. | **Fixed**: the Playwright frame carries it; asserted. |
| 4 | P2 | Stale comments/description ("summary"; "Skills/Agents"; `getSessionState` description). | **Fixed**; codegen regenerated (description-only). |
| 5 | P2 | design D3 named sections differently from the implementation. | **Fixed**. |
| 6 | P2 | `ia` was `Record<string, any>`. | **Fixed**: `Partial<IntentAnalysisPayload>`. |
| 7 | P2 | `clampList` wrote through to the caller's slice. | **Fixed**: copies into a new slice. |

Reviewer (read-only) confirmed the five boundaries and contract-first; `task verify` ran green after the final edit (below).
