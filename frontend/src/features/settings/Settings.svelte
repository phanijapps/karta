<script lang="ts">
  import { ArrowLeft, X, Server, Plug, BookOpen, ScrollText, SlidersHorizontal, RotateCcw } from '@lucide/svelte';
  import Modal from '../../shared/ui/Modal.svelte';
  import { sections, settingsFixtures, mcps, skills, type Section } from './fixtures';
  let { onclose }: { onclose: () => void } = $props();
  let section = $state<Section>('Providers');
  let draft = $state(settingsFixtures());
  let providerIndex = $state(0);
  let mcpIndex = $state(0);
  let skillIndex = $state(0);
  const icons = [Server, Plug, BookOpen, ScrollText, SlidersHorizontal];
</script>

<Modal label="Settings" {onclose}>
  <div class="settings-layout">
    <nav class="settings-nav" aria-label="Settings sections">
      <button class="back" onclick={onclose}><ArrowLeft size={16} /> Back to workspace</button>
      <h1>Settings</h1>
      <div class="section-links">
        {#each sections as item, i}
          {@const Icon = icons[i]}
          <button class:active={section === item} aria-current={section === item ? 'page' : undefined} onclick={() => section = item}><Icon size={16} strokeWidth={1.6} />{item}</button>
        {/each}
      </div>
      <div class="nav-footer"><span class="brand">Karta</span><span class="muted">Your local workspace</span></div>
    </nav>
    <section class="settings-content" aria-label={`${section} settings`}>
      <header><div><h2>{section}</h2><p class="muted">{section === 'Providers' ? 'Choose how your workspace connects to models.' : section === 'MCP' ? 'Give your agents a way to work with your tools.' : section === 'Skills' ? 'Reusable knowledge for the way you work.' : section === 'Logging' ? 'Understand what’s happening in your workspace.' : 'Fine-tune the foundations of your workspace.'}</p></div><button class="icon-button" onclick={onclose} aria-label="Close Settings"><X size={20} /></button></header>
      <div class="notice"><span class="badge">Shell preview</span><p>Changes are not saved. These are illustrative values, not your installed configuration.</p></div>
      {#if section === 'Providers'}
        <div class="section-title"><h3>Model providers</h3><span class="badge">Proposed fields</span></div>
        <p class="source">Conceptual source: <code>~/.zbot/config.yaml</code> · Provider fields are not yet supported by the daemon schema.</p>
        <div class="item-list">{#each draft.providers as provider, i}<button class:selected={providerIndex === i} aria-pressed={providerIndex === i} onclick={() => providerIndex = i}><Server size={18} /><span><strong>{provider.name}</strong><small>{provider.type}</small></span><span class="badge">Mock</span></button>{/each}</div>
        <div class="fields">
          <label>Provider name<input bind:value={draft.providers[providerIndex].name} /></label>
          <label>Connection type<select bind:value={draft.providers[providerIndex].type}><option>OpenAI-compatible</option><option>Local runtime</option></select></label>
          <label class="full">Base URL<input bind:value={draft.providers[providerIndex].url} spellcheck="false" /></label>
          <label class="full">Model identifiers<input bind:value={draft.providers[providerIndex].models} /><small class="muted">Preview only. Models are not discovered from this endpoint.</small></label>
          <label class="full">Credential environment-variable reference<input bind:value={draft.providers[providerIndex].credential} spellcheck="false" /><small class="muted">Use an example variable name, not a secret. No credentials are read.</small></label>
        </div>
        <button class="secondary" disabled>Test connection unavailable</button>
      {:else if section === 'MCP'}
        <h3>Tool servers</h3><p class="source">Conceptual source: <code>~/.zbot/config/mcps/</code></p>
        <div class="item-list">{#each mcps as server, i}<button class:selected={mcpIndex === i} aria-pressed={mcpIndex === i} onclick={() => mcpIndex = i}><Plug size={18} /><span><strong>{server.name}</strong><small>{server.transport}</small></span><span class="badge">Mock</span></button>{/each}</div>
        <div class="detail"><h3>{mcps[mcpIndex].name}</h3><p>{mcps[mcpIndex].description}</p><label>Transport<input readonly value={mcps[mcpIndex].transport} /></label><label>{mcps[mcpIndex].transport === 'stdio' ? 'Command preview' : 'URL preview'}<input readonly value={mcps[mcpIndex].value} /></label></div>
        <button class="secondary" disabled>Connect unavailable</button>
      {:else if section === 'Skills'}
        <h3>Workspace skills</h3><p class="source">Conceptual source: <code>~/.zbot/config/skills/</code></p>
        <div class="item-list">{#each skills as skill, i}<button class:selected={skillIndex === i} aria-pressed={skillIndex === i} onclick={() => skillIndex = i}><BookOpen size={18} /><span><strong>{skill.name}</strong><small>{skill.description}</small></span><span class="badge">Mock</span></button>{/each}</div>
        <div class="detail"><h3>{skills[skillIndex].name}</h3><p>{skills[skillIndex].description}</p><p class="source"><code>{skills[skillIndex].path}</code></p><p class="muted">This is an illustrative skill, not an installed registry entry.</p></div>
        <button class="secondary" disabled>Install skill unavailable</button>
      {:else if section === 'Logging'}
        <h3>Diagnostics</h3><p class="source">Proposed UI-only settings. No daemon logging configuration is changed.</p>
        <div class="fields"><label class="full">Log level<select bind:value={draft.level}><option>Debug</option><option>Info</option><option>Warn</option><option>Error</option></select></label><label class="checkbox full"><input type="checkbox" bind:checked={draft.diagnostics} />Detailed diagnostics preview</label></div>
        <p class="description muted">Logging integration is not implemented. This shell does not collect logs or diagnostic data.</p>
        <div class="actions"><button class="secondary" disabled>Live tail unavailable</button><button class="secondary" disabled>Export unavailable</button><button class="secondary" disabled>Open log folder unavailable</button></div>
      {:else}
        <h3>Runtime configuration</h3><p class="source">Existing configuration concepts · <code>~/.zbot/config.yaml</code> · Values below are fixtures, not loaded configuration.</p>
        <div class="fields"><label>Engine<input bind:value={draft.engine} /></label><label>Server address<input bind:value={draft.address} /></label><label>Max context tokens<input type="number" min="1" bind:value={draft.maxTokens} /></label><label>Reserved output tokens<input type="number" min="1" bind:value={draft.reserveTokens} /></label><label class="full">Wards root<input bind:value={draft.wards} /></label></div>
        <div class="actions"><button class="secondary" disabled>Restart unavailable</button><button class="secondary" disabled>Open config unavailable</button><button class="secondary" disabled>Reset daemon unavailable</button></div>
      {/if}
      <footer><p class="muted">Drafts are discarded when you close Settings.</p><button class="secondary" onclick={() => draft = settingsFixtures()}><RotateCcw size={14} />Reset preview</button></footer>
    </section>
  </div>
</Modal>

<style>
  .settings-layout { height: 100%; display: grid; grid-template-columns: 240px minmax(0, 1fr); }
  .settings-nav { display: flex; flex-direction: column; gap: var(--ds-space-4); padding: var(--ds-space-4); border-right: 1px solid var(--ds-color-border); min-height: 0; overflow-y: auto; }
  .back { display: flex; align-items: center; gap: var(--ds-space-2); min-height: 32px; color: var(--ds-color-muted); font-size: var(--ds-font-nav); }
  h1 { font-size: 1.25rem; padding: var(--ds-space-2); font-weight: 600; }
  .section-links { display: flex; flex-direction: column; gap: var(--ds-space-1); }
  .section-links button { display: flex; align-items: center; gap: var(--ds-space-3); padding: var(--ds-space-2) var(--ds-space-3); min-height: 36px; font-size: var(--ds-font-nav); text-align: left; color: var(--ds-color-muted); }
  .section-links .active { background: var(--ds-color-hover); color: var(--ds-color-text); font-weight: 600; }
  .nav-footer { margin-top: auto; padding: var(--ds-space-2); display: grid; gap: var(--ds-space-1); font-size: var(--ds-font-nav); }
  .brand { font-weight: 650; }
  .settings-content { overflow-y: auto; padding: var(--ds-space-8); }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: var(--ds-space-4); margin-bottom: var(--ds-space-6); }
  h2 { font-size: 1.375rem; font-weight: 600; margin-bottom: var(--ds-space-2); letter-spacing: -0.025em; }
  h3 { font-size: var(--ds-font-control); font-weight: 600; }
  .notice { display: flex; align-items: center; gap: var(--ds-space-3); border: 1px solid var(--ds-color-border); padding: var(--ds-space-3); border-radius: var(--ds-radius-control); margin-bottom: var(--ds-space-8); font-size: var(--ds-font-nav); color: var(--ds-color-muted); }
  .section-title { display: flex; align-items: center; gap: var(--ds-space-3); }
  .source { color: var(--ds-color-muted); font-size: var(--ds-font-nav); line-height: 1.6; margin: var(--ds-space-2) 0 var(--ds-space-6); overflow-wrap: anywhere; }
  .item-list { display: grid; gap: var(--ds-space-2); margin: var(--ds-space-4) 0 var(--ds-space-6); }
  .item-list button { display: flex; align-items: center; text-align: left; gap: var(--ds-space-3); padding: var(--ds-space-3); border: 1px solid var(--ds-color-border); }
  .item-list button.selected { background: var(--ds-color-hover); }
  .item-list span:not(.badge) { flex: 1; min-width: 0; }
  .item-list strong { font-weight: 550; overflow-wrap: anywhere; }
  small { display: block; font-size: 0.75rem; color: var(--ds-color-muted); margin-top: var(--ds-space-1); }
  .fields { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ds-space-6) var(--ds-space-4); margin: var(--ds-space-6) 0; }
  .full { grid-column: 1 / -1; }
  .checkbox { display: flex; align-items: center; min-height: 36px; }
  .checkbox input { width: 18px; min-height: 18px; }
  .secondary { display: inline-flex; align-items: center; justify-content: center; gap: var(--ds-space-2); padding: var(--ds-space-2) var(--ds-space-3); min-height: 36px; border: 1px solid var(--ds-color-border); font-size: var(--ds-font-nav); }
  .detail { display: grid; gap: var(--ds-space-4); margin-bottom: var(--ds-space-6); }
  .actions { display: flex; gap: var(--ds-space-2); flex-wrap: wrap; margin: var(--ds-space-6) 0; }
  .description { line-height: 1.7; }
  footer { display: flex; align-items: center; justify-content: space-between; gap: var(--ds-space-4); margin-top: var(--ds-space-8); padding-top: var(--ds-space-4); border-top: 1px solid var(--ds-color-border); font-size: var(--ds-font-nav); }
  @media (max-width: 767px) {
    .settings-layout { grid-template-columns: minmax(0, 1fr); grid-template-rows: auto minmax(0, 1fr); }
    .settings-nav { padding: var(--ds-space-3); gap: var(--ds-space-2); border-right: 0; border-bottom: 1px solid var(--ds-color-border); }
    .settings-nav h1, .nav-footer { display: none; }
    .section-links { flex-direction: row; overflow-x: auto; }
    .section-links button { flex-shrink: 0; }
    .settings-content { padding: var(--ds-space-4); }
    .fields { grid-template-columns: minmax(0, 1fr); }
    .notice, footer { flex-wrap: wrap; }
  }
</style>
