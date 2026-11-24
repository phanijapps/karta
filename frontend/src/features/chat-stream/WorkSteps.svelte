<script lang="ts">
  // The step tree view: friendly verb + object + outcome per step,
  // collapsed by default; a LIVE step opens while its turn runs and
  // tidies itself when the next step begins. Expansion shows the
  // individual calls (args), result excerpts, the plan narration and
  // the thinking (the brain) — never at the top level.
  import { Brain, ChevronDown, Wrench, Users } from '@lucide/svelte';
  import { buildSteps, describeCall, outcomeHint, type Step } from './steps';
  import type { ChatMessage } from '../../shared/api/client';

  let { messages, liveTurnId = undefined }: { messages: ChatMessage[]; liveTurnId?: string } = $props();

  let open = $state<Set<string>>(new Set());
  let expandedOnce = new Set<string>(); // live steps auto-open once, then user-driven

  const steps = $derived.by<Step[]>(() => buildSteps(messages, liveTurnId));

  // Auto-open the live step (first time it appears); never auto-close a
  // step the user opened themselves.
  $effect(() => {
    for (const s of steps) {
      if (s.live && !expandedOnce.has(s.key)) {
        expandedOnce.add(s.key);
        open = new Set([...open, s.key]);
      }
      // A live step that ENDED and was auto-opened: tidy it.
      if (!s.live && expandedOnce.has(s.key) && open.has(s.key) && s.key === lastLiveKey) {
        const next = new Set(open);
        next.delete(s.key);
        open = next;
      }
    }
  });
  let lastLiveKey = '';

  function toggle(key: string) {
    const next = new Set(open);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    open = next;
  }

  /** Top-level label: verb + object, deduped across merged calls. */
  function label(s: Step): { verb: string; object: string } {
    const described = s.calls.map((c) => describeCall(c));
    const objects = [...new Set(described.map((d) => d.object).filter(Boolean))];
    // The verb comes from the STEP's own tool (the first call's) — a
    // merged group is one kind of action even if a stray call differs.
    const verb = describeCall({ id: '', name: s.tool, args: {} }).verb;
    if (s.tool === 'delegate_agent') {
      return { verb, object: described.map((d) => d.object).filter(Boolean).join('; ') };
    }
    if (objects.length === 0) return { verb, object: s.tool };
    if (objects.length === 1) return { verb, object: objects[0] };
    // Several distinct objects: list up to 3, then +N.
    const shown = objects.slice(0, 3).join(', ');
    return { verb, object: `${shown}${objects.length > 3 ? ` +${objects.length - 3}` : ''}` };
  }

  /** Calls per step for the count suffix ("2 calls"). */
  function count(s: Step): number {
    return s.calls.length;
  }
</script>

