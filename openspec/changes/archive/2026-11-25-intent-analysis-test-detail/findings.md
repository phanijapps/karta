# Findings — intent-analysis-test-detail

Presentation-only (skip_specs). Phase gate (gpt-6.1-sol) and the final
`zbot-reviewer` pass: no P0/P1. P2s, all fixed:

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P2 | The result-wide `role="status"` would announce the whole decision (prose, chips, disclosure) atomically. | **Fixed**: the live region is the outcome line only; the detail sections sit outside it. |
| 2 | P2 | The raw-JSON assertion passed on `textContent` with the `<details>` closed — the disclosure was untested and the comment was wrong. | **Fixed**: `<pre>` hidden → summary focused + Enter → visible → JSON parsed and `primary_intent` asserted. |
| 3 | P2 | Tasks unticked; `findings.md` missing. | **Fixed** (this file; tasks ticked after verify). |
