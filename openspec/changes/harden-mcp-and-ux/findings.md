# Findings — harden-mcp-and-ux

## Phase gates

### Gate 1 — gpt-6.1-terra (backend)

Verdict: leave-ward, auto-titling, tool-event text, terminator
ordering, boundaries all confirmed. 5 P1 + 3 P2 — ALL FIXED:

- **G1-1 (P1) backoff never grows** — FIXED: the dead entry is kept
  across retries (unchanged spec) so recordStartFailure bumps the
  count (2s → 4s → 8s…); a spec CHANGE resets. Test: two expired
  windows → failures=3.
- **G1-2 (P1) busy 409 after a committed 200** — FIXED: the busy
  check precedes the headers/status. Test: status 409 + JSON
  content-type (never a stream). Found the test server needed a real
  bus (a nil bus panicked the research path).
- **G1-3 (P1) content chunks emitted finish_reason "" (invalid)** —
  FIXED: contract `type: [string, "null"]` (OpenAPI 3.1 — `nullable`
  is invalid there), FinishReason a pointer → JSON null on content
  chunks. Test pins `"finish_reason":null` and no empty-string leak.
- **G1-4 (P1) the final status chunk could drop** — FIXED:
  `streamWriter.finishAll` — an ORDERED, blocking (non-droppable)
  terminal sequence for answer + finish_reason + [DONE]; sync-mode
  writers write directly.
- **G1-5 (P1) stale tool panicked on a dead entry** — FIXED: both
  `srv` and `srv.session` re-checked after the redial attempt → typed
  FailUnavailable. Test: deconfigured server + stale tool.
- **G1-6..8 (P2)** — FIXED: Exec-side redial honors the backoff window
  (typed "retry in Ns"); error frames carry the message as a content
  chunk before `finish_reason: error`; node discovery lists
  `~/.local/node-v*/bin` newest-first (no version pin).

### Gate 2 — gpt-6.1-sol (frontend)

Verdict: guard compliance (JSON content type), roster refresh, warden
UX, auto-title rendering, FSD/runes all confirmed. 1 P1 + 1 P2 —
BOTH FIXED:

- **G2-1 (P1) hand-rolled leaveWard binding** — FIXED: routed through
  `callOperation` keyed on the generated `operations["leaveWard"]`
  (typed 409 body), the module's generated-transport pattern.
- **G2-2 (P2) Leave removed the focused control without a handoff** —
  FIXED: an awaited handler captures the ward name BEFORE the binding
  clears (the test caught the derived reading empty: "Left ."),
  announces "Left <ward>. The session moved under Research." in the
  polite live region, and focuses the composer after `tick()`;
  accessible name "Leave ward <ward>". Playwright-pinned.

## Decisions (recorded during implementation)

- **D1 — Work frames as chunk content.** OpenAI clients render only
  `delta.content`; a custom field would be dropped. Prefix markers
  (🔧 tool, 👥 delegation, 🧠 step) keep research runs legible.
- **D2 — `[DONE]` is the writer's TERMINAL frame.** A direct write
  after `finish()` raced the drain goroutine and printed `[DONE]`
  BEFORE the stop frame (live-caught). The terminator is now a
  `donePayload` message queued behind the stop chunk; the handler test
  pins the ORDER.
- **D3 — tool_call bus events carry `Text` = tool name.** The name
  lived only in the structured payload; every SSE consumer (the UI's
  work stream, /v1 frames) rendered empty labels. Now both.
- **D4 — Backoff check precedes the drop.** The first Refresh order
  (drop dead entry → check backoff) re-dialed on every Tools() call
  because the backoff state lived on the dropped entry. Check first;
  a spec CHANGE still resets.
- **D5 — Binary resolution is additive**: as-written first (absolute
  or PATH), then HOME tool dirs; unknown names fall back verbatim so
  exec reports the familiar "not found".
- **D6 — Leave keeps the transcript** (history is the truth) and drops
  the in-memory host too (otherwise the next run pre-loads the ward
  the user just left). Wardens show a note instead of Leave — a
  warden's binding IS its identity.
- **D7 — Auto-titles never overwrite** (`SetSessionTitleIfUntitled`):
  a user's name always wins; the slug is a label (spaced words), not
  a URL slug.

## Live-verified

- /v1 chat stream: `content → stop → [DONE]` (ordered).
- /v1 research stream: 6 chunk frames — 🔧/🧠 work frames then the
  sourced AMD figure (`$13.4B Q3 2026`, ir.amd.com), stop, [DONE].
