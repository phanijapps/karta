# Findings — intent-analysis-detail

## Phase gates (gpt-6.1-terra backend, gpt-6.1-sol frontend)

No P0/P1. Five P2s:

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P2 | The Test probe discarded `Sanitize`'s drops — an unknown MCP vanished without being named. | **Fixed**: `res.Analysis.Dropped = drops`; the test asserts both the event and the probe name the drop. |
| 2 | P2 | The new wire limits (≤12 hidden intents ×512, strings ≤2048/1024/64) were declared but not enforced on analyst output. | **Fixed**: `Sanitize` clamps the advisory texts to the contract's bounds (trimmed, never a failed decision); `TestSanitizeClampsAdvisoryTexts`. |
| 3 | P2 | Keyed `{#each}` on model-produced strings could throw on a duplicate. | **Fixed**: unkeyed loops (as the pre-existing chips were). |
| 4 | P2 | The MCP "advisory until mounted" qualification was tooltip-only. | **Fixed**: a visible note beside the MCP chips. |
| 5 | P2 | Legacy payloads with skills/dropped now render under a "Resources" heading, and `recommended_agents` becomes visible. | **Intentional, recorded**: the same chips render, grouped under a heading; showing recommended agents is the point of this change (they were analyzed but never surfaced). |

## Final reviewer pass

Pending.
