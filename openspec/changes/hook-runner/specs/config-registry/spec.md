# config-registry

## MODIFIED Requirements

### Requirement: Hook manifest loading

The registry SHALL load `hooks/*.yaml` manifests (the top-level `~/.zbot/hooks/` root) with: `event` from the six lifecycle names (`PreToolUse`, `PostToolUse`, `PreCompact`, `SessionStart`, `SessionEnd`, `SubagentStop`), optional `matcher` (a regex that must compile), required `command`, and `timeout_ms`. Manifests failing any constraint are reported per-file. The `matcher` applies to the event's subject — the tool name for PreToolUse/PostToolUse, the agent name for SubagentStop — and is ignored for events without a subject.

#### Scenario: Valid manifest loads

- **WHEN** a hook manifest declares `event: PreToolUse`, a compiling `matcher`, a `command`, and a timeout
- **THEN** the registry exposes the hook bound to that event

#### Scenario: Bad event or matcher rejected

- **WHEN** a manifest names an unknown event or carries a non-compiling matcher regex
- **THEN** the failure is reported with the file path and the reason

#### Scenario: The matcher binds to the event's subject

- **WHEN** a PreToolUse manifest declares `matcher: ^ward_` and a SubagentStop manifest declares `matcher: ^research`
- **THEN** the first runs only for ward tools and the second only when the research agent stops; a matcher on PreCompact, SessionStart or SessionEnd never selects by name
