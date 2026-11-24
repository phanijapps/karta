<script lang="ts">
  import { X, FolderOpen, Layers, Users } from '@lucide/svelte';
  import { getWard, type Ward } from '../shared/api/client';
  import type { Sessions } from '../entities/session/session.svelte';

  let { sessions, onclose }: { sessions: Sessions; onclose: () => void } = $props();

  let wardDetail = $state<Ward | null>(null);
  let wardLoadError = $state('');

  // The active session's ward binding drives the ward card.
  const ward = $derived(sessions.activeWard);
  $effect(() => {
    const name = ward;
    wardDetail = null;
    wardLoadError = '';
    if (!name) return;
    getWard(name)
      .then((w) => {
        if (w) wardDetail = w;
        else wardLoadError = `Ward ${name} is no longer in the vault.`;
      })
      .catch(() => {
        wardLoadError = 'Ward detail unavailable.';
      });
  });

  // Provisioned skills + the delegation roster derive from the
  // transcript: the latest intent_analysis names the skills; delegate
  // tool-call intents name the agents.
  const analysis = $derived.by(() => {
    for (let i = sessions.messages.length - 1; i >= 0; i--) {
      const m = sessions.messages[i];
      if (m.role === 'tool' && m.tool_name === 'intent_analysis') {
        try {
          const c = JSON.parse(m.content ?? '') as Record<string, any>;
          return c.intent_analysis ?? c;
        } catch {
          return null;
        }
      }
    }
    return null;
  });
  const skills = $derived<string[]>((analysis?.recommended_skills as string[]) ?? []);
  const agents = $derived.by<{ agent: string; task: string }[]>(() => {
    const out: { agent: string; task: string }[] = [];
    for (const m of sessions.messages) {
      if (m.role !== 'assistant' || !m.tool_calls) continue;
      try {
        const calls = JSON.parse(m.tool_calls) as Array<{ function?: { name?: string; arguments?: string } }>;
        for (const c of calls) {
          if (c.function?.name !== 'delegate_agent') continue;
          try {
            const args = JSON.parse(c.function.arguments ?? '{}') as { agent?: string; task?: string };
            if (args.agent) out.push({ agent: args.agent, task: args.task ?? '' });
          } catch { /* skip malformed */ }
        }
      } catch { /* skip malformed */ }
    }
    return out;
  });

  /** Render the ward's layout node ids as a compact tree list. */
  function layoutLines(node: unknown, depth: number, out: string[]): void {
    if (!node || typeof node !== 'object' || out.length > 24) return;
    const n = node as { id?: string; kind?: string; children?: unknown[] };
    const label = n.id ?? (n.kind === 'directory' ? '·' : 'file');
    out.push(`${'  '.repeat(depth)}${n.kind === 'directory' ? '▸' : '·'} ${label}`);
    for (const child of n.children ?? []) layoutLines(child, depth + 1, out);
  }
  const layout = $derived.by(() => {
    if (!wardDetail?.layout) return [];
    const out: string[] = [];
    layoutLines(wardDetail.layout, 0, out);
    return out;
  });
</script>

