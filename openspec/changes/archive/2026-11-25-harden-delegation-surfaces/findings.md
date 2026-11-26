# Findings — harden-delegation-surfaces

Defect fixes within the shipped specs (skip_specs). Found by a Playwright
+ axe review of the live INR session (pre-attribution transcript, two
delegations issued in one root step, a report between intent and result).

## Phase gate (gpt-6.1-sol)

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P1 | The tracker closed the OLDEST open delegation on an id-less result while the grouping closed the NEWEST — a mixed id / id-less sequence could strand a turn open forever (final answer suppressed, later user rows absorbed). | **Fixed**: both use newest-open; an unknown id stays a no-op. Playwright "a mixed id / id-less result sequence never strands a turn open" (fails with the old rule). |
| 2 | P2 | The slideout's focus trap ignored the new task `<summary>`. | **Fixed**: native summaries join the Tab cycle; the regression test Tabs Close → summary and expands it with Enter. |

## Final reviewer pass (zbot-reviewer) — no P0/P1

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P2 | Comments said "legacy rows carry no agent"; they arrive as `root` — position is the only signal. | **Fixed** (Chat.svelte comment, design D1). |
| 2 | P2 | Bookkeeping. | **Fixed** (this file; tasks ticked after verify). |
