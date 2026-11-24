// The session model (add-chat-modes): the session roster (Chat /
// Ward Bots / Research shells), the ACTIVE session, and per-session
// transcript state. The session's type IS the mode — there is no
// toggle anywhere; switching sessions switches the conversation.
import {
  createSession,
  getDefaultModel,
  getSessionState,
  listSessions,
  syncChatMessages,
  type ChatMessage,
  type SessionAnalysis,
  type SessionView,
} from '../../shared/api/client';

export type SessionGroups = {
  chat: SessionView | null;
  wardBots: SessionView[];
  research: SessionView[];
};

export function createSessions() {
  let sessions = $state<SessionView[]>([]);
  let activeId = $state('default');
  let transcripts = $state<Record<string, ChatMessage[]>>({});
  let cursors = $state<Record<string, number>>({});
  let model = $state(''); // catalog-backed picker sets this
  let loading = $state(true);
  let rosterError = $state('');
  // The ACTIVE session's latest intent analysis (the state surface —
  // the analysis card and the context panel render from this, never
  // from transcript parsing).
  let analysis = $state<SessionAnalysis | null>(null);
  // Analysis generation: only the LATEST request's response may land
  // (switching sessions mid-fetch must not leak another session's
  // analysis into the active card/panel).
  let analysisGeneration = 0;
  let sendError = $state('');

  const groups = $derived.by<SessionGroups>(() => {
    const g: SessionGroups = { chat: null, wardBots: [], research: [] };
    for (const s of sessions) {
      if (s.id === 'default') g.chat = s; // the ONE quick chat
      else if (s.type === 'research') {
        if (s.ward) g.wardBots.push(s);
        else g.research.push(s);
      }
      // Legacy chat-typed engine sessions (pre-migration goal runs)
      // stay out of the navigator until run-state surfacing lands.
    }
    return g;
  });
  const active = $derived.by<SessionView | null>(
    () => sessions.find((s) => s.id === activeId) ?? null,
  );
  const isResearch = $derived.by(() => active?.type === 'research');
  const activeWard = $derived.by(() => active?.ward ?? '');

  async function loadRoster() {
    let all: SessionView[] | null = null;
    // Page the roster until complete (bounded: 5 pages x 100 — beyond
    // that the cap holds and a "show all" control lands later).
    for (let page = 0; page < 5; page++) {
      const res = await listSessions(100, page > 0 ? String(page * 100) : undefined);
      if (!res.ok) break;
      all = page === 0 ? res.sessions : [...(all ?? []), ...res.sessions];
      if (!res.truncated) break;
    }
    if (all !== null) {
      sessions = all;
      rosterError = '';
    } else {
      rosterError = 'daemon';
      sessions = [];
    }
    // The quick chat is ALWAYS there — locally injected when the daemon
    // omits it or the roster call fails (it is created on first send).
    if (!sessions.some((s) => s.id === 'default')) {
      sessions = [
        {
          id: 'default',
          type: 'chat',
          status: 'created',
          updated_at: new Date().toISOString(),
        },
        ...sessions,
      ];
    }
  }

  async function loadTranscript(sessionId: string) {
    // FULL reload from seq 0: mounting replaces the cache with the
    // complete history (an incremental read from the saved cursor
    // would blank a remounted Chat when no new rows exist).
    let cursor = 0;
    const rows: ChatMessage[] = [];
    for (;;) {
      const page = await syncChatMessages(cursor, 200, sessionId);
      rows.push(...page.messages);
      cursor = page.nextCursor;
      if (!page.truncated) break;
    }
    cursors = { ...cursors, [sessionId]: cursor };
    transcripts = { ...transcripts, [sessionId]: rows };
  }

  /** Initial load: roster + the active session's transcript. */
  async function load() {
    loading = true;
    sendError = '';
    try {
      try {
        const model0 = await getDefaultModel();
        if (model0 && model === '') model = model0;
      } catch {
        /* non-critical */
      }
      await loadRoster();
      await loadTranscript(activeId);
      await loadAnalysis(activeId);
    } catch {
      sendError = 'daemon';
    } finally {
      loading = false;
    }
  }

  /** Refresh the active session's analysis from the state surface. */
  async function loadAnalysis(sessionId: string) {
    const mine = ++analysisGeneration;
    const state = await getSessionState(sessionId);
    if (mine !== analysisGeneration || sessionId !== activeId) return; // stale
    analysis = state?.intent_analysis ?? null;
  }

  /** Switch the active session (loads its transcript on first visit). */
  async function select(sessionId: string) {
    if (sessionId === activeId) return;
    activeId = sessionId;
    sendError = '';
    analysis = null;
    analysisGeneration++; // pending state fetches for the old session are stale
    if (!(sessionId in transcripts)) {
      try {
        await loadTranscript(sessionId);
      } catch {
        sendError = 'daemon';
      }
    }
    void loadAnalysis(sessionId);
  }

  /** [+ New Research]: create a shell session and activate it. */
  async function newResearch(title?: string) {
    const res = await createSession(title);
    if (res.ok) {
      await loadRoster();
      await select(res.session.id);
      return true;
    }
    sendError = res.error;
    return false;
  }

  /** Reconcile after every send outcome, failures included: the
   * transcript re-syncs and the roster re-lists (a ward binding may
   * have changed — the sidebar re-groups). */
  async function reconcile(sessionId = activeId) {
    try {
      const seen = new Set(transcripts[sessionId] ?? []);
      let cursor = cursors[sessionId] ?? 0;
      for (;;) {
        const page = await syncChatMessages(cursor, 200, sessionId);
        for (const m of page.messages) if (!seen.has(m)) seen.add(m);
        cursor = page.nextCursor;
        if (!page.truncated) break;
      }
      cursors = { ...cursors, [sessionId]: cursor };
      transcripts = { ...transcripts, [sessionId]: [...seen] };
      await loadRoster();
      await loadAnalysis(sessionId);
    } catch {
      /* next load() surfaces daemon state */
    }
  }

  /** Clear the ACTIVE session's transcript locally after a daemon clear. */
  function clearedLocally(sessionId: string) {
    transcripts = { ...transcripts, [sessionId]: [] };
    cursors = { ...cursors, [sessionId]: 0 };
  }

  return {
    load,
    loadRoster,
    select,
    newResearch,
    reconcile,
    clearedLocally,
    get groups() { return groups; },
    get sessions() { return sessions; },
    get activeId() { return activeId; },
    get active() { return active; },
    get isResearch() { return isResearch; },
    get activeWard() { return activeWard; },
    get messages() { return transcripts[activeId] ?? []; },
    get cursor() { return cursors[activeId] ?? 0; },
    get model() { return model; },
    set model(value: string) { model = value; },
    get loading() { return loading; },
    get rosterError() { return rosterError; },
    get analysis() { return analysis; },
    get sendError() { return sendError; },
    set sendError(value: string) { sendError = value; },
  };
}

export type Sessions = ReturnType<typeof createSessions>;