<aside class="context" aria-label="Session context">
  <header>
    <h2>Context</h2>
    <button class="icon-button" onclick={onclose} aria-label="Close right sidebar"><X size={18} /></button>
  </header>
  <div class="context-body">
    {#if ward}
      <section class="ctx-section" aria-label="Active ward">
        <p class="ctx-title"><FolderOpen size={12} aria-hidden="true" /> Active Ward</p>
        {#if wardDetail}
          <div class="ward-card">
            <p class="name">{wardDetail.name}</p>
            <p class="type">{wardDetail.archetype} archetype · lint {wardDetail.lint_status}</p>
            {#if wardDetail.canonical_title}
              <p class="canonical">{wardDetail.canonical_title}</p>
            {/if}
            {#if layout.length}
              <div class="ward-files" aria-label="Ward layout">
                {#each layout as line}
                  <code>{line}</code>
                {/each}
              </div>
            {/if}
          </div>
        {:else if wardLoadError}
          <p class="muted small">{wardLoadError}</p>
        {:else}
          <p class="muted small">Loading ward detail…</p>
        {/if}
      </section>
    {:else if sessions.isResearch}
      <section class="ctx-section" aria-label="Active ward">
        <p class="ctx-title"><FolderOpen size={12} aria-hidden="true" /> Active Ward</p>
        <p class="muted small">No ward yet — this session is a shell. The model enters a ward via ward_use when the research calls for it.</p>
      </section>
    {/if}

    <section class="ctx-section" aria-label="Provisioned skills">
      <p class="ctx-title"><Layers size={12} aria-hidden="true" /> Provisioned Skills</p>
      {#if skills.length}
        {#each skills as s}
          <p class="ctx-row"><span class="dot skill" aria-hidden="true"></span> {s}</p>
        {/each}
      {:else}
        <p class="muted small">{sessions.isResearch ? 'Skills provision on the next exchange (intent analysis selects them).' : 'Quick chat provisions no skills — it runs lean.'}</p>
      {/if}
    </section>

    <section class="ctx-section" aria-label="Agents">
      <p class="ctx-title"><Users size={12} aria-hidden="true" /> Agents</p>
      {#if agents.length}
        {#each agents as a}
          <p class="ctx-row"><span class="dot agent" aria-hidden="true"></span> {a.agent}{#if a.task}<span class="muted"> — {a.task}</span>{/if}</p>
        {/each}
      {:else}
        <p class="muted small">{sessions.isResearch ? 'No delegations yet in this session.' : 'Delegation is a research-session capability.'}</p>
      {/if}
    </section>
  </div>
  <footer>Local by design. Yours to control.</footer>
</aside>

<style>
  .context { height: 100%; display: flex; flex-direction: column; border-left: 1px solid var(--ds-color-border); background: var(--ds-color-background); }
  header { height: 56px; display: flex; align-items: center; justify-content: space-between; padding: var(--ds-space-2) var(--ds-space-3); border-bottom: 1px solid var(--ds-color-border); }
  h2 { font-size: var(--ds-font-chat); font-weight: 550; }
  .context-body { flex: 1; min-height: 0; overflow-y: auto; padding: var(--ds-space-4); display: flex; flex-direction: column; gap: var(--ds-space-6); color: var(--ds-color-muted); font-size: var(--ds-font-control); line-height: 1.7; }
  .ctx-section { display: grid; gap: var(--ds-space-2); align-content: start; }
  .ctx-title { display: flex; align-items: center; gap: var(--ds-space-2); margin: 0; font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.07em; color: var(--ds-color-muted); }
  .ward-card { border: 1px solid color-mix(in oklch, var(--ds-color-success, #22c55e) 25%, transparent); border-radius: 10px; padding: var(--ds-space-3); background: var(--ds-color-surface, #1e1e1e); display: grid; gap: var(--ds-space-2); }
  .ward-card .name { margin: 0; font-weight: 600; color: var(--ds-color-success, #22c55e); }
  .ward-card .type { margin: 0; font-size: 0.72rem; color: var(--ds-color-muted); }
  .ward-card .canonical { margin: 0; font-size: 0.78rem; padding: var(--ds-space-2); border-left: 2px solid var(--ds-color-success, #22c55e); border-radius: 4px; background: var(--ds-color-field, #1a1a1a); color: var(--ds-color-text); }
  .ward-files { display: grid; gap: 2px; font-size: 0.7rem; }
  .ward-files code { font-family: ui-monospace, monospace; white-space: pre; color: var(--ds-color-muted); }
  .ctx-row { display: flex; align-items: center; gap: var(--ds-space-2); margin: 0; padding: var(--ds-space-1) var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: 6px; background: var(--ds-color-surface, #1e1e1e); font-size: 0.78rem; }
  .dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
  .dot.skill { background: var(--ds-color-primary, #6366f1); }
  .dot.agent { background: var(--ds-color-success, #22c55e); }
  .muted { color: var(--ds-color-muted); }
  .small { font-size: 0.75rem; margin: 0; }
  footer { padding: var(--ds-space-4); font-size: 0.6875rem; color: var(--ds-color-muted); }
</style>
