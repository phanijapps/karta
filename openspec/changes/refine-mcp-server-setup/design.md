# Design

## Context

The MCP add/edit form is inline with a stdio checkbox that always shows
the URL field; real configuration arrives as standard `mcpServers` JSON
(modelcontextprotocol.io quickstart; Claude Desktop/Cursor/VS Code
variants). The registry's create path already enforces every rule we
need (managed-name validation, capacity, no-clobber, at-least-one
transport, field bounds); its slug algorithm is skill-documented and
unexported. Predecessor `manage-mcps-and-skills` is implemented but
unarchived and renames the same settings-shell requirement this change
touches (D7).

## Goals / Non-Goals

**Goals:** a dialog for MCP add/edit with two modes (Form / Paste
JSON); a transport selector showing only relevant fields; server-side
raw-text import of standard JSON with per-entry outcomes and explicit,
reported tolerance.

**Non-Goals:** bulk export; whole foreign config files (only the
servers map is read); SSE transport (imports as http, probe reports);
headers/auth on http (warn-dropped); skills UI; runtime wiring.

## Decisions

### D1: The paste travels as raw text; the daemon is the only parser

The request carries `document` as a STRING. The server parses with an
order-preserving, number-faithful reader (`json.Decoder` token walk,
`json.Number` literals, `UseNumber` decoding): entry order is paste
order (integer-like keys included — no JS object reordering), numeric
env literals coerce from their exact source text (`9007199254740993`
survives), and duplicate keys — detectable in a token walk, including
Unicode-escaped equivalents — reject the whole document before any
write. Client-side parsing exists only as an advisory affordance
(enabling the name field for single-object pastes). This supersedes the
reviewed draft's JSON-object transport, which silently reordered,
collapsed duplicates, and lossified numbers.

### D2: Classification by wrapper presence; entry validation is separate

Ordered ladder, decided on KEYS PRESENT, before any entry validation:
(1) `mcpServers` present → authoritative: it must be an object;
malformed or empty rejects the document with NO fallback to `servers`;
(2) else `servers` present → same rule; (3) else a top-level `command`
or `url` key → single-server document (needs `name`) — this resolves
the genuinely ambiguous `{"command": …}` shapes toward single-server,
and the rejection message tells a user who meant a server NAMED
"command" to wrap it in `mcpServers`; (4) else a bare map iff ≥1 value
is an object (all-object-values is NOT required — `{"good":{…},
"bad":42}` classifies as a map and `bad` fails per-entry); a map with
no object values rejects as containing no recognizable servers. Shape
and entry validation never mix: no malformed entry can reject a
classified document, and no classified document's shape error appears
on an entry.

### D3: One field-policy table, normative

The mcp-import spec carries the full table (accepted types, absent/null
behavior, skip-vs-fail precedence, OS alias normalization
win32/win/windows→windows, darwin/mac/macos/apple→darwin, transport
conflicts with fields winning). Only `command`/`args`/`env`/`url` ever
reach `MCPSpec`. Warnings name keys, never values (headers can carry
secrets). Outcomes carry `source_key`, `name` (stored), `status`
(created | skipped | failed), `reason` (incl. `io_error`), and
`warnings[]`, in paste order.

### D4: Duplicate-before-capacity; io failures keep the report honest

The import loop, per entry and in order: normalize the key (D5) →
duplicate check FIRST (existing snapshot name, occupied target path via
stat, or an earlier entry of this paste — including two keys colliding
on one slug) → skip; then capacity (registry count) → fail-capacity;
then `CreateMCP` (whose own capacity check can only fire on a race and
is reported as capacity). A create returning `io_error` does NOT abort:
the entry reports `io_error`, later entries still process, and the
response is the per-entry report — an unconfigured registry is the one
document-level 500, checked before any entry. This precedence sits in
the import loop, not in `CreateMCP`, so the create API keeps its
existing semantics.

### D5: The registry's slug is the one normalizer — exported

`registry.Slugify` (exporting today's unexported `slugify`, unchanged
algorithm: lowercase, non-`[a-z0-9]` runs → one dash, trim, no
truncation, no fallback) becomes the import's key-to-stem step,
validated by the managed-name rule after normalization. The registry
gains only the export and doc comment — no behavior change; the import
remembers created slugs (failed entries reserve nothing) for same-paste
collision skips.

### D6: Edit-as-JSON rides the import endpoint as replace mode

The import request gains optional `replaceName`: the document must hold
EXACTLY one entry (any wrapper form); the entry maps with full D1–D3
fidelity and replaces `replaceName`'s manifest wholesale via the
existing update path — name unchanged (renames belong to Form mode,
keeping one rename story). Multi-entry with `replaceName`, an unknown
`replaceName`, or an entry that would skip or fail → request rejected
or reported with the target untouched. The edit dialog prefills
`{"mcpServers": {"<name>": <current transports>}}`. One parser, one
normalizer, no client-side mapping of standard JSON to the canonical
PATCH shape. Mode switches keep each mode's draft while the dialog is
open.

### D7: Sequencing with the unarchived predecessor

Both changes rename `MCP and skill previews` → `Live MCP and skill
management`; this change's delta carries the rename so it validates
against today's main spec. MANDATORY rebase after the predecessor
archives: drop this change's RENAMED section, keep the MODIFIED
requirement under the new name, and re-validate against the updated
main spec before archiving (serial execution alone leaves a stale
FROM — the rename is redundant, not racy).

## Risks / Trade-offs

- [Token-walk parser is more code than `json.Unmarshal`] → it is the
  only way to keep the review's fidelity promises; table-tested.
- [Replace mode overloads the import endpoint] → one parser and one
  tolerance table instead of two; the single-entry constraint keeps it
  honest (task-tested).
- [Import creates servers without preview] → every creation is an
  outcome row; duplicates skip; delete is one click behind.
- [Two unarchived changes, one rename] → D7's mandatory rebase step,
  executed at archive time.

## Migration Plan

Additive: one endpoint, one registry export, one dialog. No data or
schema moves; rollback reverts the commit.

## Open Questions

None — caps (256 KiB, 32 entries), the tolerance table, and the OS
alias map are tunable constants/tables, not architectural unknowns.
