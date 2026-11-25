# Tasks

## 1. Contract + domain + wrapper

- [ ] 1.1 Contract: `IntentAnalysisPayload` gains `hidden_intents`,
  `solution_path`, `recommended_mcp_tools`, `ward_archetype`,
  `ward_reason`, `explanation` (optional). `CONTRACT_CHANGE=1 task
  codegen`; commit. Verify: codegen zero drift.
- [ ] 1.2 Domain payload + `contractPayload` copies the full contract;
  the Test probe inherits it. Verify: `TestCompletedEventCarriesFullContract`.

## 2. Surface

- [ ] 2.1 The Task Analysis popover renders hidden intents, explanation,
  solution path, recommended agents + MCP tools (advisory note), ward
  reason. Verify: `pnpm check`; Playwright popover assertion.

## 3. Review and hardening

- [ ] 3.1 Phase gates (gpt-6.1-terra backend, gpt-6.1-sol frontend); fix
  P0/P1; P2s fixed or recorded in findings.md. Verify: ledger zero open
  P0/P1.
- [ ] 3.2 zbot-reviewer + `task verify` green (dev stack stopped).
  Verify: reviewer zero open P0/P1; verify exits zero.
