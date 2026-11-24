# Spec Delta

## Purpose

Pre-execution intent analysis for goal runs: classify the task,
recommend resources from the semantic index, gate the approach, set
the step budget — cheap, typed, observable, and non-fatal.

## ADDED Requirements

### Requirement: A router bypasses analysis for trivial work

Before any intent-model call, a deterministic router SHALL bypass
analysis for greetings and non-task messages (the run proceeds
directly; a bypass event records the decision). No other message
reaches the model without analysis on root runs.

#### Scenario: Greeting bypasses
- **WHEN** a goal run's text is a greeting or smalltalk
- **THEN** no intent-model call is made and the run starts with a bypass event

### Requirement: The intent agent writes a typed contract

Root goal runs SHALL be analyzed by one small-model call producing a
typed contract: primary_intent, hidden_intents (actionable unstated
expectations, not labels), solution_path (high-level steps),
complexity (S/M/L/XL), recommended_skills/agents/mcp_tools (names from
the indexed resources only — names not in the index are dropped with a
warning event), ward_recommendation {action: use_existing|create_new,
name, archetype, reason} (use_existing requires an exact ward-index
match; names are reusable domain categories, never task-specific), and
execution_strategy {approach: simple|graph with concrete gating
criteria}. The analysis itself uses no tools.

#### Scenario: Full contract on a research task
- **WHEN** "Analyze my portfolio's tech holdings and write a report" starts a goal run
- **THEN** the analysis carries a kebab-case intent, a graph approach, ward recommendation, and indexed resource names only

#### Scenario: Hallucinated names are dropped
- **WHEN** the intent model recommends a skill not in the index
- **THEN** the recommendation is dropped and a warning event names the dropped item

### Requirement: Complexity sets the step budget

The contract's complexity SHALL map to the run's engine step budget
(S=4, M=8, L=16, XL=24) replacing the fixed default for that run.

#### Scenario: XL task gets the big budget
- **WHEN** analysis returns complexity XL
- **THEN** the run's bounded loop allows up to 24 steps

### Requirement: Analysis injects as a prompt section and gates approach

A successful analysis SHALL render as a `## Task Analysis` section in
the root run's system prompt (children unaffected), including the
fast-path instruction when approach=simple (skip multi-step
orchestration). The recommendations are advisory — the model decides.

#### Scenario: Simple approach stays lean
- **WHEN** analysis says approach=simple for a quick question
- **THEN** the injected section tells the model to answer directly without orchestration

### Requirement: Resources are discovered semantically

Skills, enabled MCP tools, and agents SHALL be embedded (name +
description) into a resource index via the local ONNX seam; the intent
agent's context carries the semantic recall hits for the user goal
(not the full listing) with a listing fallback for small rosters or a
cold cache. Registry changes (skill installed, MCP enabled, agent
edited) SHALL refresh the indexed entries.

#### Scenario: Semantic recall surfaces the right MCP
- **WHEN** the goal mentions "search the web" and a search MCP's tools are indexed
- **THEN** the recall hits include that MCP's tools without a full listing in the context

#### Scenario: Index refresh on registry change
- **WHEN** a new skill is installed while the daemon runs
- **THEN** the next analysis recalls it without a restart

### Requirement: Analysis is observable and non-fatal

Every decision SHALL emit events (started/complete/bypass/failed with
reason) and persist the result on the execution record. Analysis
failure NEVER blocks the run — it proceeds without the section and the
events say why.

#### Scenario: Model failure degrades gracefully
- **WHEN** the intent-model call fails
- **THEN** the run starts anyway, a failed event carries the reason, and no Task Analysis section is injected
