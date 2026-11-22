<script lang="ts">
  import { Bot, FileCode2 } from '@lucide/svelte';
  import { listAgents, type AgentEntry } from '../../shared/api/client';

  let agents = $state<AgentEntry[]>([]);
  let loaded = $state(false);

  $effect(() => {
    void (async () => {
      agents = await listAgents();
      loaded = true;
    })();
  });
</script>

{#if !loaded}
  <p class="muted">Loading the agent roster…</p>
{:else if agents.length === 0}
  <div class="empty">
    <Bot size={28} strokeWidth={1.2} />
    <h3>No agents configured</h3>
    <p class="muted">Create a markdown file with YAML frontmatter under <code>~/.zbot/agents/</code> — the body becomes the agent's prompt. Example:</p>
    <pre><code>---
name: research-agent
description: Gathers and synthesizes evidence.
model: provider/model-id   # optional
tools: []                  # optional allowlist
max_depth: 0               # delegation grant
---
You research assigned tasks…</code></pre>
  </div>
{:else}
  <ul class="agent-list" aria-label="Configured agents">
    {#each agents as a (a.name)}
      <li class="agent-card">
        <div class="agent-head">
          <Bot size={16} aria-hidden="true" />
          <strong>{a.display_name ?? a.name}</strong>
          {#if a.name !== (a.display_name ?? a.name)}<code class="muted">{a.name}</code>{/if}
          {#if a.max_depth !== undefined && a.max_depth > 0}
            <span class="badge" title={`May delegate to depth ${a.max_depth}`}>delegates</span>
          {/if}
        </div>
        {#if a.description}<p class="muted">{a.description}</p>{/if}
        <dl class="agent-meta">
          {#if a.model}<div><dt>Model</dt><dd><code>{a.model}</code></dd></div>{/if}
          <div>
            <dt>Tools</dt>
            <dd>{a.tools === undefined ? 'all tools' : a.tools.length === 0 ? 'none' : a.tools.join(', ')}</dd>
          </div>
          <div><dt>Delegation</dt><dd>{a.max_depth ? `depth grant ${a.max_depth}` : 'cannot delegate'}</dd></div>
        </dl>
        {#if a.source}<p class="source"><FileCode2 size={12} aria-hidden="true" /> {a.source}</p>{/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .empty { text-align: center; padding: 2rem 1rem; display: grid; gap: .5rem; justify-items: center; }
  .empty pre { text-align: left; max-width: 32rem; overflow-x: auto; }
  .agent-list { list-style: none; margin: 0; padding: 0; display: grid; gap: .75rem; }
  .agent-card { border: 1px solid var(--border, #333); border-radius: 10px; padding: .9rem 1rem; display: grid; gap: .4rem; }
  .agent-head { display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; }
  .badge { font-size: .72rem; padding: .1rem .5rem; border-radius: 99px; border: 1px solid var(--border, #333); color: var(--tx-muted, #888); }
  .agent-meta { display: flex; gap: 1.5rem; flex-wrap: wrap; margin: 0; font-size: .85rem; }
  .agent-meta div { display: grid; gap: .1rem; }
  .agent-meta dt { color: var(--tx-muted, #888); font-size: .72rem; text-transform: uppercase; letter-spacing: .04em; }
  .agent-meta dd { margin: 0; }
  .source { display: inline-flex; gap: .35rem; align-items: center; font-size: .75rem; color: var(--tx-muted, #888); margin: 0; }
</style>
