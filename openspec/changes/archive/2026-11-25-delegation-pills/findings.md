# Findings — delegation-pills

Presentation-only over data the contract already carries (transcript
rows + the state's executions); no backend or contract change.

## Phase gate (gpt-6.1-sol)

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P1 | `aria-modal="true"` without modal keyboard behaviour (Tab could leave the drawer). | **Fixed**: Tab wraps inside the panel (last → first, Shift+Tab first → last); focus enters on open; Escape/close return focus to the pill. Playwright asserts the wrap. |
| 2 | P1 | The grouping fallback could show another agent's calls (attributed rows with an agent/execution mismatch fell back to the first open group; unknown result ids fell back too; legacy adjacency picked the FIRST open group, not the nearest). | **Fixed**: attributed rows join only on an exact agent (+ execution once bound) match, else are left out; a supplied-but-unknown result id matches nothing; legacy rows (no agent, no execution) take the MOST RECENT open group; root rows never join. |
| 3 | P2 | Copy: "see the sub-agent report card" (cards are gone); "No tool calls — the agent answered directly" while still running. | **Fixed**: "open the agent's delegation pill"; "No tool calls yet." until done. |
| — | Note | Overlapping LEGACY delegations (two intents open at once, unattributed rows) cannot be told apart beyond nearest-open — inherent to pre-attribution transcripts; recorded, not fixable without data. |  |

## Final reviewer pass (zbot-reviewer) — no P0/P1

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P2 | Bookkeeping: unticked tasks, missing findings.md, a test title that matched none. | **Fixed** (this file; tasks aligned). |
| 2 | P2 | `delegationGroups` recomputed per turn on every render. | **Fixed**: derived once per turn inside the `turns` derivation; the slideout looks up by call id. |