- The operator's bare `command: npx` config (which failed before the
  resolver) connects — `brave-mcp live (9 tools)`.

## P2s (recorded, not blocking)

- **P2-1 — Leave on wardens.** Hidden by design (D6); a "retire this
  ward's warden" surface is a later change if wanted.
- ~~**P2-2 — Streaming error frames carry no message**~~ — superseded
  by gate fix G1-7 (the message rides a content chunk before
  `finish_reason: error`).

## Final review

**zbot-reviewer (final gate): every terra + sol fix verified holding.
1 P1 (documentation) + 5 P2 — ALL FIXED on receipt:**

- **F-1 (P1) contract + architecture doc still said non-streaming** —
  FIXED: the /v1 operation description and
  docs/architecture/openai-channel.md now describe the chunk stream,
  the pre-stream busy 409, and `finish_reason: error`.
- **F-2 (P2) `srv.notBefore` read outside `e.mu`** — FIXED: captured
  under the lock (a latent data race with recordStartFailure).
- **F-3 (P2) host-drop path untested + dead scaffolding** — FIXED:
  TestLeaveWardHandler wires a real SetWardDropper and asserts the
  engine host drop; TestHostLeave pins Active()==nil + shell refusal.
- **F-4 (P2) D3 (tool_call events carry Text) unpinned** — FIXED:
  TestChildUsesToolResult now collects the stream and asserts the
  clock_now event's Text (children surface as delegation events —
  pinned on both kinds).
- **F-5 (P2) AGENTS.md drift** — FIXED: server (streaming/finishAll/
  leave), intent (TitleSession), store (SetSessionTitleIfUntitled),
  wardtools (Host.Leave), engine/eino (DropWard + Text) each gained
  their bullet.
- **F-6 (P2) record/code drift** — FIXED: node ordering compares
  numeric segments (v9 < v10); SlugTitle doc says spaced words; the
  design's 48-char fallback claim corrected (no ASCII → no title);
  the superseded P2-2 struck.

**Operator validation follow-up (post-review):**
- *Thinking leaked into chat text* — root cause was NOT the stream
  (211 reasoning deltas were correctly kind-tagged) but glm's habit of
  narrating its plan as `content` on tool-call intent rows ("Announcing,
  then executing one call…"), which the transcript rendered as chat
  bubbles. FIXED: tool-call intent rows are compact step rows — the
  narration folds under "show", the reasoning under a collapsed
  "Thinking" (the brain), only final answers are bubbles. The LIVE
  bubble's reasoning now starts collapsed too. Playwright-pinned.
- *Intent analysis not showing* — the inline card only rendered when
  state already carried an analysis. REPLACED with a compact chip
  (intent · complexity · ward) present for EVERY research session
  (pending → "Analyzing…"), with an **(i)** that opens the Task
  Analysis popover (intent, budget, approach, ward action, skills,
  dropped, degradation reason) — dialog semantics, Escape closes,
  focus returns to the (i). Light/dark axe: 0 violations (fixed three
  pre-existing light-theme contrast failures on the way: ward badge on
  the chip, dropped chips, the ward strip).

**Operator ideation follow-up — tool-call stacking (friendly verbs):**
The flat tool-row transcript (30+ identical rows on the NVDA run) is
replaced by a STEP TREE (`features/chat-stream/steps.ts` +
`WorkSteps.svelte`, pure frontend): each assistant tool-call TURN is
one step labeled **friendly verb + object + outcome** ("Entered ward
stock-analysis · ok", "Ran python3 data/amd_deepdive.py · exit 0 ·
3.9s", "Searched the web … · 2 calls · 1 error"); consecutive
same-tool turns merge (count + merged outcomes); a delegation's child
turns nest under it (the injected child user row is absorbed, not a
bubble); only final answers are chat bubbles. Expansion shows the
plan ("Plan" label), the brain (collapsed), each call's raw name +
args + result excerpt. Mixed turns name by majority tool. Live steps
auto-open once and tidy when the turn ends. Live result on the
operator's NVDA session: 7 steps. Tests: 11 session specs (grouping,
nesting, folding, outcomes); axe 0/0 (light-theme error-hint contrast
fixed on the way). Decisions recorded: collapsed-by-default with
auto-open-while-live; friendly verb at the top level, raw name in the
expansion.

**task verify: exit 0** — gates + codegen zero drift + lint 0 +
full suites + budget gates OK (suite 27.3s / 28s — the hardening
tests' cost trimmed honestly: the echo server builds once per test
binary via sync.Once, the group-kill poll window tightened to 0.8s
past the landing time); 92 Playwright.
