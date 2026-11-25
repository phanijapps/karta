// The centralized SSE Timeline Tree store (runes-based, per the
// frontend constitution): one EventSource per active session over
// GET /api/sessions/{id}/events — durable replay first, then the live
// tail. The research chat renders the live work stream from this;
// every event carries a stable id (reconnect replay dedupes).
import type { components } from "../api/schema";
import type { IntentAnalysisPayload } from "../api/client";

/** The contract's TimelineEventKind (regenerated) — the closed set the
 * daemon streams; re-exported for consumers that want it. */
export type TimelineEventKind = components["schemas"]["TimelineEventKind"];
/** The contract's ward transition payload (ward_entered/ward_created). */
export type WardEventPayload = components["schemas"]["WardEventPayload"];

export type TimelineEvent = {
  id: string;
  kind: TimelineEventKind | string;
  text: string;
  intentAnalysis?: IntentAnalysisPayload | Record<string, unknown>;
  ward?: WardEventPayload;
  receivedAt: number;
};

// Live-view bound: the transcript is the durable truth; the stream is
// a bounded volatile view.
const MAX_EVENTS = 200;

export function sessionEventsUrl(sessionId: string): string {
  return `/api/sessions/${encodeURIComponent(sessionId)}/events`;
}

export function createTimeline() {
  let events = $state<TimelineEvent[]>([]);
  let connected = $state(false);
  let source: EventSource | null = null;
  let generation = 0;
  const seen = new Set<string>();

  function push(ev: TimelineEvent) {
    if (seen.has(ev.id)) return; // reconnect replay dedupe
    seen.add(ev.id);
    events = [...events, ev];
    if (events.length > MAX_EVENTS) {
      events = events.slice(events.length - MAX_EVENTS);
    }
  }

  /** Connect to the session's event stream (replaces any prior one). */
  function connect(sessionId: string) {
    close();
    const mine = ++generation;
    const es = new EventSource(sessionEventsUrl(sessionId));
    source = es;
    const handle = (kind: string) => (msg: MessageEvent<string>) => {
      if (mine !== generation) return; // stale connection
      try {
        const raw = JSON.parse(msg.data) as {
          id?: string;
          kind?: string;
          text?: string;
          intent_analysis?: IntentAnalysisPayload;
          ward?: WardEventPayload;
        };
        push({
          id: raw.id ?? `${kind}-${msg.lastEventId || Math.random()}`,
          kind: raw.kind ?? kind,
          text: raw.text ?? "",
          intentAnalysis: raw.intent_analysis,
          ward: raw.ward,
          receivedAt: Date.now(),
        });
      } catch {
        /* malformed frame: skip; the durable stream is the truth */
      }
    };
    const kinds: TimelineEventKind[] = [
      "session",
      "agent_step",
      "tool_call",
      "delegation",
      "hook_decision",
      "compaction_triggered",
      "chitta_tool_invoked",
      "analysis_started",
      "analysis_completed",
      "analysis_bypassed",
      "analysis_failed",
      "ward_entered",
      "ward_created",
    ];
    for (const k of kinds) es.addEventListener(k, handle(k) as EventListener);
    es.onopen = () => {
      if (mine === generation) connected = true;
    };
    es.onerror = () => {
      // EventSource auto-reconnects; a dead daemon just pauses the view.
      if (mine === generation) connected = false;
    };
  }

  /** Close the stream and drop the volatile view. */
  function close() {
    generation++;
    connected = false;
    if (source) {
      source.close();
      source = null;
    }
    events = [];
    seen.clear();
  }

  return {
    connect,
    close,
    get events() {
      return events;
    },
    get connected() {
      return connected;
    },
  };
}

export type Timeline = ReturnType<typeof createTimeline>;
