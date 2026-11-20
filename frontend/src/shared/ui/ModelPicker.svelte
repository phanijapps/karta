<script lang="ts">
  // Catalog-backed model picker: reads the daemon's in-memory model
  // catalog (instant), groups by provider, holds NO local selection state
  // (callers bind), and never falls back to fixture models. Option values
  // are provider-qualified ("provider/model") so selection preserves
  // which provider was chosen; displayed text stays the raw model id.
  import { getModels, type CatalogModel } from '../api/client';

  let { value = $bindable(''), id, label = 'Model' }: { value?: string; id: string; label?: string } = $props();

  let models = $state<CatalogModel[]>([]);
  let loading = $state(true);
  let failed = $state(false);
  let requestGen = 0; // fences overlapping loads: only the latest commits

  const qualified = (m: CatalogModel) => `${m.provider}/${m.id}`;

  $effect(() => {
    void load();
    // Manual-refresh coherence: Settings refreshes and provider mutations
    // announce themselves so mounted pickers re-read the catalog without
    // polling.
    window.addEventListener('karta:models-refreshed', handleRefreshed);
    return () => {
      window.removeEventListener('karta:models-refreshed', handleRefreshed);
      requestGen++; // invalidate any in-flight load from this instance
    };
  });

  function handleRefreshed() {
    void load();
  }

  async function load() {
    const gen = ++requestGen;
    loading = true;
    try {
      const fetched = await getModels();
      if (gen !== requestGen) return; // a newer load owns the state
      models = fetched;
      failed = false;
    } catch {
      if (gen !== requestGen) return;
      failed = true;
    } finally {
      if (gen === requestGen) loading = false;
    }
  }

  // Reconcile the selection against the latest catalog: a model removed
  // by a refresh (or a provider deleted) clears the selection instead of
  // silently pointing at nothing.
  $effect(() => {
    if (loading || failed) return;
    if (value !== '' && !models.some((m) => qualified(m) === value)) {
      value = '';
    }
  });

  // Group entries by provider in catalog order.
  const groups = $derived(() => {
    const byProvider = new Map<string, CatalogModel[]>();
    for (const m of models) {
      const list = byProvider.get(m.provider) ?? [];
      list.push(m);
      byProvider.set(m.provider, list);
    }
    return [...byProvider.entries()];
  });
</script>

<div class="model-picker">
  <label class="sr-only" for={id}>{label}</label>
  {#if loading}
    <select id={id} disabled aria-busy="true"><option>Loading models…</option></select>
  {:else if failed}
    <select id={id} disabled><option>Daemon unreachable — retry in Settings</option></select>
  {:else if models.length === 0}
    <select id={id} disabled><option>No models yet — refresh in Settings → Providers</option></select>
  {:else}
    <select id={id} aria-label={label} onchange={(e) => (value = e.currentTarget.value)}>
      {#if value === ''}<option value="" selected>Select a model</option>{/if}
      {#each groups() as [provider, entries] (provider)}
        <optgroup label={provider}>
          {#each entries as m (qualified(m))}<option value={qualified(m)} selected={value === qualified(m)}>{m.id}</option>{/each}
        </optgroup>
      {/each}
    </select>
  {/if}
  <span class="badge" title="Replies are simulated until the agent engine lands">Simulated</span>
</div>

<style>
  /* Compact trigger matching the Open WebUI reference selector:
     13px text, ~28px tall, rounded, muted, truncating label. */
  .model-picker { display: flex; align-items: center; gap: var(--ds-space-1); min-width: 0; }
  select {
    font-size: 0.8125rem;
    font-weight: 450;
    min-height: 28px;
    padding: 2px 8px;
    width: auto;
    max-width: 180px;
    border: 0;
    border-radius: var(--ds-radius-control);
    background: transparent;
    color: var(--ds-color-muted);
    cursor: pointer;
    transition: background 100ms, color 100ms;
  }
  select:hover { background: var(--ds-color-hover); color: var(--ds-color-text); }
  select:disabled { color: var(--ds-color-muted); cursor: default; background: transparent; }
  .badge { font-size: 0.6875rem; border: 1px solid var(--ds-color-border); border-radius: 999px; padding: 2px var(--ds-space-2); color: var(--ds-color-muted); white-space: nowrap; }
  @media (max-width: 767px) { select { max-width: 140px; } .badge { display: none; } }
</style>
