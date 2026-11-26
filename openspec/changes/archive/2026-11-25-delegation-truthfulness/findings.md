# Findings — delegation-truthfulness

## Phase gate (gpt-6.1-terra)

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P1 | The tool-row extraction double-persisted every tool result (loop + helper). | **Fixed**: persisted once (`emitToolCall`); the existing stream tests + replay pin one row per call. |
| 2 | P1 | The nudge fired on the first TOOL-FREE completion at any step, not the FIRST completion — an agent that did its work and then answered was nudged. | **Fixed**: `step == 0` only (the spec's "first completion"). |
| 3 | P2 | The nudge row lost per-step usage. | **Fixed**: `usageOrNull` on the row; `TestNudgeUsageSumsAndCancelAfterGenerate` sums a nudged child to its execution totals. |
| 4 | P2 | A `"[]"` tool_calls row could be trimmed at a history-window boundary. | **Fixed**: `hasToolCalls` decodes the array; `TestTrimBoundaryEmptyToolCallsIsNotAnIntent`. |
| 5 | P2 | A run cancelled while the model answered was recorded as success. | **Fixed**: cancellation checked right after `Generate`; test finishes `cancelled`. |
| 6 | P2 | Receipt extraction missed bare filenames, label was misleading, URLs/inputs could appear. | **Fixed**: bare filenames with alphabetic extensions; URLs stripped; version-like tokens excluded; the label says "existence at fold-back, not proof of creation"; tests for each. |
| 7 | P2 | Receipt checks needed ward containment incl. symlinks. | **Fixed**: `wardtools.FileTools.Exists` (the tools' own `resolve` + a leaf symlink check); traversal and an outward symlink read ✗ (tested). |
| 8 | P2 | Seed read errors permanently suppressed the upgrade. | **Fixed**: only `IsNotExist` skips; other errors return without the marker. |

## Final reviewer pass (zbot-reviewer) — no P0/P1

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P2 | Spec said "FIRST completion", the implementation nudged any first tool-free completion. | **Fixed** (gate #2). |
| 2 | P2 | The receipt precondition (a hosted ward) was missing from the spec. | **Fixed**: the requirement states it, plus the existence-not-creation semantics and the containment rule. |
| 3 | P2 | Stale comment "Mounted on ROOT runs only". | **Fixed**. |
| 4 | P2 | "Raising the cap to 2 allows nesting" untested. | **Fixed**: `TestDelegationDepthGrantAndCap` now sets the cap to 2 explicitly (and `TestDepthCapHoldsNesting` pins the hold at 1). |
| 5 | P2 | `DefaultDepthCap = 2` differed from the config default. | **Fixed**: the constant mirrors the config default (1). |
| 6 | P2 | `tasks.md` unticked. | **Fixed** after verify. |
