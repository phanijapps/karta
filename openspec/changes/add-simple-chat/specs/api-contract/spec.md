# Spec Delta

## ADDED Requirements

### Requirement: Chat send endpoint

The contract SHALL define `POST /api/chat/messages` accepting the message text and the selected model (`provider/model-id`), returning every row the exchange appended in order (user row, any tool rows, final assistant row) with the exchange's end cursor. Typed failures SHALL return the persisted user row and its cursor. It SHALL return a typed error when no model is selected (400) and typed provider failures (502 with reason) or keyring-unavailable outcomes matching the provider endpoints.

#### Scenario: Send returns the full exchange

- **WHEN** the send endpoint is called with text and a selected model
- **THEN** it returns the appended user message, any tool rows, and the assistant reply in order

#### Scenario: Send without a model

- **WHEN** the send endpoint is called without a model selection
- **THEN** it returns 400 with guidance to pick a model

#### Scenario: Provider failure returns the persisted row

- **WHEN** the completion fails upstream
- **THEN** it returns the typed failure together with the persisted user message and cursor, and the transcript keeps any committed intermediate rows

### Requirement: Chat transcript sync endpoint

The contract SHALL define `GET /api/chat/messages` returning transcript rows ordered by `seq` with bounded pages, a `next_cursor` that is the LAST DELIVERED sequence (a page cursor, never the database tail), and `truncated` indicating whether more replay rows remain. Clients page until `truncated` is false.

#### Scenario: Full replay pages to the end

- **WHEN** the endpoint is called with no cursor on a multi-page transcript
- **THEN** paging with each returned `next_cursor` yields every row exactly once, in order

#### Scenario: Incremental sync

- **WHEN** the endpoint is called with the last delivered cursor
- **THEN** it returns only newer rows and the new page cursor
