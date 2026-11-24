# Design

## Context

The operator's model (mock:
docs/architecture/mock-session-model.html): wards ARE sessions. Each
ward bot has its own persistent conversation. Research sessions start
as shells and enter wards. Quick chat is always there, untouched. The
sidebar is the workspace navigator. No mode toggle — the session type
determines the pipeline depth.

## The session model

```
SESSIONS:
  chat          "default"          ← ONE persistent, lean pipeline
  research      "cloud-pricing"    ← shell (no ward yet, full pipeline)
  research      "stock-analysis"   ← ward-bound (full + ward context)
  research      "india-analysis"   ← ward-bound
```

Sidebar groups:
- **Chat**: the one "default" session
- **Ward Bots**: research sessions WITH a ward binding (named by ward)
- **Research**: research sessions WITHOUT a ward (shells)
- **[+ New Research]**: creates a new research session

## Session lifecycle

```
[+ New Research] → research session (type=research, ward=nil)
       │
   user sends a message → full pipeline (intent analysis → skills →
   delegation → progress budgets)
       │
   intent recommends a ward → root calls ward_use("stock-analysis")
       │
   session WARDS: ward="stock-analysis"  ← the binding sticks
       │
   sidebar shows: "Stock Analysis" under Ward Bots
   next message: ward context pre-loaded, sticky
       │
   ...user switches to Quick Chat → lean session, untouched
   ...user switches back → same ward session, prior research intact
```

## What survives from the toggle implementation

| Piece | Status | Change |
|---|---|---|
| `domain.ChatMode` | ✅ keep | becomes the session TYPE enum |
| sessions `mode` column | ✅ keep | becomes `type` + add `ward` column |
| `SendMode` / `researchPath` seam | ✅ keep | routes by session type, not request field |
| Continue endpoint | ✅ keep | unchanged |
| Task Analysis card rendering | ✅ keep | renders from intent_analysis events |
| Ward tools / progress / delegation | ✅ keep | already session-scoped |

## What's new

1. **Session CRUD**: GET /api/sessions returns sessions with type +
   ward; POST /api/sessions creates a research session; the sidebar
   renders from this.
2. **Session-scoped chat-send**: the send carries session_id; the
   service looks up the session's type + ward and routes
   accordingly.
3. **Ward binding**: when ward_use/ward_create fires in a research
   session, the session record's ward column updates; the sidebar
   re-groups.
4. **The context panel**: a third column showing the active session's
   ward (canonical page, files), provisioned skills, agent roster.
5. **Sidebar redesign**: sessions replace the current nav items for
   the chat surface.

## Decisions

1. **Session type is immutable**: a chat session stays chat; a
   research session stays research. Ward binding is mutable (enters/
   leaves wards). This avoids mode-thrash entirely.
2. **One ward session per ward**: ward_use on a session that already
   has a ward binding SWITCHES the binding; ward_use on a DIFFERENT
   research session that targets the same ward — both sessions can
   work in the same ward (the vault is a shared workspace); but the
   sidebar groups by ward, so they appear together.
3. **The chat surface renders the ACTIVE session**: switching sessions
   in the sidebar switches the transcript + composer context. One
   Chat component, session-driven.
4. **Research sessions have names**: auto-titled from the first
   message's intent, or user-renamed. The sidebar shows the name +
   last-activity hint.
5. **Chat sessions are NOT deletable** (the persistent quick chat);
   research sessions can be archived/deleted by the user.

## Risks / Trade-offs

- [Multiple live sessions] — each research session holds engine
  state (ward host, progress tracker). Memory-bounded by session
  count; ward hosts are small (a few KB each).
- [Session list growth] — many research sessions over time. Sidebar
  caps at ~20 visible + "show all"; archived sessions hidden.

## Migration Plan

Additive: the existing "default" session becomes type=chat; no other
sessions exist yet. The mode column from the earlier toggle work
becomes the type column. No data moves.

## Open Questions

- Should ward-bound sessions be renameable independently of the ward
  name? (Mock shows ward name as the session name.)
