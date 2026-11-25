# Spec Delta

## MODIFIED Requirements

### Requirement: The research thread separates reports from the answer

The research thread SHALL render dialogue only, with three distinct roles: the ROOT run's assistant rows without tool calls are answers — the LAST one is THE answer; any other agent's assistant row without tool calls is a sub-agent report, never rendered in the answer style. Every delegation in a turn — derived from the root's `delegate_agent` intents so that transcripts from before attribution qualify too — SHALL surface as a clickable pill naming the agent; activating it opens a slideout dialog with the delegation's task, its cost when known, the agent's calls (the Work tab's step tree over that delegation's rows) and the report it returned, with Escape and the close control returning focus to the pill. The answer block SHALL expose the model's reasoning for the final step under a collapsed "Thinking" toggle and the last narration the root wrote before the answer, labelled as narration.

#### Scenario: Reports never pose as answers
- **WHEN** a run delegates a research task and the child returns a long report
- **THEN** the thread shows a delegation pill for the child agent and the root's final answer as the only answer-styled block

#### Scenario: A pill opens the agent's calls
- **WHEN** the user activates a delegation pill
- **THEN** a slideout names the agent and task, lists the agent's calls with their outcomes, shows the report it returned, and Escape closes it with focus back on the pill

#### Scenario: Legacy sessions get pills too
- **WHEN** a session's transcript predates agent attribution but contains `delegate_agent` intents
- **THEN** its delegations render as pills with the same slideout

#### Scenario: Thinking is the model's reasoning, narration is labelled
- **WHEN** the final answer is rendered
- **THEN** its Thinking toggle reveals the final step's reasoning text and the narration line is labelled as narration, not thought

#### Scenario: Legacy transcripts still render
- **WHEN** a session's rows predate agent attribution
- **THEN** the thread renders them as before (answers), with delegation pills from the intents
