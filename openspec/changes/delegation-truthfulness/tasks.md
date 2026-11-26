# Tasks

## 1. Config + registry + depth knob

- [ ] 1.1 `delegation.max_depth` (default 1, ≥1 validated) →
  `SetDepthCap`; `AgentSpec.RequireToolUse` (strict decode both forms).
  Verify: config/registry tests; `TestDepthCapHoldsNesting`.

## 2. Engine

- [ ] 2.1 MCP plane for children; the first-answer nudge (`require_tool_use`);
  deliverable receipts; per-step usage on assistant rows. Verify:
  `TestChildGetsMCPTools`, `TestToolFreeFirstAnswerNudgedOnce`,
  `TestDeliverableReceipts`, `TestAssistantRowsCarryUsage`.

## 3. Seeds

- [ ] 3.1 Honesty contract on research-agent + builder-agent seeds with
  `require_tool_use: true`; the once-per-home upgrade of byte-identical
  copies. Verify: `TestSeedHonestyUpgrade`, seed decode test.

## 4. Review and hardening

- [ ] 4.1 Phase gate (gpt-6.1-terra); fix P0/P1; P2s fixed or recorded.
- [ ] 4.2 zbot-reviewer + `task verify` green (dev stack stopped).
