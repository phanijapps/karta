<script lang="ts">
  import { tick } from 'svelte';
  import { ArrowUp, Brain, Plus, Sparkles, Wrench, Microscope, MessageCircle, FolderOpen, ChevronDown, RefreshCw } from '@lucide/svelte';
  import ModelPicker from '../../shared/ui/ModelPicker.svelte';
  import { sendChatMessage, streamChatMessage, continueSession } from '../../shared/api/client';
  import type { Sessions } from '../../entities/session/session.svelte';

  let { sessions }: { sessions: Sessions } = $props();

  let draft = $state('');
  let sending = $state(false);
  let transcriptEl: HTMLElement;

  // Chat mode state (persisted per session; default = chat).
  let mode = $state<'chat' | 'research'>('chat');
  let wardContext = $state<{ name: string; archetype: string } | null>(null);

  // Live stream state (volatile view; the transcript stays the truth).
  let reasoning = $state('');
  let content = $state('');
  let streaming = $state(false);
  let thinkingOpen = $state(true);
  let announced = $state('');

  // Research exchange state (the Task Analysis card data).
  let analysis = $state<{
    primary_intent: string;
    complexity: string;
    step_budget: number;
    approach: string;
    ward_name: string;
    ward_action: string;
    dropped: string[];
  } | null>(null);
  let showWork = $state(false);
  let budgetRemaining = $state<number | null>(null);

  // One-shot load (cursor changes inside load() must not retrigger).
  let loaded = false;
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

  const canSend = $derived(draft.trim() !== '' && !sending);

  async function send() {
    const text = draft.trim();
    if (!text || sending) return;
    sending = true;
    streaming = true;
    reasoning = '';
    content = '';
    thinkingOpen = true;
    announced = '';
    analysis = null;
    showWork = false;
    budgetRemaining = null;
    sessions.sendError = '';
    try {
      let result: Awaited<ReturnType<typeof sendChatMessage>>;
      if (mode === 'chat') {
        try {
          result = await streamChatMessage(text, sessions.model, (d) => {
            if (d.kind === 'reasoning') {
              reasoning += d.text;
            } else {
              if (content === '' && reasoning !== '') thinkingOpen = false;
              content += d.text;
            }
          });
        } catch {
          result = await sendChatMessage(text, sessions.model);
        }
      } else {
        // Research mode: the exchange completes as a unit.
        result = await sendChatMessage(text, sessions.model, 'research');
        // Extract the Task Analysis from the exchange's tool rows.
        const analysisRow = result.messages?.find((r: any) => r.role === 'tool' && r.tool_name === 'intent_analysis');
        if (analysisRow) {
          try {
            const payload = JSON.parse(analysisRow.content);
            analysis = payload.intent_analysis ?? payload;
          } catch { /* not a JSON analysis */ }
        }
      }
      await sessions.reconcile();
      if (!result.ok) {
        sessions.sendError = result.errorMessage ?? 'send failed';
        announced = 'Reply failed.';
      } else {
        announced = 'Reply complete.';
      }
      draft = '';
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
    const res = await continueSession('default', 25);
    if (res.ok) {
      budgetRemaining = (budgetRemaining ?? 0) + res.granted;
      announced = `Extended by ${res.granted} iterations.`;
    }
  }

  const modeLabel = $derived(mode === 'chat' ? 'Chat' : 'Research');
</script>

<div class="chat-region" class:research={mode === 'research'}>
  {#if sessions.sendError === 'daemon'}
    <div class="chat-banner" role="alert">
      <strong>Daemon unreachable.</strong> Start it with <code>go run ./cmd/zbotd</code> from <code>backend/</code>; your transcript is safe locally.
    </div>
  {:else if sessions.sendError}
    <div class="chat-banner" role="alert">{sessions.sendError}</div>
  {/if}

  <span class="sr-only" aria-live="polite">{announced}</span>

  <div class="transcript" bind:this={transcriptEl} aria-label="Conversation transcript">
    {#if sessions.loading}
      <div class="chat-empty"><Sparkles size={28} strokeWidth={1.2} /><p class="muted">Loading the conversation…</p></div>
    {:else if sessions.messages.length === 0}
      <div class="chat-empty">
        <Sparkles size={28} strokeWidth={1.2} />
        <h3>A conversation that stays yours</h3>
        <p class="muted">One persistent conversation — every message is kept locally, forever. Pick a mode and a model above.</p>
      </div>
    {:else}
      {#each sessions.messages as m (m.id)}
        {#if m.role === 'tool' && m.tool_name === 'intent_analysis'}
          <!-- Task Analysis card: the research exchange header -->
          {@const parsed = (() => { try { const c = JSON.parse(m.content); return c.intent_analysis ?? c; } catch { return null; } })()}
          {#if parsed}
          <article class="analysis-card" aria-label="Task Analysis">
              <header class="analysis-head">
                <Microscope size={16} aria-hidden="true" />
                <strong>Research</strong>
                <span class="badge approach">{parsed.approach}</span>
                <span class="badge complexity" data-complexity={parsed.complexity}>{parsed.complexity}</span>
                {#if parsed.ward_name}
                  <span class="badge ward"><FolderOpen size={11} aria-hidden="true" /> {parsed.ward_name}</span>
                {/if}
              </header>
              <dl class="analysis-meta">
                <div><dt>Intent</dt><dd>{parsed.primary_intent}</dd></div>
                {#if parsed.dropped?.length}
                  <div><dt>Dropped</dt><dd>{parsed.dropped.join(', ')}</dd></div>
                {/if}
              </dl>
          </article>
          {/if}
        {:else if m.role === 'tool'}
          <article class="tool-row" aria-label={`Tool ${m.tool_name ?? 'call'}`}>
            <Wrench size={14} />
            <span class="tool-name">{m.tool_name ?? 'tool'}</span>
            <details class="tool-detail">
              <summary><ChevronDown size={12} aria-hidden="true" /></summary>
              <code class="tool-output">{m.content}</code>
            </details>
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
      {#if streaming && (reasoning !== '' || content !== '')}
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
      {:else if sending}
        <article class="bubble assistant pending" aria-label="Assistant is working" aria-busy="true">
          {#if mode === 'research'}
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
    <!-- Ward context bar (research mode when a ward is active) -->
    {#if mode === 'research' && wardContext}
      <div class="ward-bar" aria-label="Active ward">
        <FolderOpen size={14} aria-hidden="true" />
        <span>{wardContext.name}</span>
        <span class="muted">· {wardContext.archetype}</span>
        <button type="button" class="leave-ward" onclick={() => (wardContext = null)}>Leave</button>
      </div>
    {/if}

    <!-- Continue bar (research mode near budget) -->
    {#if mode === 'research' && budgetRemaining !== null && budgetRemaining <= 2}
      <div class="continue-bar" role="alert">
        <span>Budget: {budgetRemaining} steps remaining</span>
        <button type="button" class="continue-btn" onclick={() => void extend()}>
          <RefreshCw size={13} aria-hidden="true" /> Continue +25
        </button>
      </div>
    {/if}

    <form class="composer" onsubmit={(e) => { e.preventDefault(); void send(); }}>
      <!-- Mode toggle -->
      <div class="mode-toggle" role="radiogroup" aria-label="Execution mode">
        <button type="button" class:active={mode === 'chat'} aria-pressed={mode === 'chat'} onclick={() => (mode = 'chat')}>
          <MessageCircle size={14} aria-hidden="true" /><span>Chat</span>
        </button>
        <button type="button" class:active={mode === 'research'} aria-pressed={mode === 'research'} onclick={() => (mode = 'research')}>
          <Microscope size={14} aria-hidden="true" /><span>Research</span>
        </button>
      </div>

      <button type="button" class="icon-button" aria-label="Attach (not implemented)" disabled title="Not implemented"><Plus size={18} /></button>
      <label class="sr-only" for="composer-input">Message Karta</label>
      <textarea
        id="composer-input"
        bind:value={draft}
        onkeydown={onkeydown}
        placeholder={mode === 'research' ? 'Research question…' : sessions.model === '' ? 'Pick a model first…' : 'Message Karta'}
        aria-label={mode === 'research' ? 'Research question' : 'Message Karta'}
        autocomplete="off"
        rows="1"></textarea>
      <button type="submit" class="send" disabled={!canSend} aria-label="Send message" title="Send"><ArrowUp size={18} /></button>
    </form>
    <p class="composer-note">
      {#if mode === 'research'}
        Research mode: full pipeline — intent analysis, ward tools, delegation.
      {:else}
        Replies come from your selected model via your local daemon. Replies are saved locally, forever.
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

  .research-progress { display: flex; align-items: center; gap: .5rem; color: var(--ds-color-muted, #888); font-size: .85rem; }

  .composer-region { padding: .75rem clamp(.75rem, 3vw, 2rem) .5rem; display: grid; gap: .4rem; border-top: 1px solid var(--ds-color-border, #333); background: var(--ds-color-sidebar, #161616); min-width: 0; max-width: 100%; overflow: hidden; }

  .mode-toggle { display: inline-flex; border-radius: 10px; border: 1px solid var(--ds-color-border, #333); overflow: hidden; margin-bottom: .4rem; }
  .mode-toggle button { display: inline-flex; align-items: center; gap: .35rem; padding: .35rem .9rem; border: none; background: transparent; color: var(--ds-color-muted, #888); font-size: .8rem; cursor: pointer; }
  @media (max-width: 480px) { .mode-toggle button span { display: none; } .mode-toggle button { padding: .35rem .6rem; } }
  .mode-toggle button.active { background: var(--accent, var(--ds-color-primary, #3b82f6)); color: var(--ds-color-on-primary, #fff); }
  .mode-toggle button:not(.active):hover { background: var(--ds-color-hover, #2a2a2a); }

  .ward-bar { display: flex; align-items: center; gap: .45rem; padding: .35rem .7rem; border-radius: 8px; border: 1px solid var(--ds-color-success, #22c55e); font-size: .8rem; color: var(--ds-color-success, #22c55e); }
  .leave-ward { margin-left: auto; background: none; border: none; color: var(--ds-color-muted, #888); font-size: .72rem; cursor: pointer; text-decoration: underline; }

  .continue-bar { display: flex; align-items: center; justify-content: space-between; padding: .4rem .7rem; border-radius: 8px; border: 1px solid var(--ds-color-danger, #e5484d); font-size: .8rem; }
  .continue-btn { display: inline-flex; align-items: center; gap: .3rem; padding: .25rem .7rem; border-radius: 6px; border: 1px solid var(--ds-color-border, #333); background: var(--ds-color-surface, #1e1e1e); color: inherit; font-size: .78rem; cursor: pointer; }
  .continue-btn:hover { background: var(--ds-color-hover, #2a2a2a); }

  .composer { display: flex; align-items: flex-end; gap: .5rem; padding: .5rem; border-radius: 14px; border: 1px solid var(--ds-color-border, #333); background: var(--ds-color-field, #1a1a1a); min-width: 0; max-width: 100%; box-sizing: border-box; }
  .composer:focus-within { border-color: var(--accent, var(--ds-color-focus, #2563eb)); }
  .composer textarea { flex: 1; min-width: 0; resize: none; border: none; background: transparent; color: inherit; font: inherit; padding: .3rem 0; outline: none; max-height: 10rem; }
  .composer textarea::placeholder { color: var(--ds-color-muted, #666); }

  .send { display: grid; place-items: center; width: 2.2rem; height: 2.2rem; border-radius: 10px; border: none; background: var(--accent, var(--ds-color-primary, #3b82f6)); color: var(--ds-color-on-primary, #fff); cursor: pointer; }
  .send:disabled { opacity: .4; cursor: not-allowed; }

  .icon-button { display: grid; place-items: center; width: 2.2rem; height: 2.2rem; border-radius: 10px; border: none; background: transparent; color: var(--ds-color-muted, #888); cursor: pointer; }
  .icon-button:disabled { opacity: .3; }

  .composer-note { margin: 0; font-size: .72rem; color: var(--ds-color-muted, #666); text-align: center; }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
</style>
