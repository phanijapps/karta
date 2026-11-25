<script lang="ts">
  import { tick } from 'svelte';
  import { ArrowUp, Brain, Sparkles, Microscope, FolderOpen, RefreshCw, Info, X } from '@lucide/svelte';
  import { sendChatMessage, streamChatMessage, continueSession } from '../../shared/api/client';
  import { costText, fmtTokens, isTerminalAnalysisKind } from '../../entities/timeline-event/analysisText';
  import { createTimeline } from '../../shared/sse/timeline.svelte';
  import { renderMarkdown } from '../../shared/markdown/render';
  import type { Sessions } from '../../entities/session/session.svelte';
  import type { ChatMessage, IntentAnalysisPayload } from '../../shared/api/client';
  let { sessions, onresearch }: { sessions: Sessions; onresearch?: () => void } = $props();
  let draft = $state('');
  let sending = $state(false);
  let transcriptEl: HTMLElement;
  let composerEl: HTMLTextAreaElement;
  // Live stream state (volatile view; the transcript stays the truth).
  let reasoning = $state('');
  let content = $state('');
  let streaming = $state(false);
  let thinkingOpen = $state(true);
  let announced = $state('');
  // Continue bar (research sessions near budget).
  let budgetRemaining = $state<number | null>(null);
  // The session a send originated in: volatile output and the pending
  // bubble render ONLY while that session is active (switching away
  // mid-send must not leak the live exchange into another transcript).
  let sendSessionId = $state('');
  // The live work stream (research runs): the session's Timeline SSE —
  // steps, tool calls, delegations, the analysis the moment it lands.
  const timeline = createTimeline();
  // One-shot load (cursor changes inside load() must not retrigger).
  let loaded = $state(false);
  $effect(() => {
    if (loaded) return;
    loaded = true;
    void sessions.load();
  });
  async function scrollToLatest() {
    await tick();
    transcriptEl?.scrollTo({ top: transcriptEl.scrollHeight });
  }
  $effect(() => {
    sessions.messages;
    void scrollToLatest();
  });
  // The run's latest narration (agent_step text) — the main thread's
  // live "thinking" while the work itself fills the sidebar.
  const latestThought = $derived.by<string>(() => {
    for (let i = timeline.events.length - 1; i >= 0; i--) {
      const ev = timeline.events[i];
      if (ev.kind === 'agent_step' && ev.text && !ev.text.startsWith('engine:') && !ev.text.startsWith('Pre-execution')) return ev.text;
    }
    return '';
  });
  // Live analysis: the terminal SSE analysis_* events render the card
  // the moment the decision lands (mid-run), not at completion. The
  // started event only feeds the work stream.
  // Each event is handled once: a replayed OLD terminal event must not
  // release the pending state of a NEW run (the effect re-runs on every
  // push and re-iterates the whole list).
  const handledAnalysis = new Set<string>();
  // Pending from send start until this run's terminal analysis lands —
  // the chip shows "Analyzing…" instead of the previous decision.
  let analysisPending = $state(false);
  $effect(() => {
    for (const ev of timeline.events) {
      if (handledAnalysis.has(ev.id)) continue;
      handledAnalysis.add(ev.id);
      if (isTerminalAnalysisKind(ev.kind) && ev.intentAnalysis) {
        analysisPending = false;
        sessions.applyLiveAnalysis(ev.intentAnalysis as Record<string, unknown>);
      }
    }
  });
  // Session switch / unmount: drop the stream.
  $effect(() => {
    sessions.activeId;
    return () => timeline.close();
  });
  // Research sessions show their work in the right sidebar: open it
  // (once, on wide screens) when a research session becomes active.
  $effect(() => {
    if (sessions.isResearch) onresearch?.();
  });
  const canSend = $derived(draft.trim() !== '' && !sending);
  const isResearch = $derived(sessions.isResearch);
  // Turns: the transcript split at USER rows — each turn carries the
  // rows that follow (tool intents + results → steps; final assistant
  // content → answers). Children's injected user rows (delegation
  // context) are absorbed into the preceding turn so the step tree
  // nests them instead of breaking the view.
  type Turn = { key: string; user: ChatMessage | null; rows: ChatMessage[]; answers: ChatMessage[]; live: boolean };
  const turns = $derived.by<Turn[]>(() => {
    const out: Turn[] = [];
    let cur: Turn | null = null;
    for (const m of sessions.messages) {
      if (m.role === 'user') {
        // A user row right after a delegate_agent intent is the child's
        // injected context — absorb it, don't start a turn.
        const prevIntent = cur?.rows[cur.rows.length - 1];
        if (cur && prevIntent?.tool_calls && prevIntent.tool_calls.includes('"delegate_agent"')) {
          cur.rows.push(m);
          continue;
        }
        cur = { key: m.id, user: m, rows: [], answers: [], live: false };
        out.push(cur);
        continue;
      }
      if (!cur) continue;
      if (m.role === 'assistant' && !m.tool_calls && (m.content || '').trim() !== '') cur.answers.push(m);
      else cur.rows.push(m);
    }
    if (out.length) out[out.length - 1].live = sending && sessions.activeId === sendSessionId;
    return out;
  });
  // The Task Analysis popover (the (i) chip): open state + the
  // trigger ref for focus return on close.
  let analysisOpen = $state(false);
  let analysisTrigger = $state<HTMLButtonElement | null>(null);
  const analysisAvailable = $derived(isResearch && !analysisPending && sessions.analysis !== null && !['bypassed', 'started'].includes((sessions.analysis as Record<string, any>).status));
  const analysisCost = $derived(costText((sessions.analysis ?? {}) as Partial<IntentAnalysisPayload>));
  $effect(() => {
    if (analysisOpen) void tick().then(() => document.getElementById('analysis-popover')?.focus());
  });
  async function closeAnalysis() {
    analysisOpen = false;
    await tick();
    analysisTrigger?.focus();
  }
  const ward = $derived(sessions.activeWard);

  /** Delegations from assistant tool-call intents: agent + task pairs
   * (the delegation roster; results close them). */



  async function send() {
    const text = draft.trim();
    if (!text || sending) return;
    sending = true;
    if (isResearch) analysisPending = true;
    streaming = true;
    reasoning = '';
    content = '';
    thinkingOpen = false; // the brain: collapsed by default, click to open
    announced = '';
    budgetRemaining = null;
    sessions.sendError = '';
    const sessionId = sessions.activeId;
    sendSessionId = sessionId;
    draft = ''; // clear at send start — a multi-minute run never holds the text
    try {
      let result: Awaited<ReturnType<typeof sendChatMessage>>;
      if (isResearch) {
        // Research sessions complete as units — the LIVE work streams as
        // Timeline events (steps, tools, delegations) while the run
        // works; the roster re-lists periodically so sidebar badges
        // (● working) follow the run.
        timeline.connect(sessionId);
        const rosterTick = setInterval(() => void sessions.loadRoster(), 4000);
        try {
          result = await sendChatMessage(text, sessions.model, sessionId);
        } finally {
          clearInterval(rosterTick);
        }
        timeline.close();
      } else {
        try {
          result = await streamChatMessage(text, sessions.model, (d) => {
            if (d.kind === 'reasoning') {
              reasoning += d.text;
            } else {
              content += d.text;
            }
          }, sessionId);
        } catch {
          result = await sendChatMessage(text, sessions.model, sessionId);
        }
      }
      await sessions.reconcile(sessionId);
      if (!result.ok) {
        sessions.sendError = result.errorMessage ?? 'send failed';
        announced = 'Reply failed.';
      } else {
        announced = 'Reply complete.';
      }
    } finally {
      sending = false;
      streaming = false;
    }
  }
  function onkeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      void send();
    }
  }
  /** Leave the active session's ward, then hand focus to the composer
   * (the strip — and the focused button — disappears on success). */
  async function leaveWard() {
    const sid = sessions.activeId;
    const leftName = ward; // capture BEFORE the binding clears (derived)
    await sessions.leaveActiveWard();
    if (sessions.activeId !== sid || sessions.activeWard) return; // failed or switched
    announced = `Left ${leftName}. The session moved under Research.`;
    await tick();
    composerEl?.focus();
  }
  async function extend() {
    const res = await continueSession(sessions.activeId, 25);
    if (res.ok) {
      budgetRemaining = (budgetRemaining ?? 0) + res.granted;
      announced = `Extended by ${res.granted} iterations.`;
    }
  }
  const sessionPlaceholder = $derived(
    isResearch
      ? ward ? `Continue researching in ${ward}…` : 'Research question — the pipeline analyzes, enters wards, delegates…'
      : sessions.model === '' ? 'Pick a model first…' : 'Message Karta',
  );
