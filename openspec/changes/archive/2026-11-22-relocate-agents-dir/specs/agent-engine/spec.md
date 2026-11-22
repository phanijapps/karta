# Spec Delta

## MODIFIED Requirements

### Requirement: Agent definitions come from the registry

A goal MAY name a registry agent (`~/.zbot/agents`); its system prompt, optional model override, and tool allowlist SHALL shape the run. With no agent named and none configured, a minimal wiring prompt is used. Model precedence: the request's model, else the agent's, else the run is refused with guidance to select one. No agent is hardcoded in code.

#### Scenario: A configured agent shapes its run

- **WHEN** a goal names an agent with a system prompt and tool allowlist
- **THEN** the run uses that prompt, that model if set, and only allowlisted tools

#### Scenario: No model resolvable

- **WHEN** neither request nor agent names a model
- **THEN** the run is refused with guidance to select a model
