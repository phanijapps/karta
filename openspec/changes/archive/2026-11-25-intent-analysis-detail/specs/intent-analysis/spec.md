# Spec Delta

## MODIFIED Requirements

### Requirement: The analysis is queryable session state

The daemon SHALL expose the active session's LATEST terminal analysis
(`analysis_completed`, `analysis_bypassed`, or `analysis_failed`) as
session state (`GET /api/sessions/{id}/state`): the session row plus the
analysis block — the FULL contract the analyst decided (primary intent,
hidden intents, solution path, complexity, budget, approach and its
explanation, ward action/name/archetype/reason, recommended
skills/agents/MCP tools, dropped names) and, when analyzed, model,
tokens in/out, duration, attempts. The chat surface's Task Analysis
card and the context panel's Provisioned Skills section SHALL render
from this state (or the live event) — never by parsing transcript rows.

#### Scenario: The card renders for engine-routed research runs
- **WHEN** a research session's state carries an analyzed intent
- **THEN** the chat surface renders the Task Analysis card with the
  intent, complexity, approach, ward, provisioned skills, and dropped
  names

#### Scenario: The analyst's full decision reaches the surface
- **WHEN** an analysis decides hidden intents, a solution path, MCP
  recommendations, a ward reason, and an explanation
- **THEN** the completed event, the session state, and the Task Analysis
  popover carry every one of them

#### Scenario: Chat sessions render no analysis
- **WHEN** the active session is the quick chat (lean — no analysis
  runs)
- **THEN** session state carries no analysis block and the surface
  renders no card

#### Scenario: The draft clears at send start
- **WHEN** the user sends a message
- **THEN** the composer's draft clears immediately (not on
  completion) — a multi-minute research run never holds the user's
  text in the input
