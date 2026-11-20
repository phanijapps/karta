# Spec Delta

## Purpose

The in-process publish/subscribe bus delivering session-scoped timeline events to subscribers — the transport that later persists the Timeline Tree and streams over SSE.

## ADDED Requirements

### Requirement: Topic-scoped publish and subscribe

The eventbus SHALL implement the domain `EventBus` interface with subscriptions scoped to a session topic, delivering each published `TimelineEvent` only to that topic's subscribers.

#### Scenario: Subscribers see only their session

- **WHEN** events are published to session A while two subscribers listen to A and B respectively
- **THEN** the A subscriber receives all A events and the B subscriber receives none of them

#### Scenario: Publish without subscribers

- **WHEN** an event is published to a topic with no subscribers
- **THEN** the publish call succeeds without error and without blocking

### Requirement: Non-blocking publish

Publishing SHALL NOT block on subscriber consumption; each subscription SHALL have a bounded buffer.

#### Scenario: Slow subscriber does not stall the publisher

- **WHEN** a subscriber stops consuming and its buffer fills while events keep publishing
- **THEN** publish calls continue to return promptly

### Requirement: Subscriber isolation with explicit overflow policy

A slow or failed subscriber SHALL NOT affect delivery to other subscribers. When a subscription's buffer overflows, the bus SHALL apply a documented overflow policy and make the loss observable rather than silently dropping events without trace.

#### Scenario: Healthy subscriber unaffected

- **WHEN** subscriber B stops consuming and overflows
- **THEN** subscriber A continues receiving every event published to the topic

#### Scenario: Overflow is observable

- **WHEN** a subscription's buffer overflows
- **THEN** the policy (drop-oldest) applies and the subscription exposes or signals the overflow count so loss is detectable

### Requirement: Context-aware lifecycle

Subscriptions SHALL end cleanly when their context is cancelled, and closing the bus SHALL stop all delivery without goroutine leaks.

#### Scenario: Context cancellation ends a subscription

- **WHEN** a subscription's context is cancelled
- **THEN** its channel closes and no further events are delivered to it

#### Scenario: Close stops everything

- **WHEN** the bus is closed with active subscriptions
- **THEN** all subscription channels close and a goroutine-leak check after close reports none

#### Scenario: Events survive concurrent access

- **WHEN** multiple goroutines publish and subscribe concurrently
- **THEN** the bus operates without data races under the Go race detector
