# mock-chat Specification

## Purpose

Let users explore a realistic conversation layout and composer flow using safe deterministic fixtures before live agent integration exists.

## Requirements

### Requirement: Selectable sample conversations

The chat surface SHALL offer at least two labeled sample conversations in the left sidebar, with user and assistant messages and a selected state. Selecting a conversation SHALL show its own transcript. New Chat SHALL open an empty in-memory conversation without deleting existing transcripts.

#### Scenario: Switch sample conversations
- **WHEN** the user selects a different sample conversation
- **THEN** its title and messages appear and the left sidebar reflects the selected conversation

#### Scenario: Start an empty chat
- **WHEN** the user activates New Chat after viewing a sample
- **THEN** a fresh empty conversation appears with a composer and suggested starter text
- **AND** previous transcripts remain selectable for the current page lifetime

### Requirement: Deterministic mock composition

Submitting nonblank composer text SHALL append a user message and one deterministic mock assistant response to the selected conversation, clear the composer, and reveal the latest message. Blank input SHALL NOT submit. The interface SHALL identify the response as simulated and SHALL NOT imply agent work or tool execution.

#### Scenario: Send a message
- **WHEN** the user enters text and activates Send or presses Enter outside IME composition
- **THEN** the transcript gains that user message and a visibly simulated assistant reply with no external request
- **AND** focus remains in the composer for continued typing

#### Scenario: Multiline and blank input
- **WHEN** the user presses Shift+Enter in the composer
- **THEN** a newline is inserted without sending
- **AND** Send remains disabled for an empty or whitespace-only draft

#### Scenario: Composition input
- **WHEN** the user presses Enter while an input-method composition is active
- **THEN** the shell does not submit a message

### Requirement: Model and action previews are honest

The chat header SHALL offer a fixture-backed model/provider selector whose selection is in-memory only. Unimplemented attachment, voice, share, or execution actions SHALL be omitted or visibly unavailable with an explanation. Displayed message content SHALL be rendered as safe text without executing HTML.

#### Scenario: Select a mock model
- **WHEN** the user selects another fixture model
- **THEN** the header reflects that choice with a mock indication and no model discovery or connection request occurs

#### Scenario: Untrusted message text
- **WHEN** a user submits text containing HTML or script-like markup
- **THEN** it appears as inert text rather than executable content
