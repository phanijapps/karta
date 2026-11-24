<script lang="ts">
  import { tick } from 'svelte';
  import { ArrowUp, Brain, Sparkles, Wrench, Microscope, FolderOpen, ChevronDown, RefreshCw, Users } from '@lucide/svelte';
  import { sendChatMessage, streamChatMessage, continueSession } from '../../shared/api/client';
  import { createTimeline } from '../../shared/sse/timeline.svelte';
  import type { Sessions } from '../../entities/session/session.svelte';
  import type { ChatMessage } from '../../shared/api/client';
  let { sessions }: { sessions: Sessions } = $props();
  let draft = $state('');
  let sending = $state(false);
  let transcriptEl: HTMLElement;
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
  // Live analysis: the SSE intent_analysis event renders the card the
  // moment the analysis lands (mid-run), not at completion.
  $effect(() => {
    for (const ev of timeline.events) {
      if (ev.kind === 'intent_analysis' && ev.intentAnalysis) {
        sessions.applyLiveAnalysis(ev.intentAnalysis as Record<string, unknown>);
      }
    }
  });
  // Session switch / unmount: drop the stream.
  $effect(() => {
    sessions.activeId;
    return () => timeline.close();
  });
  const canSend = $derived(draft.trim() !== '' && !sending);
  const analysisCard = $derived.by<Record<string, any> | null>(() => {
    const a = sessions.analysis as Record<string, any> | null;
    if (!a || !sessions.isResearch) return null;
    if (a.status === 'bypassed') return null; // trivial goal: no card
    return a;
  });
  const isResearch = $derived(sessions.isResearch);
  const ward = $derived(sessions.activeWard);

  /** Delegations from assistant tool-call intents: agent + task pairs
   * (the delegation roster; results close them). */
  function delegationsOf(m: ChatMessage): { agent: string; task: string }[] {
    if (!m.tool_calls) return [];
    try {
      const calls = JSON.parse(m.tool_calls) as Array<{ function?: { name?: string; arguments?: string } }>;
      const out: { agent: string; task: string }[] = [];
      for (const c of calls) {
        if (c.function?.name !== 'delegate_agent') continue;
        try {
          const args = JSON.parse(c.function.arguments ?? '{}') as { agent?: string; task?: string };
          if (args.agent) out.push({ agent: args.agent, task: args.task ?? '' });
        } catch { /* malformed arguments: skip */ }
      }
      return out;
    } catch {
      return [];
    }
  }

  /** Render one live Timeline event as a work-stream row (null =
   * filtered noise). */
  function workRow(ev: { kind: string; text: string; intentAnalysis?: unknown }): {
    icon: string; kind: string; text: string; tone: string; more?: string;
  } | null {
    const clamp = (s: string, n: number) => (s.length > n ? s.slice(0, n) + '…' : s);
    switch (ev.kind) {
      case 'intent_analysis': {
        const ia = (ev.intentAnalysis ?? {}) as Record<string, any>;
        return {
          icon: '🔬', tone: 'analysis', kind: 'analysis',
          text: ia.primary_intent ? `${ia.primary_intent} · ${ia.complexity ?? '?'} · budget ${ia.step_budget ?? '?'}` : 'analysis complete',
        };
      }
      case 'agent_step':
        if (!ev.text) return null;
        if (ev.text.startsWith('engine:')) return { icon: '⚙', tone: 'system', kind: 'engine', text: clamp(ev.text, 160) };
        if (ev.text.startsWith('Pre-execution')) return null;
        return { icon: '🧠', tone: 'step', kind: 'step', text: clamp(ev.text, 240) };
      case 'tool_call':
        return { icon: '🔧', tone: 'tool', kind: 'tool', text: ev.text || 'tool', more: undefined };
      case 'delegation':
        return { icon: '👥', tone: 'delegation', kind: 'delegated', text: clamp(ev.text, 160) };
      case 'session':
        if (ev.text === 'run completed' || ev.text.startsWith('goal:')) return null;
        return { icon: '•', tone: 'system', kind: 'run', text: clamp(ev.text, 120) };
      case 'chitta_tool_invoked':
        return { icon: '🧬', tone: 'tool', kind: 'memory', text: clamp(ev.text, 120) };
      default:
        return null;
    }
  }

  /** The tool names an assistant tool-call intent row requests. */
  function toolCallNames(m: ChatMessage): string[] {
    try {
      const calls = JSON.parse(m.tool_calls ?? '') as Array<{ function?: { name?: string } }>;
      return calls.map((c) => c.function?.name ?? 'tool');
    } catch {
      return ['tool'];
    }
  }

  async function send() {
    const text = draft.trim();
    if (!text || sending) return;
    sending = true;
    streaming = true;
    reasoning = '';
    content = '';
    thinkingOpen = true;
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
        // Timeline events (steps, tools, delegations) while the run works.
        timeline.connect(sessionId);
        result = await sendChatMessage(text, sessions.model, sessionId);
        timeline.close();
      } else {
        try {
          result = await streamChatMessage(text, sessions.model, (d) => {
            if (d.kind === 'reasoning') {
              reasoning += d.text;
            } else {
              if (content === '' && reasoning !== '') thinkingOpen = false;
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
    {#if analysisCard}
      <!-- Task Analysis card: the research exchange header, rendered
             from SESSION STATE (never transcript parsing) — shown even
             while the first exchange is still pending. -->
      <article class="analysis-card" aria-label="Task Analysis">
          <header class="analysis-head">
            <Microscope size={16} aria-hidden="true" />
            <strong>Research</strong>
            {#if analysisCard.approach}
              <span class="badge approach">{analysisCard.approach}</span>
            {/if}
            {#if analysisCard.complexity}
              <span class="badge complexity" data-complexity={analysisCard.complexity}>{analysisCard.complexity}</span>
            {/if}
            {#if analysisCard.ward_name}
              <span class="badge ward"><FolderOpen size={11} aria-hidden="true" /> {analysisCard.ward_name}</span>
            {/if}
          </header>
          {#if analysisCard.primary_intent}
            <dl class="analysis-meta">
              <div><dt>Intent</dt><dd>{analysisCard.primary_intent}</dd></div>
              {#if analysisCard.step_budget}
                <div><dt>Budget</dt><dd>{analysisCard.step_budget} steps</dd></div>
              {/if}
            </dl>
          {/if}
          {#if analysisCard.recommended_skills?.length || analysisCard.dropped?.length}
            <div class="skills-row" aria-label="Provisioned skills">
              {#each analysisCard.recommended_skills ?? [] as s}
                <span class="skill-chip">{s}</span>
              {/each}
              {#each analysisCard.dropped ?? [] as s}
                <span class="skill-chip dropped" title="Recommended but not in the index — dropped">{s} ✕ dropped</span>
              {/each}
            </div>
          {/if}
          {#if analysisCard.status === 'failed' && analysisCard.reason}
            <p class="muted analysis-note">Analysis degraded: {analysisCard.reason}</p>
          {/if}
        </article>
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
      {#each sessions.messages as m (m.id)}
        {#if m.role === 'tool'}
          <article class="tool-row" aria-label={`Tool ${m.tool_name ?? 'call'}`}>
            <Wrench size={14} />
            <span class="tool-name">{m.tool_name ?? 'tool'}</span>
            <details class="tool-detail">
              <summary><ChevronDown size={12} aria-hidden="true" /></summary>
              <code class="tool-output">{m.content}</code>
            </details>
          </article>
        {:else if m.role === 'assistant' && delegationsOf(m).length > 0}
          <!-- Delegation tree: the delegate calls this exchange spawned -->
          <article class="del-tree" aria-label="Delegation">
            <p class="del-title"><Users size={12} aria-hidden="true" /> Delegation</p>
            {#each delegationsOf(m) as d}
              <p class="del-node">
                <span class="dot done" aria-hidden="true"></span>
                <strong>{d.agent}</strong>
                {#if d.task}<span class="muted">— {d.task}</span>{/if}
              </p>
            {/each}
          </article>
        {:else if m.role === 'assistant' && m.tool_calls && !m.content}
          <!-- Assistant tool-call INTENT row (no prose): compact, named -->
          <article class="tool-row" aria-label="Tool call intent">
            <Wrench size={14} />
            <span class="tool-name">{toolCallNames(m).join(', ')}</span>
          </article>
        {:else}
          <article class="bubble {m.role}" aria-label={m.role === 'user' ? 'Your message' : 'Assistant message'}>
            {#if m.reasoning}
              <details class="thinking">
                <summary><Brain size={13} aria-hidden="true" /> Thinking</summary>
                <p class="muted">{m.reasoning}</p>
              </details>
            {/if}
            {m.content}
          </article>
        {/if}
      {/each}
    {/if}
      {#if sending && sessions.activeId === sendSessionId && isResearch && timeline.events.length > 0}
        <!-- The LIVE work stream: Timeline events as the research run
             works — analysis, steps, tool calls, delegations. -->
        <section class="work-stream" aria-label="Research run in progress" aria-live="polite">
          {#each timeline.events as ev (ev.id)}
            {@const row = workRow(ev)}
            {#if row}
              <div class="work-row {row.tone}">
                <span class="work-icon" aria-hidden="true">{row.icon}</span>
                <span class="work-kind">{row.kind}</span>
                <span class="work-text">{row.text}</span>
                {#if row.more}
                  <details class="work-more">
                    <summary>more</summary>
                    <pre>{row.more}</pre>
                  </details>
                {/if}
              </div>
            {/if}
          {/each}
          <div class="work-row live" aria-hidden="true"><span class="dots"><i></i><i></i><i></i></span> working…</div>
        </section>
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
            <div class="stream-text">{content}</div>
          {:else}
            <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
          {/if}
        </article>
      {:else if sending && sessions.activeId === sendSessionId}
        <article class="bubble assistant pending" aria-label="Assistant is working" aria-busy="true">
          {#if isResearch}
            <div class="research-progress">
              <Microscope size={16} aria-hidden="true" />
              <span>Running the full pipeline…</span>
              <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
            </div>
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
  .bubble { max-width: min(48rem, 90%); padding: .7rem 1rem; border-radius: 14px; line-height: 1.55; white-space: pre-wrap; overflow-wrap: break-word; }
  .bubble.user { justify-self: end; background: var(--ds-color-primary, #3b82f6); color: var(--ds-color-on-primary, #fff); border-bottom-right-radius: 4px; }
  .bubble.assistant { justify-self: start; background: var(--ds-color-surface, #1e1e1e); border: 1px solid var(--ds-color-border, #333); border-bottom-left-radius: 4px; }
  .bubble.pending, .bubble.streaming { min-height: 2.5rem; display: grid; align-content: center; }
  .thinking { margin-bottom: .5rem; font-size: .8rem; color: var(--ds-color-muted, #888); }
  .thinking summary { cursor: pointer; display: inline-flex; gap: .35rem; align-items: center; list-style: none; }
  .thinking p { margin: .3rem 0 0; padding: .4rem .6rem; border-left: 2px solid var(--ds-color-border, #333); font-size: .78rem; }
  .stream-text { white-space: pre-wrap; }
  .dots { display: inline-flex; gap: 4px; }
  .dots i { width: 6px; height: 6px; border-radius: 50%; background: var(--ds-color-muted, #888); animation: pulse 1.2s ease-in-out infinite; }
  .dots i:nth-child(2) { animation-delay: .15s; }
  .dots i:nth-child(3) { animation-delay: .3s; }
  @keyframes pulse { 0%, 80%, 100% { opacity: .3; } 40% { opacity: 1; } }
  .tool-row { display: flex; align-items: flex-start; gap: .45rem; padding: .3rem .6rem; font-size: .78rem; color: var(--ds-color-muted, #888); border-radius: 6px; }
  .tool-row:hover { background: var(--ds-color-hover, #2a2a2a); }
  .tool-name { color: var(--ds-color-text, #eee); font-weight: 500; white-space: nowrap; }
  .tool-detail summary { cursor: pointer; display: inline-flex; color: var(--ds-color-muted, #888); }
  .tool-output { display: block; max-width: 40rem; max-height: 10rem; overflow: auto; padding: .3rem .5rem; margin-top: .2rem; border-radius: 4px; background: var(--ds-color-field, #1a1a1a); font-size: .72rem; white-space: pre-wrap; }
  /* The Task Analysis card (research exchange header) */
  .analysis-card { border: 1px solid var(--accent, var(--ds-color-primary, #6366f1)); border-radius: 12px; padding: .7rem 1rem; display: grid; gap: .4rem; background: var(--ds-color-surface, #1e1e1e); max-width: min(48rem, 90%); }
  .analysis-head { display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; }
  .analysis-head strong { font-size: .9rem; }
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
  .skill-chip.dropped { color: var(--ds-color-muted, #666); text-decoration: line-through; }
  /* Delegation tree */
  .del-tree { border: 1px solid var(--ds-color-border, #333); border-radius: 12px; padding: .5rem .8rem; background: var(--ds-color-surface, #1e1e1e); max-width: min(48rem, 90%); font-size: .78rem; }
  .del-title { display: flex; align-items: center; gap: .35rem; margin: 0 0 .3rem; font-size: .66rem; text-transform: uppercase; letter-spacing: .06em; color: var(--ds-color-muted, #888); }
  .del-node { display: flex; align-items: center; gap: .45rem; margin: 0; padding: .15rem 0; }
  .del-node .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--ds-color-success, #22c55e); flex-shrink: 0; }
  .del-node strong { font-weight: 500; }
  .research-progress { display: flex; align-items: center; gap: .5rem; color: var(--ds-color-muted, #888); font-size: .85rem; }
  /* The live work stream */
  .work-stream { display: grid; gap: .25rem; max-width: min(48rem, 90%); padding: .6rem .8rem; border: 1px solid var(--ds-color-border, #333); border-radius: 12px; background: var(--ds-color-surface, #1e1e1e); font-size: .78rem; overflow: hidden; }
  .work-row { display: flex; align-items: baseline; gap: .5rem; margin: 0; min-width: 0; color: var(--ds-color-text, #eee); }
  .work-row .work-icon { flex-shrink: 0; }
  .work-row .work-kind { flex-shrink: 0; font-size: .62rem; text-transform: uppercase; letter-spacing: .05em; color: var(--ds-color-muted, #888); min-width: 4.2rem; }
  .work-row .work-text { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .work-row.tool .work-text, .work-row.step .work-text { color: var(--ds-color-muted, #aaa); }
  .work-row.analysis { color: var(--accent, #6366f1); font-weight: 500; }
  .work-row.delegation .work-text { color: var(--ds-color-success, #22c55e); }
  .work-row.system .work-text { color: var(--ds-color-muted, #888); font-style: italic; }
  .work-row.live { color: var(--ds-color-muted, #888); }
  .work-more summary { cursor: pointer; color: var(--ds-color-muted, #888); font-size: .68rem; }
  .work-more pre { max-width: 36rem; max-height: 8rem; overflow: auto; margin: .2rem 0 0; padding: .3rem .5rem; border-radius: 4px; background: var(--ds-color-field, #1a1a1a); font-size: .68rem; white-space: pre-wrap; }
  .composer-region { padding: .75rem clamp(.75rem, 3vw, 2rem) .5rem; display: grid; gap: .4rem; border-top: 1px solid var(--ds-color-border, #333); background: var(--ds-color-sidebar, #161616); min-width: 0; max-width: 100%; overflow: hidden; }
  .ward-bar { display: flex; align-items: center; gap: .45rem; padding: .35rem .7rem; border-radius: 8px; border: 1px solid color-mix(in oklch, var(--ds-color-success, #22c55e) 40%, transparent); background: color-mix(in oklch, var(--ds-color-success, #22c55e) 10%, transparent); font-size: .8rem; color: var(--ds-color-success, #22c55e); }
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
