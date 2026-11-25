# Spec Delta

## MODIFIED Requirements

### Requirement: The context panel surfaces the active session's context

The right panel SHALL render the ACTIVE session's context: the ward card (canonical title, archetype, lint status, layout) when the session is ward-bound, the provisioned skills, and the agent roster. Chat sessions render no ward card. The Work tab SHALL label every step with the agent that took it, show one card per delegation (agent, task summary, tool mix, token usage, duration, status) as the run works, and carry a run footer with the session's executions' cost (prompt/completion tokens per execution and in total, delegations, elapsed time).

#### Scenario: Ward-bound session context
- **WHEN** the active session is ward-bound
- **THEN** the context panel shows the ward card and the session's provisioned skills and agents

#### Scenario: Chat session context
- **WHEN** the active session is the quick chat
- **THEN** the context panel shows no ward card

#### Scenario: Delegations are cards with cost
- **WHEN** a research run delegates to two agents
- **THEN** the Work tab shows one card per delegation with the agent, its tool mix, token usage and duration, updated as each completes

#### Scenario: The run footer totals the run
- **WHEN** a research run completes
- **THEN** the Work tab footer shows the total prompt/completion tokens across the root, the analyst and every child, the number of delegations, and the elapsed time

## ADDED Requirements

### Requirement: The research thread separates reports from the answer

The research thread SHALL render dialogue only, with three distinct roles: the ROOT run's assistant rows without tool calls are answers — the LAST one is THE answer; any other agent's assistant row without tool calls is a sub-agent report, rendered as a collapsed card naming the agent and the report's size, never in the answer style. The answer block SHALL expose the model's reasoning for the final step under a collapsed "Thinking" toggle and the last narration the root wrote before the answer, labelled as narration.

#### Scenario: Reports never pose as answers
- **WHEN** a run delegates a research task and the child returns a long report
- **THEN** the thread shows a collapsed report card for the child agent and the root's final answer as the only answer-styled block

#### Scenario: Thinking is the model's reasoning, narration is labelled
- **WHEN** the final answer is rendered
- **THEN** its Thinking toggle reveals the final step's reasoning text and the narration line is labelled as narration, not thought

#### Scenario: Legacy transcripts still render
- **WHEN** a session's rows predate agent attribution
- **THEN** the thread renders them as before (answers), with no report cards
