# cron-scheduler Specification

## Purpose

Cron-expressed schedules that start goal runs on their own: registry
definitions, CRUD contract, fire/miss/overlap semantics, per-schedule
history, and a main-area Schedules surface.

## Requirements

### Requirement: Schedules are registry-defined

Schedule definitions SHALL be YAML files under
`~/.zbot/config/schedules/` (strict fields: name, cron expression,
goal, optional agent, optional model, missed-fire policy `skip`
default | `catch_up`), loaded with the registry's semantics (per-file
error isolation, capacity bound, hot-reload) and managed through the
registry's atomic CRUD write paths (no-clobber creates, provenance).

#### Scenario: A schedule loads
- **WHEN** `~/.zbot/config/schedules/digest.yaml` defines a valid 5-field or descriptor cron with a goal
- **THEN** the snapshot exposes the schedule with its parsed next-fire time

#### Scenario: Invalid expression is a typed create rejection
- **WHEN** a create carries an expression the cron parser rejects
- **THEN** the API responds with a typed 400 naming the parse error and no file is written

### Requirement: Fires start goal runs in a per-schedule session

At each fire time the scheduler SHALL start a `RunGoal` into the
schedule's persistent session (`sched-<name>`) with the schedule's goal,
agent, and model; the run is observable and cancellable like any other.
While a schedule's run is in flight, further fires SHALL be skipped and
recorded. Missed fires SHALL follow the schedule's policy (`skip`
forgets; `catch_up` fires once on wake). Every fire/skip/miss decision
SHALL emit to the eventbus.

#### Scenario: A fired schedule runs its goal
- **WHEN** a `@every 1s`-style test schedule's fire time arrives
- **THEN** a run starts in `sched-<name>`, its steps appear in that session's timeline, and the outcome is recorded

#### Scenario: Overlapping fires skip
- **WHEN** a fire is due while the schedule's previous run is still active
- **THEN** no second run starts and the skip is recorded as an observable event

### Requirement: Schedules CRUD contract

`GET /api/schedules` SHALL list schedules with next-fire time and last
outcome; `POST /api/schedules` creates (typed 400s for invalid
expressions/names); `GET/PUT/DELETE /api/schedules/{name}` read, update
(rename-aware), and delete. Schemas strict; endpoints additive.

#### Scenario: Round trip
- **WHEN** a schedule is created, listed, updated, and deleted via the API
- **THEN** each step reflects the registry truth and the file tree matches

### Requirement: Schedules live in the main navigation

The sidebar SHALL offer a Schedules destination (between Conversations
and Wards) opening a Schedules page: schedule cards (name,
expression, humanized cadence, next fire, goal, agent), create/edit with
inline expression validation from the daemon's parser, delete with
confirm, and an action opening the schedule's session timeline.

#### Scenario: The page manages the roster
- **WHEN** the user creates a schedule with an invalid expression
- **THEN** the dialog shows the daemon's parse error and creates nothing

#### Scenario: Timeline access
- **WHEN** the user opens a schedule's timeline from its card
- **THEN** the app shows that schedule's session history
