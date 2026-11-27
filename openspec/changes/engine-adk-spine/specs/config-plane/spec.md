# config-plane

## ADDED Requirements

### Requirement: Engine run path and context budget knobs
The daemon config SHALL strictly decode: `engine_loop` (`adk` | `legacy`,
default `adk` after the migration slice — `legacy` during the transition),
`skills.injection` (`progressive` | `bodies`, default `progressive`), and
under `compaction`: `summarizer` (optional qualified `provider/model` for
in-run summaries; empty = the run's own model) and `tool_result_cap`
(size in tokens; 0 = framework default). Unknown values SHALL hard-fail
(strict decode).

#### Scenario: The run path is selectable
- **WHEN** config sets `engine_loop: legacy`
- **THEN** the daemon constructs the engine with the legacy loop and runs behave as before the migration
#### Scenario: Skills injection is revertible
- **WHEN** config sets `skills.injection: bodies`
- **THEN** engine-run prompts compose full SKILL.md bodies (the pre-migration behavior) and the skill tool is not offered
#### Scenario: An invalid value fails loudly
- **WHEN** config sets `engine_loop: spaghetti`
- **THEN** startup fails with a strict-decode error naming the field
