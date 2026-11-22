# mcp-import Specification

## Purpose

Accept standard `mcpServers` JSON (modelcontextprotocol.io and common tool variants) as RAW TEXT, parse it server-side with full fidelity (key order, number literals, duplicate detection), land each entry as an independent managed manifest, and report every outcome — tolerance is explicit and reported, never silent.

## Requirements

### Requirement: Raw-text document handling

The import request SHALL carry the pasted document as raw text. The daemon SHALL parse it server-side with an order-preserving, number-faithful reader: entries process in paste order, numeric `env` literals coerce from their exact source text, and duplicate keys (including Unicode-escaped equivalents) SHALL reject the whole document with a typed error naming the key — before any server is created. Document-level rejection SHALL cover: non-UTF-8 or BOM-prefixed text, malformed JSON, trailing content after the document, a JSON non-object, and payloads over 256 KiB. Every document-level check SHALL complete before the first create; a rejected document creates nothing.

#### Scenario: Paste order is processing order

- **WHEN** a paste lists server `"zeta"` before `"alpha"`
- **THEN** the outcomes are reported in that order and, at capacity, `"zeta"` is created while `"alpha"` reports the capacity failure

#### Scenario: Duplicate keys reject before writes

- **WHEN** a paste declares the key `"fs"` twice (once Unicode-escaped)
- **THEN** the request fails with a typed error naming the duplicated key and nothing is created

#### Scenario: Trailing garbage rejects

- **WHEN** a valid object is followed by more content
- **THEN** the request fails a whole-document check and nothing is created

### Requirement: Document classification

The document SHALL classify by wrapper PRESENCE, before any entry validation: a present `mcpServers` key is authoritative (it must be an object — a malformed or empty authoritative wrapper rejects the document with no fallback to `servers`); else a present `servers` key likewise; else a top-level `command` or `url` key means a single-server document (the request's `name` supplies its name); else the document is a bare map when at least one value is an object, and a map with no object values rejects as containing no recognizable servers. An unclassifiable document rejects with an error naming what was found. Classification decides the SHAPE; entry contents are then validated per entry — a malformed entry is a per-entry failure, never a document rejection.

#### Scenario: Wrapper presence is authoritative

- **WHEN** a paste has `"mcpServers": []` (malformed) or `"mcpServers": {}` (empty) alongside a valid `servers` map
- **THEN** the document rejects — no fallback, nothing created

#### Scenario: One bad entry does not reject the document

- **WHEN** a bare map contains one valid server and one scalar entry
- **THEN** the valid server is created and the scalar entry is reported failed with its reason

#### Scenario: Single-server document

- **WHEN** the paste is one server object and the request supplies its name
- **THEN** it imports exactly as the wrapped form would; without a name the request fails a document-level check

### Requirement: Per-server independence and outcomes

Each entry SHALL be processed independently in paste order, and every entry gets one reported outcome — created (with the stored name and any warnings), skipped (duplicate, disabled, other-OS), or failed (shape, field, name, capacity, or `io_error`) — carrying its original key and its specific reason. Duplicates SHALL be detected before capacity: an existing manifest, an occupied target path, or an earlier entry of the same paste makes the entry a skip even when the registry is full. A filesystem write failure (`io_error`) on one entry SHALL NOT abort the import: the entry is reported failed with `io_error` and later entries still process; the response is the per-entry report, never a bare error that discards it. Capacity fails only the entries that did not fit.

#### Scenario: Duplicates outrank capacity

- **WHEN** a full registry imports a paste naming an existing server plus a new one
- **THEN** the existing name is skipped as a duplicate and the new one reports the capacity failure

#### Scenario: Write failure keeps the report honest

- **WHEN** the filesystem refuses an entry's write after an earlier entry was created
- **THEN** the response still reports every entry — the earlier creation as created, the refused entry as failed `io_error` — and later entries still process

### Requirement: Field policy

Only `command`, `args`, `env`, and `url` SHALL ever reach a manifest, under the manifest's own rules (at-least-one transport; string `command`; string-array `args`; string-map `env`). Every other key follows this table — actions are warn-drop (import, warning naming the key, never echoing values), skip (entry skipped with that reason), or fail (entry failed): `type` string `stdio`/`http` warn-drop; `type` `"sse"` warn-drop with a deprecation note (the url imports as http; Test connection verifies); non-string or unknown `type` warn-drop inferring from fields; `headers` warn-drop; `cwd`, `autoApprove`, `alwaysAllow`, and comment-like keys warn-drop; `disabled: true` (boolean) skips the entry, non-boolean `disabled` warn-drops; `os` — string or array, normalized (`win32`/`win`/`windows`→windows, `darwin`/`mac`/`macos`/`apple`→darwin, `linux`→linux) — skips when present, non-empty, and not matching the platform, imports when absent, empty, null, or matching; a transport hint conflicting with the fields (`type: "http"` with only `command`) warn-drops with the fields winning. `env` number or boolean literals coerce to their exact source text with a warning; `env` objects, arrays, or null fail the entry; non-string `args` entries fail the entry.

#### Scenario: Foreign keys warn without echoing values

- **WHEN** an http server carries `headers` and `autoApprove`
- **THEN** the entry is created and its outcome warnings name the ignored keys only

#### Scenario: Platform scoping skips

- **WHEN** a Linux daemon imports a paste with `disabled: true` and `os: ["win32"]` entries
- **THEN** both are reported skipped with their reasons and nothing is created for them

#### Scenario: Conflicting transport hint loses to fields

- **WHEN** an entry has `type: "http"` but only a `command`
- **THEN** the entry imports as stdio with a warning naming the conflict

### Requirement: Key-to-name normalization

A JSON key SHALL become the managed file stem via the registry's slug algorithm — lowercase, runs of anything outside `[a-z0-9]` collapse to one dash, dashes trimmed, no truncation and no invented fallback — validated against the managed-name rule after normalization; the outcome reports the original key beside the stored name whenever they differ. A key normalizing to an unusable name, or an empty key, fails its entry with a typed reason. Two distinct keys normalizing to one slug: the first creates, the rest skip as duplicates; failed entries reserve nothing.

#### Scenario: Friendly key slugifies with report

- **WHEN** a paste keys a server `"Filesystem (local)"`
- **THEN** the manifest is stored as `filesystem-local` and the outcome reports both names

#### Scenario: Colliding slugs from distinct keys

- **WHEN** one paste contains `"Filesystem (local)"` and `"filesystem-local"`
- **THEN** the first is created and the second is reported skipped as a duplicate
