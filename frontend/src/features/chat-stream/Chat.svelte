<script lang="ts">
  import { tick } from 'svelte';
  import { ArrowUp, Plus, Sparkles } from '@lucide/svelte';
  import { type Sessions } from '../../entities/session/session.svelte';
  import ModelPicker from '../../shared/ui/ModelPicker.svelte';
  let { sessions }: { sessions: Sessions } = $props();
  let draft = $state('');
  let announcement = $state('');
  let textarea: HTMLTextAreaElement;
  let bottom: HTMLDivElement;
  let previousId: number | undefined;
  $effect(() => { if (sessions.selected !== previousId) { draft = ''; previousId = sessions.selected; } });
  async function send() {
    if (!draft.trim()) return;
    sessions.send(draft);
    draft = '';
    announcement = `Simulated assistant reply added. ${sessions.active.messages.length} messages in this conversation.`;
    await tick();
    bottom.scrollIntoView({ block: 'nearest' });
    textarea.focus();
  }
</script>

<div class="chat">
  <section class="transcript" aria-label="Conversation messages">
    <div class="message-column">
      {#if !sessions.active.messages.length}
        <div class="greeting">
          <Sparkles size={30} strokeWidth={1.4} />
          <h1>What’s on your mind?</h1>
          <p class="muted">A little space for your next big idea.</p>
          <button class="starter" onclick={() => { draft = 'Help me plan my next project.'; textarea.focus(); }}>Help me plan my next project <span aria-hidden="true">↗</span></button>
        </div>
      {:else}
        <h1 class="sr-only">{sessions.active.title}</h1>
        {#each sessions.active.messages as message (message.id)}
          <article class:user={message.role === 'user'} aria-label={message.role === 'user' ? 'Your message' : 'Mock assistant message'}>
            {#if message.role === 'assistant'}<div class="assistant-label"><span class="avatar" aria-hidden="true">K</span><strong>Karta</strong><span class="badge">Mock</span></div>{/if}
            <p>{message.text}</p>
          </article>
        {/each}
      {/if}
      <div bind:this={bottom!}></div>
    </div>
  </section>
  <div class="composer-region">
    <form class="composer" onsubmit={(event) => { event.preventDefault(); void send(); }}>
      <label class="sr-only" for="message">Message Karta</label>
      <textarea id="message" bind:this={textarea!} bind:value={draft} rows="2" placeholder="Send a message" onkeydown={(event) => { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); void send(); } }}></textarea>
      <div class="composer-tools">
        <button class="icon-button" type="button" disabled aria-label="Attachments unavailable in shell preview" title="Attachments are not implemented"><Plus size={20} /></button>
        <div class="model-label"><ModelPicker id="composer-model" label="Model" bind:value={sessions.model} /></div>
        <button class="send icon-button" type="submit" aria-label="Send message" disabled={!draft.trim()}><ArrowUp size={20} /></button>
      </div>
    </form>
    <p class="disclaimer">Shell preview. Replies are simulated. Nothing is saved.</p>
  </div>
  <div class="sr-only" aria-live="polite">{announcement}</div>
</div>

<style>
  .chat { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .transcript { overflow-y: auto; flex: 1; min-height: 0; padding: var(--ds-space-8) var(--ds-space-6); }
  .message-column { max-width: var(--ds-chat-width); margin: auto; }
  article { margin: 0 0 2.5rem; font-size: var(--ds-font-chat); line-height: 1.8; }
  article p { white-space: pre-wrap; overflow-wrap: anywhere; }
  article.user { background: var(--ds-color-bubble); padding: var(--ds-space-3) var(--ds-space-6); border-radius: var(--ds-radius-composer); width: fit-content; max-width: 85%; margin-left: auto; }
  .assistant-label { display: flex; align-items: center; gap: var(--ds-space-2); margin-bottom: var(--ds-space-4); font-size: var(--ds-font-control); }
  .avatar { display: grid; place-items: center; width: 26px; height: 26px; border: 1px solid var(--ds-color-border); border-radius: 50%; font-weight: 700; }
  .composer-region { padding: var(--ds-space-3) var(--ds-space-6) var(--ds-space-4); }
  .composer { max-width: var(--ds-chat-width); margin: auto; padding: var(--ds-space-4) var(--ds-space-3) var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-composer); background: var(--ds-color-surface); box-shadow: 0 6px 24px -4px #0001; }
  textarea { width: 100%; min-height: 56px; max-height: 160px; resize: vertical; border: 0; padding: 0 var(--ds-space-2); font-size: var(--ds-font-chat); }
  .composer-tools { display: flex; align-items: center; gap: var(--ds-space-2); }
  .model-label { display: flex; align-items: center; gap: var(--ds-space-2); min-width: 0; }
  .send { margin-left: auto; background: var(--ds-color-text); color: var(--ds-color-background); border-radius: 50%; }
  .send:hover:not(:disabled) { background: var(--ds-color-muted); }
  .disclaimer { text-align: center; color: var(--ds-color-muted); font-size: 0.6875rem; margin-top: var(--ds-space-3); }
  .greeting { min-height: 40dvh; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: var(--ds-space-4); text-align: center; }
  .greeting h1 { font-size: 1.875rem; font-weight: 600; letter-spacing: -0.04em; }
  .starter { border: 1px solid var(--ds-color-border); padding: var(--ds-space-3) var(--ds-space-4); margin-top: var(--ds-space-6); border-radius: var(--ds-radius-composer); }
  @media (max-width: 767px) { .transcript { padding: var(--ds-space-4); } .composer-region { padding: var(--ds-space-2); } }
</style>