</script>

<div class="chat-region" class:research={isResearch}>
  {#if sessions.sendError === 'daemon'}
    <div class="chat-banner" role="alert">
      <strong>Daemon unreachable.</strong> Start it with <code>go run ./cmd/zbotd</code> from <code>backend/</code>; your transcript is safe locally.
    </div>
  {:else if sessions.sendError}
    <div class="chat-banner" role="alert">{sessions.sendError}</div>
  {/if}
  <span class="sr-only" aria-live="polite">{announced}</span>
  <div class="transcript" bind:this={transcriptEl} aria-label="Conversation transcript">
    {#if isResearch}
      <!-- Task Analysis: a compact chip + (i) — the analysis popover
           (intent, complexity, budget, approach, ward, skills, dropped,
           degradation reason) fed by SESSION STATE + the live SSE
           event. Shown for every research session, pending or not. -->
      <div class="analysis-chip-row">
        <span class="analysis-chip" class:pending={!analysisAvailable}>
          <Microscope size={13} aria-hidden="true" />
          {#if analysisAvailable}
            <span class="chip-intent">{(sessions.analysis as Record<string, any>).primary_intent || 'Analysis'}</span>
            {#if (sessions.analysis as Record<string, any>).complexity}
              <span class="badge complexity" data-complexity={(sessions.analysis as Record<string, any>).complexity}>{(sessions.analysis as Record<string, any>).complexity}</span>
            {/if}
            {#if (sessions.analysis as Record<string, any>).ward_name}
              <span class="badge ward"><FolderOpen size={11} aria-hidden="true" /> {(sessions.analysis as Record<string, any>).ward_name}</span>
            {/if}
          {:else}
            <span class="chip-intent muted">{sending ? 'Analyzing…' : 'No analysis yet'}</span>
          {/if}
          <button
            type="button"
            class="info-btn"
            bind:this={analysisTrigger}
            aria-label="Task analysis details"
            aria-expanded={analysisOpen}
            aria-controls="analysis-popover"
            disabled={!analysisAvailable}
            onclick={() => (analysisOpen = !analysisOpen)}
          ><Info size={14} aria-hidden="true" /></button>
        </span>
      </div>
    {/if}
    {#if analysisOpen && analysisAvailable}
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div class="analysis-popover-wrap" role="presentation">
        <div id="analysis-popover" class="analysis-popover" role="dialog" aria-label="Task Analysis" tabindex="-1" onkeydown={(e) => { if (e.key === 'Escape') void closeAnalysis(); }}>
          <header class="analysis-head">
            <Microscope size={16} aria-hidden="true" />
            <strong>Task Analysis</strong>
            {#if (sessions.analysis as Record<string, any>).approach}
              <span class="badge approach">{(sessions.analysis as Record<string, any>).approach}</span>
            {/if}
            {#if (sessions.analysis as Record<string, any>).complexity}
              <span class="badge complexity" data-complexity={(sessions.analysis as Record<string, any>).complexity}>{(sessions.analysis as Record<string, any>).complexity}</span>
            {/if}
            {#if (sessions.analysis as Record<string, any>).ward_action}
              <span class="badge ward"><FolderOpen size={11} aria-hidden="true" /> {(sessions.analysis as Record<string, any>).ward_name} · {(sessions.analysis as Record<string, any>).ward_action}</span>
            {/if}
            <button type="button" class="icon-button pop-close" aria-label="Close task analysis" onclick={() => void closeAnalysis()}><X size={16} /></button>
          </header>
          <dl class="analysis-meta">
            {#if (sessions.analysis as Record<string, any>).primary_intent}
              <div><dt>Intent</dt><dd>{(sessions.analysis as Record<string, any>).primary_intent}</dd></div>
            {/if}
            {#if (sessions.analysis as Record<string, any>).step_budget}
              <div><dt>Budget</dt><dd>{(sessions.analysis as Record<string, any>).step_budget} steps</dd></div>
            {/if}
            <div><dt>Status</dt><dd>{(sessions.analysis as Record<string, any>).status}</dd></div>
            {#if (sessions.analysis as Record<string, any>).model}
              <div><dt>Model</dt><dd>{(sessions.analysis as Record<string, any>).model}</dd></div>
            {/if}
            {#if analysisCost}
              <div><dt>Cost</dt><dd>{analysisCost}</dd></div>
            {/if}
            {#if (sessions.analysis as Record<string, any>).context_window}
              <div><dt>Window</dt><dd>{fmtTokens((sessions.analysis as Record<string, any>).context_window)}{(sessions.analysis as Record<string, any>).window_source ? ` (${(sessions.analysis as Record<string, any>).window_source})` : ''}{(sessions.analysis as Record<string, any>).trimmed ? ' · packet trimmed to fit' : ''}</dd></div>
            {/if}
          </dl>
          {#if (sessions.analysis as Record<string, any>).recommended_skills?.length || (sessions.analysis as Record<string, any>).dropped?.length}
            <div class="skills-row" aria-label="Provisioned skills">
              {#each (sessions.analysis as Record<string, any>).recommended_skills ?? [] as s}
                <span class="skill-chip">{s}</span>
              {/each}
              {#each (sessions.analysis as Record<string, any>).dropped ?? [] as s}
                <span class="skill-chip dropped" title="Recommended but not in the index — dropped">{s} ✕ dropped</span>
              {/each}
            </div>
          {/if}
          {#if (sessions.analysis as Record<string, any>).status === 'failed' && (sessions.analysis as Record<string, any>).reason}
            <p class="muted analysis-note">Analysis degraded: {(sessions.analysis as Record<string, any>).reason}</p>
          {/if}
        </div>
      </div>
    {/if}
    {#if sessions.loading}
      <div class="chat-empty"><Sparkles size={28} strokeWidth={1.2} /><p class="muted">Loading the conversation…</p></div>
    {:else if sessions.messages.length === 0}
      <div class="chat-empty">
        <Sparkles size={28} strokeWidth={1.2} />
        {#if isResearch}
          <h3>A research session</h3>
          <p class="muted">Ask a research question — intent analysis runs, skills are provisioned, wards and delegation come into play. Everything stays local.</p>
        {:else}
          <h3>A conversation that stays yours</h3>
          <p class="muted">One persistent conversation — every message is kept locally, forever. Pick a model above.</p>
        {/if}
      </div>
    {:else}
      {#each turns as turn (turn.key)}
        {#if turn.user}
          <article class="bubble user" aria-label="Your message">{turn.user.content}</article>
        {/if}
        {#each turn.answers as m (m.id)}
          <article class="bubble assistant" aria-label="Assistant message">
            {#if m.reasoning}
              <details class="thinking">
                <summary><Brain size={13} aria-hidden="true" /> Thinking</summary>
                <p class="muted">{m.reasoning}</p>
              </details>
            {/if}
            {@html renderMarkdown(m.content ?? '')}
          </article>
        {/each}
      {/each}
    {/if}
      {#if streaming && sessions.activeId === sendSessionId && (reasoning !== '' || content !== '')}
        <article class="bubble assistant streaming" aria-label="Assistant is responding" aria-busy="true">
          {#if reasoning !== ''}
            <details class="thinking" bind:open={thinkingOpen}>
              <summary><Brain size={13} aria-hidden="true" /> Thinking</summary>
              <p class="muted">{reasoning}</p>
            </details>
          {/if}
          {#if content !== ''}
            <div class="stream-text">{@html renderMarkdown(content)}</div>
          {:else}
            <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
          {/if}
        </article>
      {:else if sending && sessions.activeId === sendSessionId}
        <article class="bubble assistant pending" aria-label="Assistant is working" aria-busy="true">
          {#if isResearch}
            <!-- The LATEST thinking, live: the engine's agent_step events
                 narrate the run; the tool history lives in the sidebar. -->
            {#if latestThought !== ''}
              <details class="thinking live" open>
                <summary><Brain size={13} aria-hidden="true" /> Thinking <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span></summary>
                <p class="muted">{latestThought}</p>
              </details>
            {:else}
              <div class="research-progress">
                <Microscope size={16} aria-hidden="true" />
                <span>Running the full pipeline…</span>
                <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
              </div>
            {/if}
          {:else}
            <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
          {/if}
          <span class="sr-only">Working on your message…</span>
        </article>
      {/if}
  </div>
  <div class="composer-region">
    <!-- Ward context strip (research sessions with a sticky binding) -->
    {#if isResearch && ward}
      <div class="ward-bar" aria-label="Active ward">
        <FolderOpen size={14} aria-hidden="true" />
        <span>{ward}</span>
        <span class="muted">· research</span>
        {#if sessions.activeId.startsWith('ward-')}
          <span class="muted ward-note">· the warden always lives here</span>
        {:else}
          <button type="button" class="leave-ward" aria-label={`Leave ward ${ward}`} onclick={() => void leaveWard()}>Leave</button>
        {/if}
      </div>
    {/if}
    <!-- Continue bar (research sessions near budget) -->
    {#if isResearch && budgetRemaining !== null && budgetRemaining <= 2}
      <div class="continue-bar" role="alert">
        <span>Budget: {budgetRemaining} steps remaining</span>
        <button type="button" class="continue-btn" onclick={() => void extend()}>
          <RefreshCw size={13} aria-hidden="true" /> Continue +25
        </button>
      </div>
    {/if}
    <form class="composer" onsubmit={(e) => { e.preventDefault(); void send(); }}>
      <label class="sr-only" for="composer-input">{isResearch ? 'Research question' : 'Message Karta'}</label>
      <textarea
        id="composer-input"
        bind:this={composerEl}
        bind:value={draft}
        onkeydown={onkeydown}
        placeholder={sessionPlaceholder}
        aria-label={isResearch ? 'Research question' : 'Message Karta'}
        autocomplete="off"
        rows="1"></textarea>
      <button type="submit" class="send" disabled={!canSend} aria-label="Send message" title="Send"><ArrowUp size={18} /></button>
    </form>
    <p class="composer-note">
      {#if isResearch}
        Research · full pipeline — intent analysis, ward tools, delegation, budgets.
      {:else}
        Quick chat · lean — replies come from your selected model via your local daemon, saved locally forever.
      {/if}
    </p>
  </div>
</div>
<style>
  .chat-region { display: grid; grid-template-rows: 1fr auto; min-height: 0; height: 100%; }
  .chat-region.research { --accent: var(--ds-color-primary, #6366f1); }
  .transcript { overflow-y: auto; padding: 1rem clamp(.75rem, 3vw, 2rem); display: grid; gap: .6rem; align-content: start; }
  .chat-empty { display: grid; gap: .5rem; justify-items: center; align-content: center; min-height: 50%; text-align: center; }
  .chat-empty h3 { margin: 0; }
  .chat-banner { margin: .5rem 1rem; padding: .6rem 1rem; border-radius: 8px; border: 1px solid var(--ds-color-danger, #e5484d); background: var(--ds-color-danger-soft, oklch(.3 .06 27)); }
  .bubble { max-width: min(48rem, 90%); padding: .7rem 1rem; border-radius: 14px; line-height: 1.55; overflow-wrap: break-word; }
  .bubble.user { white-space: pre-wrap; }
  /* Rendered markdown inside assistant bubbles */
  .bubble.assistant :global(> :first-child), .bubble.assistant .stream-text :global(> :first-child) { margin-top: 0; }
  .bubble.assistant :global(> :last-child), .bubble.assistant .stream-text :global(> :last-child) { margin-bottom: 0; }
  .bubble.assistant :global(p), .bubble.assistant .stream-text :global(p) { margin: .45rem 0; }
  .bubble.assistant :global(h1), .bubble.assistant :global(h2), .bubble.assistant :global(h3), .bubble.assistant :global(h4) { margin: .9rem 0 .35rem; font-weight: 600; line-height: 1.3; }
  .bubble.assistant :global(h1) { font-size: 1.15rem; }
  .bubble.assistant :global(h2) { font-size: 1.05rem; }
  .bubble.assistant :global(h3), .bubble.assistant :global(h4) { font-size: .95rem; }
  .bubble.assistant :global(ul), .bubble.assistant :global(ol) { margin: .4rem 0; padding-left: 1.3rem; }
  .bubble.assistant :global(li) { margin: .15rem 0; }
  .bubble.assistant :global(li > p) { margin: .1rem 0; }
  .bubble.assistant :global(blockquote) { margin: .5rem 0; padding: .2rem .8rem; border-left: 3px solid var(--ds-color-border, #333); color: var(--ds-color-muted, #aaa); }
  .bubble.assistant :global(code) { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .82em; padding: .1rem .3rem; border-radius: 4px; background: var(--ds-color-field, #1a1a1a); }
  .bubble.assistant :global(pre) { margin: .5rem 0; padding: .6rem .8rem; border-radius: 8px; background: var(--ds-color-field, #1a1a1a); overflow-x: auto; }
  .bubble.assistant :global(pre code) { padding: 0; background: none; font-size: .78rem; }
  .bubble.assistant :global(a) { color: var(--accent, var(--ds-color-primary, #6366f1)); text-decoration: underline; text-underline-offset: 2px; }
  .bubble.assistant :global(a[href^="http"])::after { content: ' ↗'; font-size: .75em; text-decoration: none; }
  .bubble.assistant :global(table) { border-collapse: collapse; margin: .5rem 0; font-size: .86em; display: block; overflow-x: auto; }
  .bubble.assistant :global(th), .bubble.assistant :global(td) { border: 1px solid var(--ds-color-border, #333); padding: .25rem .55rem; text-align: left; }
  .bubble.assistant :global(th) { background: var(--ds-color-hover, #2a2a2a); font-weight: 600; }
  .bubble.assistant :global(hr) { border: none; border-top: 1px solid var(--ds-color-border, #333); margin: .8rem 0; }
  .stream-text { white-space: normal; }
  .bubble.user { justify-self: end; background: var(--ds-color-primary, #3b82f6); color: var(--ds-color-on-primary, #fff); border-bottom-right-radius: 4px; }
  .bubble.assistant { justify-self: start; background: var(--ds-color-surface, #1e1e1e); border: 1px solid var(--ds-color-border, #333); border-bottom-left-radius: 4px; }
  .bubble.pending, .bubble.streaming { min-height: 2.5rem; display: grid; align-content: center; }
  .thinking { margin-bottom: .5rem; font-size: .8rem; color: var(--ds-color-muted, #888); }
  .thinking summary { cursor: pointer; display: inline-flex; gap: .35rem; align-items: center; list-style: none; }
  .thinking.live summary .dots { display: inline-flex; gap: 3px; margin-left: .3rem; }
  .thinking.live summary .dots i { width: 4px; height: 4px; }
  .thinking p { margin: .3rem 0 0; padding: .4rem .6rem; border-left: 2px solid var(--ds-color-border, #333); font-size: .78rem; }
  .stream-text { white-space: pre-wrap; }
  .dots { display: inline-flex; gap: 4px; }
  .dots i { width: 6px; height: 6px; border-radius: 50%; background: var(--ds-color-muted, #888); animation: pulse 1.2s ease-in-out infinite; }
  .dots i:nth-child(2) { animation-delay: .15s; }
  .dots i:nth-child(3) { animation-delay: .3s; }
  @keyframes pulse { 0%, 80%, 100% { opacity: .3; } 40% { opacity: 1; } }
  .thinking summary { cursor: pointer; display: inline-flex; gap: .35rem; align-items: center; list-style: none; }
  /* Task Analysis: compact chip + (i) popover (fed by session state) */
  .analysis-chip-row { display: flex; justify-self: start; max-width: min(48rem, 90%); }
  .analysis-chip { position: relative; display: inline-flex; align-items: center; gap: .4rem; padding: .3rem .4rem .3rem .65rem; border-radius: 999px; border: 1px solid var(--accent, var(--ds-color-primary, #6366f1)); background: var(--ds-color-surface, #1e1e1e); color: var(--accent, var(--ds-color-primary, #6366f1)); font-size: .76rem; }
  .analysis-chip.pending { border-color: var(--ds-color-border, #333); color: var(--ds-color-muted, #888); }
  .analysis-chip .chip-intent { max-width: 22rem; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-weight: 500; }
  .analysis-chip .chip-intent.muted { font-weight: 400; }
  /* Badges on the chip inherit its accent — the green ward badge on the
     indigo chip failed AA contrast in the light theme. */
  .analysis-chip .badge, .analysis-chip .badge.ward,
  .analysis-popover .badge.ward { color: inherit; border-color: currentColor; }
  .analysis-chip .info-btn { display: grid; place-items: center; width: 1.5rem; height: 1.5rem; border-radius: 50%; border: 1px solid currentColor; background: transparent; color: inherit; cursor: pointer; }
  .analysis-chip .info-btn:disabled { opacity: .4; cursor: not-allowed; }
  .analysis-chip .info-btn:hover:not(:disabled), .analysis-chip .info-btn[aria-expanded="true"] { background: var(--accent, var(--ds-color-primary, #6366f1)); color: var(--ds-color-on-primary, #fff); }
  .analysis-popover-wrap { position: relative; display: flex; justify-self: start; width: min(48rem, 90%); }
  .analysis-popover { width: 100%; border: 1px solid var(--accent, var(--ds-color-primary, #6366f1)); border-radius: 12px; padding: .7rem 1rem; display: grid; gap: .5rem; background: var(--ds-color-surface, #1e1e1e); box-shadow: 0 12px 32px rgb(0 0 0 / .35); outline: none; }
  .analysis-popover .pop-close { margin-left: auto; }
  .analysis-popover .analysis-meta { display: grid; grid-template-columns: max-content 1fr; gap: .25rem 1rem; margin: 0; font-size: .8rem; }
  .analysis-popover .analysis-meta div { display: contents; }
  .analysis-popover .analysis-meta dt { color: var(--ds-color-muted, #888); font-size: .68rem; text-transform: uppercase; letter-spacing: .04em; }
  .analysis-popover .analysis-meta dd { margin: 0; }
  .analysis-popover .analysis-note { margin: 0; font-size: .76rem; }
  .badge { display: inline-flex; align-items: center; gap: .25rem; font-size: .7rem; padding: .15rem .55rem; border-radius: 999px; border: 1px solid var(--ds-color-border, #333); }
  .badge.approach { color: var(--accent, #6366f1); }
  .badge.complexity { font-weight: 600; }
  .badge.complexity[data-complexity='S'] { color: var(--ds-color-success, #22c55e); }
  .badge.complexity[data-complexity='M'] { color: var(--ds-color-primary, #3b82f6); }
  .badge.complexity[data-complexity='L'] { color: var(--ds-color-danger, #e5484d); }
  .badge.complexity[data-complexity='XL'] { color: var(--ds-color-danger, #e5484d); font-weight: 700; }
  .badge.ward { color: var(--ds-color-success, #22c55e); }
  .analysis-meta { display: flex; gap: 1.5rem; flex-wrap: wrap; margin: 0; font-size: .8rem; }
  .analysis-meta div { display: grid; gap: .1rem; }
  .analysis-meta dt { color: var(--ds-color-muted, #888); font-size: .68rem; text-transform: uppercase; letter-spacing: .04em; }
  .analysis-meta dd { margin: 0; }
  .skills-row { display: flex; gap: .35rem; flex-wrap: wrap; }
  .skill-chip { display: inline-flex; align-items: center; gap: .25rem; font-size: .7rem; padding: .12rem .5rem; border-radius: 6px; background: color-mix(in oklch, var(--accent, #6366f1) 12%, transparent); color: var(--accent, #6366f1); border: 1px solid color-mix(in oklch, var(--accent, #6366f1) 25%, transparent); }
  /* Dropped chips: explicit label + strikethrough carry the state —
   * NO opacity reduction (whole-element alpha breaks text contrast in
   * the light theme). */
  /* Dropped chips: full-contrast text + strikethrough + the ✕ label carry
     the state (muted text on the tinted chip failed AA in light theme). */
  .skill-chip.dropped { color: var(--ds-color-text, #eee); text-decoration: line-through; }
  /* Delegation tree */
  .research-progress { display: flex; align-items: center; gap: .5rem; color: var(--ds-color-muted, #888); font-size: .85rem; }
  /* The live work stream */
  .composer-region { padding: .75rem clamp(.75rem, 3vw, 2rem) .5rem; display: grid; gap: .4rem; border-top: 1px solid var(--ds-color-border, #333); background: var(--ds-color-sidebar, #161616); min-width: 0; max-width: 100%; overflow: hidden; }
  .ward-bar .leave-ward { margin-left: auto; background: none; border: none; color: var(--ds-color-muted, #888); font-size: .72rem; cursor: pointer; text-decoration: underline; }
  .ward-bar .ward-note { font-size: .68rem; }
  /* The ward strip: full-contrast text; the border + icon carry the ward
     green (green text on the tinted strip failed AA in light theme). */
  .ward-bar { display: flex; align-items: center; gap: .45rem; padding: .35rem .7rem; border-radius: 8px; border: 1px solid color-mix(in oklch, var(--ds-color-success, #22c55e) 40%, transparent); background: color-mix(in oklch, var(--ds-color-success, #22c55e) 10%, transparent); font-size: .8rem; color: var(--ds-color-text, #eee); }
  .ward-bar :global(svg) { color: var(--ds-color-success, #22c55e); }
  .continue-bar { display: flex; align-items: center; justify-content: space-between; padding: .4rem .7rem; border-radius: 8px; border: 1px solid var(--ds-color-danger, #e5484d); font-size: .8rem; }
  .continue-btn { display: inline-flex; align-items: center; gap: .3rem; padding: .25rem .7rem; border-radius: 6px; border: 1px solid var(--ds-color-border, #333); background: var(--ds-color-surface, #1e1e1e); color: inherit; font-size: .78rem; cursor: pointer; }
  .continue-btn:hover { background: var(--ds-color-hover, #2a2a2a); }
  .composer { display: flex; align-items: flex-end; gap: .5rem; padding: .5rem; border-radius: 14px; border: 1px solid var(--ds-color-border, #333); background: var(--ds-color-field, #1a1a1a); min-width: 0; max-width: 100%; box-sizing: border-box; }
  .composer:focus-within { border-color: var(--accent, var(--ds-color-focus, #2563eb)); }
  .composer textarea { flex: 1; min-width: 0; resize: none; border: none; background: transparent; color: inherit; font: inherit; padding: .3rem 0; outline: none; max-height: 10rem; }
  .composer textarea::placeholder { color: var(--ds-color-muted, #666); }
  .send { display: grid; place-items: center; width: 2.2rem; height: 2.2rem; border-radius: 10px; border: none; background: var(--accent, var(--ds-color-primary, #3b82f6)); color: var(--ds-color-on-primary, #fff); cursor: pointer; }
  .send:disabled { opacity: .4; cursor: not-allowed; }
  .composer-note { margin: 0; font-size: .72rem; color: var(--ds-color-muted, #666); text-align: center; }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
</style>
