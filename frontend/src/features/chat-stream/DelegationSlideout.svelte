<script lang="ts">
  // The delegation slideout (delegation-pills): a drawer over the chat
  // area showing one delegation — the agent and its task, its cost when
  // the session's executions know it, the agent's calls (the Work tab's
  // step tree over the delegation's rows), and the report it returned.
  // Focus moves in on open and returns to the pill on close; Escape and
  // the close button close it.
  import { X, Users } from '@lucide/svelte';
  import WorkSteps from './WorkSteps.svelte';
  import { renderMarkdown } from '../../shared/markdown/render';
  import { fmtDuration } from '../../entities/timeline-event/duration';
  import { fmtTokens } from '../../entities/timeline-event/analysisText';
  import { taskGist, type DelegationGroup } from './steps';
  import type { SessionExecution } from '../../shared/api/client';

  let {
    group,
    executions = [],
    onclose,
  }: {
    group: DelegationGroup;
    executions?: SessionExecution[];
    onclose: () => void;
  } = $props();

  let panel = $state<HTMLDivElement | null>(null);
  const groupGist = $derived(taskGist(group.task, 160) || `Delegated to ${group.agent}`);

  // The child's execution row (cost/status), matched by execution id.
  const exec = $derived(
    group.executionId ? executions.find((e) => e.id === group.executionId) : undefined,
  );
  const cost = $derived.by<string>(() => {
    const e = exec;
    if (!e) return group.done ? 'completed' : 'running';
    const parts = [e.status];
    if (e.tokens_in || e.tokens_out) parts.push(`${fmtTokens(e.tokens_in)} → ${fmtTokens(e.tokens_out)} tokens`);
    if (e.started_at && e.finished_at) {
      const d = fmtDuration(Date.parse(e.finished_at) - Date.parse(e.started_at));
      if (d) parts.push(d);
    }
    return parts.join(' · ');
  });

  $effect(() => {
    panel?.focus(); // focus moves in; the pill gets it back on close
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={onclose}>
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="slideout"
    role="dialog"
    aria-modal="true"
    aria-label={`Delegation to ${group.agent}`}
    tabindex="-1"
    bind:this={panel}
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => {
      // Escape closes (handled here: focus lives inside the panel).
      if (e.key === 'Escape') {
        onclose();
        return;
      }
      // Modal by declaration, modal by behaviour: Tab wraps inside the
      // panel so focus never leaves the drawer while it is open.
      if (e.key === 'Tab' && panel) {
        // Native <summary> is focusable too (the task disclosure).
        const focusables = [...panel.querySelectorAll<HTMLElement>('button, [href], input, select, textarea, summary, [tabindex]:not([tabindex="-1"])')]
          .filter((el) => !el.hasAttribute('disabled') && el.offsetParent !== null);
        if (focusables.length === 0) {
          e.preventDefault();
          return;
        }
        const first = focusables[0];
        const last = focusables[focusables.length - 1];
        const active = document.activeElement as HTMLElement | null;
        if (e.shiftKey && (active === first || active === panel)) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && active === last) {
          e.preventDefault();
          first.focus();
        }
      }
    }}
  >
    <header class="head">
      <span class="who"><Users size={16} aria-hidden="true" /><strong>{group.agent}</strong></span>
      <span class="cost">{cost}</span>
      <button type="button" class="icon-button" aria-label="Close delegation" onclick={onclose}><X size={16} /></button>
    </header>
    {#if group.task}
      <!-- A one-line gist; the full task on demand (it can run to
           thousands of characters, ward preamble included). -->
      <details class="task">
        <summary title={group.task}>{groupGist}</summary>
        <pre class="task-full">{group.task}</pre>
      </details>
    {/if}
    <section class="calls" aria-label="Calls">
      <h4>Calls</h4>
      {#if group.rows.some((r) => r.role === 'assistant' && r.tool_calls)}
        <WorkSteps messages={group.rows} />
      {:else}
        <p class="calls-empty">{group.done ? 'No tool calls — the agent answered directly.' : 'No tool calls yet.'}</p>
      {/if}
    </section>
    <section class="report" aria-label="Report">
      <h4>Report</h4>
      {#if group.done}
        <div class="report-body">{@html renderMarkdown(group.report)}</div>
      {:else}
        <p class="muted small">Running — the report lands here when the agent returns.</p>
      {/if}
    </section>
  </div>
</div>

<style>
  .backdrop { position: fixed; inset: 0; background: rgb(0 0 0 / .35); z-index: 40; display: flex; justify-content: flex-end; }
  .slideout { width: min(540px, 92vw); height: 100%; background: var(--ds-color-surface, #1e1e1e); border-left: 1px solid var(--ds-color-border, #333); box-shadow: -12px 0 32px rgb(0 0 0 / .35); display: flex; flex-direction: column; outline: none; animation: slide-in .18s ease-out; }
  @keyframes slide-in { from { transform: translateX(24px); opacity: 0; } to { transform: none; opacity: 1; } }
  @media (prefers-reduced-motion: reduce) { .slideout { animation: none; } }
  .head { display: flex; align-items: center; gap: .6rem; padding: .8rem 1rem; border-bottom: 1px solid var(--ds-color-border, #333); }
  .who { display: inline-flex; align-items: center; gap: .45rem; color: var(--ds-color-primary, #3b82f6); }
  .who strong { color: var(--ds-color-text, #f4f4f4); }
  .cost { margin-left: auto; color: var(--ds-color-muted, #888); font-size: .78rem; font-variant-numeric: tabular-nums; }
  .task { margin: 0; padding: .55rem 1rem; color: var(--ds-color-muted, #888); font-size: .82rem; border-bottom: 1px dashed var(--ds-color-border, #333); }
  .task > summary { cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .task-full { margin: .5rem 0 0; padding: .6rem .7rem; max-height: 12rem; overflow: auto; white-space: pre-wrap; font: inherit; font-size: .78rem; border-radius: 8px; background: var(--ds-color-hover, #2a2a2a); }
  .calls, .report { padding: .7rem 1rem; overflow: auto; }
  .calls-empty { margin: 0; color: var(--ds-color-muted, #888); font-size: .8rem; }
  .calls { flex: 0 0 auto; max-height: 45%; }
  .report { flex: 1 1 auto; border-top: 1px solid var(--ds-color-border, #333); }
  h4 { margin: 0 0 .4rem; font-size: .66rem; text-transform: uppercase; letter-spacing: .06em; color: var(--ds-color-muted, #888); }
  .report-body { font-size: .9rem; line-height: 1.5; }
  .report-body :global(> :first-child) { margin-top: 0; }
  .muted { color: var(--ds-color-muted, #888); }
  .small { font-size: .78rem; }
</style>
