<script lang="ts">
  import { tick } from 'svelte';
  import { ArrowUp, Brain, Plus, Sparkles, Wrench } from '@lucide/svelte';
  import ModelPicker from '../../shared/ui/ModelPicker.svelte';
  import { sendChatMessage, streamChatMessage } from '../../shared/api/client';
  import type { Sessions } from '../../entities/session/session.svelte';

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
    sessions.sendError = '';
    try {
      let result: Awaited<ReturnType<typeof sendChatMessage>>;
      try {
        // Streamed send: deltas render live; the terminal exchange (the
        // persisted rows) reconciles the transcript afterward.
        result = await streamChatMessage(text, sessions.model, (d) => {
          if (d.kind === 'reasoning') {
            reasoning += d.text;
          } else {
            // First content collapses the Thinking block; the user can
            // reopen it afterward.
            if (content === '' && reasoning !== '') thinkingOpen = false;
            content += d.text;
          }
        });
      } catch {
        // Stream transport failed before any typed answer: fall back to
        // the synchronous endpoint (same persisted truth).
        result = await sendChatMessage(text, sessions.model);
      }
      // Reconcile from the last contiguously delivered cursor on EVERY
      // outcome — the daemon returns the persisted rows either way.
      await sessions.reconcile();
      if (!result.ok) {
        sessions.sendError = result.errorMessage ?? 'send failed';
        announced = 'Reply failed.';
      } else {
        announced = 'Reply complete.';
      }
      draft = '';
    } catch {
      sessions.sendError = 'daemon';
      announced = 'Reply failed.';
    } finally {
      streaming = false;
      sending = false;
      await scrollToLatest();
      composerFocus();
    }
  }

  function composerFocus() {
    document.getElementById('composer-input')?.focus();
  }

  function onkeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      void send();
    }
  }
</script>

<div class="chat-region">
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
        <p class="muted">One persistent conversation — every message is kept locally, forever. Pick a model above and say hello.</p>
      </div>
    {:else}
      {#each sessions.messages as m (m.id)}
        {#if m.role === 'tool'}
          <article class="tool-row" aria-label={`Tool ${m.tool_name ?? 'call'}`}>
            <Wrench size={14} />
            <span class="tool-name">{m.tool_name ?? 'tool'}</span>
            <code class="tool-output">{m.content}</code>
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
          <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
          <span class="sr-only">Working on your message…</span>
        </article>
      {/if}
  </div>

  <div class="composer-region">
    <form class="composer" onsubmit={(e) => { e.preventDefault(); void send(); }}>
      <button type="button" class="icon-button" aria-label="Attach (not implemented)" disabled title="Not implemented"><Plus size={18} /></button>
      <label class="sr-only" for="composer-input">Message Karta</label>
      <textarea
        id="composer-input"
        bind:value={draft}
        onkeydown={onkeydown}
        placeholder={sessions.model === '' ? 'Pick a model first…' : 'Message Karta'}
        aria-label="Message Karta"
        autocomplete="off"
        rows="1"></textarea>
      <button type="submit" class="send" disabled={!canSend} aria-label="Send message" title="Send"><ArrowUp size={18} /></button>
    </form>
    <p class="composer-note">Replies come from your selected model via your local daemon. Replies are saved locally, forever.</p>
  </div>
</div>

<style>
  .chat-region { height: 100%; display: flex; flex-direction: column; min-height: 0; }
  .chat-banner { border: 1px solid var(--ds-color-border); background: var(--ds-color-surface); padding: var(--ds-space-2) var(--ds-space-3); border-radius: var(--ds-radius-control); font-size: var(--ds-font-nav); color: var(--ds-color-muted); margin: var(--ds-space-2) var(--ds-space-4) 0; }
  .transcript { flex: 1; overflow-y: auto; padding: var(--ds-space-6) max(calc(50% - 29rem), var(--ds-space-4)); scroll-behavior: smooth; }
  .chat-empty { display: grid; justify-items: center; gap: var(--ds-space-2); padding-top: 15vh; color: var(--ds-color-muted); text-align: center; }
  .chat-empty h3 { font-size: 1.05rem; font-weight: 600; color: var(--ds-color-text); }
  .bubble { max-width: 48rem; padding: var(--ds-space-3) var(--ds-space-4); border-radius: var(--ds-radius-composer); margin-bottom: var(--ds-space-3); line-height: 1.6; overflow-wrap: anywhere; white-space: pre-wrap; }
  .bubble.user { background: var(--ds-color-surface-raised, var(--ds-color-hover)); margin-left: auto; border: 1px solid var(--ds-color-border); }
  .bubble.assistant { background: transparent; color: var(--ds-color-text); }
  .bubble.pending { color: var(--ds-color-muted); display: flex; align-items: center; gap: var(--ds-space-2); }
  .dots { display: inline-flex; gap: 4px; }
  .dots i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; opacity: 0.4; animation: dot-pulse 1.2s infinite; }
  .dots i:nth-child(2) { animation-delay: 0.2s; }

  .thinking {
    margin-bottom: 0.5rem;
    font-size: 0.85rem;
  }
  .thinking summary {
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    color: var(--tx-muted, #888);
    user-select: none;
  }
  .thinking p {
    margin: 0.4rem 0 0;
    white-space: pre-wrap;
    border-left: 2px solid var(--border, #333);
    padding-left: 0.6rem;
  }
  .stream-text {
    white-space: pre-wrap;
  }
  .dots i:nth-child(3) { animation-delay: 0.4s; }
  @keyframes dot-pulse { 0%, 80%, 100% { opacity: 0.25; } 40% { opacity: 1; } }
  .tool-row { display: flex; align-items: baseline; gap: var(--ds-space-2); max-width: 48rem; margin: 0 auto var(--ds-space-3) 0; font-size: 0.8125rem; color: var(--ds-color-muted); border: 1px dashed var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-1) var(--ds-space-2); }
  .tool-name { font-weight: 600; }
  .tool-output { font-size: 0.75rem; overflow-wrap: anywhere; max-width: 100%; }
  .composer-region { padding: var(--ds-space-2) max(calc(50% - 29rem), var(--ds-space-4)) var(--ds-space-4); }
  .composer { display: flex; align-items: center; gap: var(--ds-space-2); border: 1px solid var(--ds-color-border); background: var(--ds-color-surface); border-radius: var(--ds-radius-composer); padding: var(--ds-space-2); box-shadow: 0 1px 3px rgb(0 0 0 / 0.06); }
  .composer textarea { flex: 1; background: transparent; border: 0; outline: none; font-size: 1rem; color: var(--ds-color-text); min-height: 28px; resize: none; font-family: inherit; }
  .composer textarea::placeholder { color: var(--ds-color-muted); }
  .icon-button { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; border-radius: 999px; color: var(--ds-color-muted); }
  .icon-button:disabled { opacity: 0.4; }
  .send { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; border-radius: 999px; background: var(--ds-color-text); color: var(--ds-color-background); }
  .send:disabled { opacity: 0.3; }
  .composer-note { text-align: center; font-size: 0.6875rem; color: var(--ds-color-muted); margin-top: var(--ds-space-2); }
  @media (max-width: 767px) { .transcript { padding: var(--ds-space-4); } .composer-region { padding: var(--ds-space-2) var(--ds-space-2) var(--ds-space-3); } }
</style>
