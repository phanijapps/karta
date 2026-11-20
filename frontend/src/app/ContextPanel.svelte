<script lang="ts">
  import { X, Layers, Activity, FileText } from '@lucide/svelte';
  let { onclose }: { onclose: () => void } = $props();
  let selected = $state('Context');
</script>
<aside class="context" aria-label="Session context">
  <header><div class="switcher" role="group" aria-label="Context views">{#each ['Context', 'Timeline'] as tab}<button class:active={selected === tab} aria-pressed={selected === tab} onclick={() => selected = tab}>{tab}</button>{/each}</div><button class="icon-button" onclick={onclose} aria-label="Close right sidebar"><X size={18} /></button></header>
  <div class="context-body">
    {#if selected === 'Context'}<Layers size={24} strokeWidth={1.2} /><h2>A place for context</h2><p>Sources, project files, and working context will appear here.</p><div class="placeholder-row"><FileText size={16} /><span>No sources connected</span></div>{:else}<Activity size={24} strokeWidth={1.2} /><h2>Follow the work</h2><p>Agent steps, tools, and decisions will appear in the Timeline Tree.</p>{/if}
    <span class="badge">TODO · Not implemented</span>
    <p class="small">This panel is a shell placeholder. No files are read and no agent events are running.</p>
  </div>
  <footer>Local by design. Yours to control.</footer>
</aside>
<style>
  .context { height: 100%; display: flex; flex-direction: column; border-left: 1px solid var(--ds-color-border); background: var(--ds-color-background); }
  header { height: 56px; display: flex; align-items: center; justify-content: space-between; padding: var(--ds-space-2) var(--ds-space-3); border-bottom: 1px solid var(--ds-color-border); }
  .switcher { display: flex; gap: var(--ds-space-1); }
  .switcher button { padding: var(--ds-space-2); font-size: var(--ds-font-nav); min-height: 32px; color: var(--ds-color-muted); }
  .switcher .active { color: var(--ds-color-text); background: var(--ds-color-hover); }
  .context-body { flex: 1; min-height: 0; overflow-y: auto; padding: var(--ds-space-8) var(--ds-space-6); display: flex; align-items: flex-start; flex-direction: column; gap: var(--ds-space-4); color: var(--ds-color-muted); font-size: var(--ds-font-control); line-height: 1.7; }
  h2 { font-size: var(--ds-font-chat); font-weight: 550; color: var(--ds-color-text); }
  .placeholder-row { display: flex; align-items: center; gap: var(--ds-space-2); border: 1px dashed var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-3); width: 100%; font-size: var(--ds-font-nav); }
  .small { font-size: 0.75rem; }
  footer { padding: var(--ds-space-4); font-size: 0.6875rem; color: var(--ds-color-muted); }
</style>
