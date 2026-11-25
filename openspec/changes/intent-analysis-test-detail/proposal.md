# Proposal

## Why

The Intent Analysis card's Test result prints a one-line decision
(intent · complexity · approach · ward). The response carries the
analyst's whole v1 decision — hidden intents, solution path,
explanation, resources, ward reason — and the operator asking "where do
I see the response?" cannot see it there. Presentation only: the
settings-shell scenario already requires "the contract summary".

## What Changes

- The Test result renders the full decision (hidden intents, solution
  path, explanation, resources incl. dropped, ward reason) and a
  collapsible raw JSON view of the payload.

## Capabilities

None — presentation of fields the contract already carries
(`skip_specs: true`).

## Impact

`frontend/src/features/settings/advanced/IntentAnalysisCard.svelte`,
its Playwright spec.
