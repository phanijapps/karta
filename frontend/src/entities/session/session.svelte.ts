// The session model (add-chat-modes): the session roster (Chat /
// Ward Bots / Research shells), the ACTIVE session, and per-session
// transcript state. The session's type IS the mode — there is no
// toggle anywhere; switching sessions switches the conversation.
import {
  createSession,
  ensureWarden,
  leaveWard,
  getDefaultModel,
  getSessionState,
  listSessions,
  listWards,
  syncChatMessages,
  type ChatMessage,
  type SessionAnalysis,
  type SessionExecution,
  type SessionView,
} from '../../shared/api/client';

/** One UNIQUE ward row: the ward plus its sessions (recency-ordered —
 * index 0 is the last executed). The warden (ward-<name>) is tagged. */
export type WardGroup = {
  ward: string;
  title: string;
  sessions: { session: SessionView; isWarden: boolean }[];
  /** The session to activate when the ward is selected (the last
   * executed; the warden when it is the only one). */
  latestId: string;
};

export type SessionGroups = {
  chat: SessionView | null;
  wardBots: WardGroup[];
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
  // The ward roster (v2): wards WITHOUT a live session still render as
  // rows — selecting one ensures the ward's warden session.
  let wards = $state<{ name: string; title?: string }[]>([]);
  // The EXPANDED ward (its sessions render under it). Null = collapsed.
  let expandedWard = $state<string | null>(null);
  // The ACTIVE session's latest intent analysis (the state surface —
  // the analysis card and the context panel render from this, never
  // from transcript parsing).
  let analysis = $state<SessionAnalysis | null>(null);
  // The session's execution rows (root, analyst, children) with their
  // cost — the Work tab's run footer (run-legibility).
  let executions = $state<SessionExecution[]>([]);
  // Analysis generation: only the LATEST request's response may land
  // (switching sessions mid-fetch must not leak another session's
  // analysis into the active card/panel).
  let analysisGeneration = 0;
  let sendError = $state('');

  const groups = $derived.by<SessionGroups>(() => {
    // Ward name → its bound sessions, RECENCY-ORDERED (the roster is
    // recency-sorted globally, so insertion preserves that order —
    // index 0 is the ward's last-executed session).
    const byWard = new Map<string, SessionView[]>();
    const g: SessionGroups = { chat: null, wardBots: [], research: [] };
    for (const s of sessions) {
      if (s.id === 'default') g.chat = s; // the ONE quick chat
      else if (s.type === 'research') {
        if (s.ward) byWard.set(s.ward, [...(byWard.get(s.ward) ?? []), s]);
        else g.research.push(s);
      }
      // Legacy chat-typed engine sessions (pre-migration goal runs)
      // stay out of the navigator until run-state surfacing lands.
    }
    // ONE row per ward (unique): the ward + its sessions nested; the
    // latest session is the selection target. Wards absent from the
    // roster but holding sessions still group (deleted vault entries).
    const rosterNames = new Set(wards.map((w) => w.name));
    const names = new Set<string>([...rosterNames, ...byWard.keys()]);
    for (const name of names) {
      const bound = byWard.get(name) ?? [];
      const meta = wards.find((w) => w.name === name);
      g.wardBots.push({
        ward: name,
        title: meta?.title ?? name,
        sessions: bound.map((s) => ({ session: s, isWarden: s.id === `ward-${name}` })),
        latestId: bound[0]?.id ?? '',
      });
    }
    // Most recently used ward first (its newest session's timestamp).
    const at = (w: WardGroup) => w.sessions[0]?.session.updated_at ?? '';
    g.wardBots.sort((a, b) => (at(b) ?? '').localeCompare(at(a) ?? ''));
    return g;
  });
  const active = $derived.by<SessionView | null>(
    () => sessions.find((s) => s.id === activeId) ?? null,
  );
  // A bound active session auto-expands its ward (selecting any
  // session of a ward reveals its siblings) — an EFFECT, never a
  // state write inside the derived.
  $effect(() => {
    if (active?.ward) expandedWard = active.ward;
  });
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
    // The ward roster rides along (fail-soft: wards are enrichment).
    try {
      const roster = await listWards();
      if (roster) {
        wards = roster.wards
          .map((w) => ({ name: w.name, title: w.canonical_title ?? undefined }));
      }
    } catch {
      /* wards are enrichment; sessions stand alone */
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
    if (sessionId !== activeId) return; // switched away
    // The cost always applies (a live analysis event cannot invalidate
    // it); the analysis itself yields to a live event that landed while
    // this fetch was in flight.
    executions = state?.executions ?? [];
    if (mine !== analysisGeneration) return; // a live analysis won
    analysis = state?.intent_analysis ?? null;
  }

  /** Switch the active session (loads its transcript on first visit). */
  async function select(sessionId: string) {
    if (sessionId === activeId) return;
    activeId = sessionId;
    sendError = '';
    analysis = null;
    executions = [];
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

  /** Select a WARD (its unique row): expand it and activate its LAST
   * EXECUTED session — the warden is ensured only when the ward has
   * no sessions at all. Selecting a session row picks that one. */
  async function selectWard(ward: string) {
    expandedWard = ward;
    const group = groups.wardBots.find((g) => g.ward === ward);
    if (group && group.latestId) {
      await select(group.latestId);
      return;
    }
    const res = await ensureWarden(ward);
    if (res.ok) {
      await loadRoster();
      expandedWard = ward;
      await select(res.sessionId);
    } else {
      sendError = res.error;
    }
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

  /** Apply a LIVE intent-analysis event (the SSE surface): the card
   * renders the moment analysis lands, not at run completion. */
  function applyLiveAnalysis(payload: Record<string, unknown> | undefined) {
    if (!payload) return;
    analysisGeneration++; // a pending state fetch is now stale
    analysis = payload as SessionAnalysis;
  }

  /** Leave the active session's ward (clears the binding; the session
   * re-groups under Research; the roster refreshes). */
  async function leaveActiveWard() {
    const res = await leaveWard(activeId);
    if (res.ok) {
      await loadRoster();
    } else {
      sendError = res.error;
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
    selectWard,
    leaveActiveWard,
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
    get executions() { return executions; },
    applyLiveAnalysis,
    get expandedWard() { return expandedWard; },
    isRunning(id: string) {
      return sessions.some((s) => s.id === id && s.run === 'running');
    },
    set expandedWard(value: string | null) { expandedWard = value; },
    get sendError() { return sendError; },
    set sendError(value: string) { sendError = value; },
  };
}

export type Sessions = ReturnType<typeof createSessions>;