<section class="steps" aria-label="Work steps">
  {#each steps as s (s.key)}
    {@const l = label(s)}
    {@const hint = outcomeHint(s.tool, s.outcomes)}
    {@const isMerge = count(s) > 1}
    {@const hasDetail = s.narration || s.reasoning || s.outcomes.length || s.calls.length > 1}
    <div class="step" class:live={s.live} class:delegation={s.tool === 'delegate_agent'} class:nested={false}>
      <button
        type="button"
        class="step-row"
        class:expandable={hasDetail || s.children.length > 0}
        aria-expanded={open.has(s.key)}
        onclick={() => hasDetail || s.children.length > 0 ? toggle(s.key) : undefined}
        disabled={!hasDetail && s.children.length === 0}
      >
        <span class="step-icon" aria-hidden="true">
          {#if s.tool === 'delegate_agent'}<Users size={13} />{:else}<Wrench size={13} />{/if}
        </span>
        <span class="step-text">
          <span class="verb">{l.verb}</span>
          {#if l.object}<span class="object">{l.object}</span>{/if}
        </span>
        <span class="step-meta">
          {#if s.live}<span class="pulse" aria-label="working"></span>{:else if isMerge}<span class="count">{count(s)} calls</span>{/if}
          {#if hint && !s.live}<span class="hint" class:err={hint.includes('error')}>{hint}</span>{/if}
          {#if hasDetail || s.children.length > 0}<ChevronDown size={12} class={open.has(s.key) ? 'chev-open' : 'chev'} aria-hidden="true" />{/if}
        </span>
      </button>
      {#if open.has(s.key)}
        <div class="step-detail">
          {#if s.narration}
            <p class="narration"><span class="narration-label">Plan</span> {s.narration}</p>
          {/if}
          {#if s.reasoning}
            <details class="thinking">
              <summary><Brain size={12} aria-hidden="true" /> Thinking</summary>
              <p class="muted">{s.reasoning}</p>
            </details>
          {/if}
          {#each s.calls as c, i (c.id)}
            {@const d = describeCall(c)}
            {@const outs = s.outcomes.filter((o) => o.callId === c.id)}
            <div class="call">
              <div class="call-head">
                <code class="raw">{c.name}</code>
                <span class="call-args">{JSON.stringify(c.args).slice(0, 160)}{JSON.stringify(c.args).length > 160 ? '…' : ''}</span>
              </div>
              {#each outs as o (o.callId + i)}
                <pre class="result" class:err={o.isError}>{o.output.slice(0, 600)}{o.output.length > 600 ? '\n…' : ''}</pre>
              {/each}
            </div>
          {/each}
          {#if s.children.length}
            <div class="children">
              {#each s.children as child (child.key)}
                {@const cl = label(child)}
                {@const ch = outcomeHint(child.tool, child.outcomes)}
                <div class="step child">
                  <span class="step-icon" aria-hidden="true"><Wrench size={12} /></span>
                  <span class="step-text"><span class="verb">{cl.verb}</span> {#if cl.object}<span class="object">{cl.object}</span>{/if}</span>
                  {#if count(child) > 1}<span class="count">{count(child)} calls</span>{/if}
                  {#if ch}<span class="hint" class:err={ch.includes('error')}>{ch}</span>{/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/each}
</section>

<style>
  .steps { display: grid; gap: 2px; max-width: min(48rem, 90%); }
  .step { border-radius: 8px; }
  .step.live .step-row { border-color: color-mix(in oklch, var(--accent, var(--ds-color-primary, #6366f1)) 45%, transparent); }
  .step-row { display: flex; align-items: center; gap: .5rem; width: 100%; text-align: left; padding: .3rem .5rem; border: 1px solid transparent; border-radius: 8px; background: transparent; color: var(--ds-color-text, #eee); font-size: .78rem; cursor: default; }
  .step-row.expandable { cursor: pointer; }
  .step-row.expandable:hover { background: var(--ds-color-hover, #2a2a2a); }
  .step-row:disabled { cursor: default; }
  .step-icon { display: grid; place-items: center; width: 1.3rem; color: var(--ds-color-muted, #888); flex-shrink: 0; }
  .step.delegation .step-icon { color: var(--ds-color-success, #22c55e); }
  .step-text { flex: 1; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .verb { font-weight: 500; }
  .object { color: var(--ds-color-muted, #aaa); }
  .object::before { content: ' '; }
  :global(.chev) { transition: transform .15s; }
  :global(.chev-open) { transform: rotate(180deg); }
  .step-meta { display: inline-flex; align-items: center; gap: .45rem; flex-shrink: 0; color: var(--ds-color-muted, #888); font-size: .68rem; }
  .hint { padding: .05rem .4rem; border-radius: 999px; background: var(--ds-color-hover, #2a2a2a); }
  /* Error hint: the danger color as a BORDER on a neutral pill — red text
     on a red tint failed AA in the light theme. */
  .hint.err { color: var(--ds-color-text, #eee); border: 1px solid var(--ds-color-danger, #e5484d); background: var(--ds-color-hover, #2a2a2a); }
  .pulse { width: 7px; height: 7px; border-radius: 50%; background: var(--accent, var(--ds-color-primary, #6366f1)); animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: .35; } 50% { opacity: 1; } }
  .step-detail { margin: .1rem 0 .35rem 1.4rem; padding: .35rem .6rem; border-left: 1px solid var(--ds-color-border, #333); display: grid; gap: .35rem; }
  .narration { margin: 0; font-size: .74rem; color: var(--ds-color-muted, #888); }
  .narration-label { font-size: .62rem; text-transform: uppercase; letter-spacing: .05em; color: var(--ds-color-muted, #888); border: 1px solid var(--ds-color-border, #333); border-radius: 999px; padding: 0 .4rem; margin-right: .2rem; }
  .thinking summary { cursor: pointer; display: inline-flex; gap: .3rem; align-items: center; list-style: none; font-size: .74rem; color: var(--ds-color-muted, #888); }
  .thinking p { margin: .2rem 0 0; padding: .3rem .5rem; border-left: 2px solid var(--ds-color-border, #333); border-radius: 4px; background: var(--ds-color-field, #1a1a1a); font-size: .72rem; max-height: 10rem; overflow: auto; }
  .call { display: grid; gap: .2rem; }
  .call-head { display: flex; gap: .5rem; align-items: baseline; min-width: 0; }
  .raw { font-family: ui-monospace, monospace; font-size: .68rem; color: var(--ds-color-text, #eee); }
  .call-args { font-family: ui-monospace, monospace; font-size: .66rem; color: var(--ds-color-muted, #888); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .result { margin: 0; max-height: 8rem; overflow: auto; padding: .3rem .5rem; border-radius: 4px; background: var(--ds-color-field, #1a1a1a); font-size: .68rem; white-space: pre-wrap; color: var(--ds-color-muted, #aaa); }
  .result.err { color: var(--ds-color-text, #eee); border-left: 2px solid var(--ds-color-danger, #e5484d); }
  .children { display: grid; gap: 2px; margin-top: .2rem; padding-left: .6rem; border-left: 1px solid var(--ds-color-border, #333); }
  .step.child { display: flex; align-items: center; gap: .4rem; padding: .15rem .2rem; font-size: .74rem; }
  .step.child .step-text { flex: 1; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .muted { color: var(--ds-color-muted, #888); }
</style>
