<script lang="ts">
  import { Brain } from '@lucide/svelte';
  import CapabilityCard from './CapabilityCard.svelte';
  import { getDistillation, putDistillation, getModelCatalog } from '../../../shared/api/client';

  let open = $state(false);
  let provider = $state('');
  let model = $state('');
  let models = $state<Record<string, string[]>>({});
  let connected = $state<boolean | null>(null);
  let loading = $state(false);
  let saving = $state(false);
  let error = $state('');
  let saved = $state(false);
  let dirty = $state(false);

  const configured = $derived(connected === true && provider !== '' && model !== '');

  async function load() {
    loading = true;
    connected = null;
    try {
      const [settings, catalog] = await Promise.all([getDistillation(), getModelCatalog()]);
      provider = settings.provider ?? '';
      model = settings.model ?? '';
      const grouped: Record<string, string[]> = {};
      for (const m of catalog.models ?? []) {
        if (!grouped[m.provider]) grouped[m.provider] = [];
        grouped[m.provider].push(m.id);
      }
      models = grouped;
      connected = true;
      dirty = false;
    } catch {
      connected = false;
    } finally {
      loading = false;
    }
  }

  async function save() {
    saving = true;
    error = '';
    saved = false;
    try {
      // Prompt is NOT sent — it lives in the config file, managed outside
      // settings (owner decision). Omitting it preserves the saved value.
      await putDistillation({ provider, model });
      saved = true;
      dirty = false;
    } catch (e) {
      error = e instanceof Error ? e.message : 'Save failed';
    } finally {
      saving = false;
    }
  }

  function onProviderChange() {
    model = '';
    dirty = true;
  }

  $effect(() => { load(); });
</script>

<CapabilityCard
  icon={Brain}
  title="Distillation"
  description="Turn conversation history into lasting memory"
  badge={connected === false ? 'Daemon unreachable' : configured ? 'Configured' : loading ? 'Loading…' : 'Not configured'}
  badgeTone={connected === false ? 'warn' : configured ? 'ok' : ''}
  summary={configured ? `${provider} · ${model}` : ''}
  bind:open
>
  {#if connected === false}
    <p class="muted" role="status">Daemon unreachable — start zbotd and reopen Settings.</p>
    <div class="actions"><button class="secondary" onclick={() => void load()}>Retry</button></div>
  {:else if loading}
    <p class="muted" role="status">Loading…</p>
  {:else}
    <div class="fields">
      <label>Provider
        <select bind:value={provider} onchange={onProviderChange}>
          <option value="" disabled>Select provider</option>
          {#each Object.keys(models).sort() as p}
            <option value={p}>{p}</option>
          {/each}
        </select>
      </label>
      <label>Model
        <select bind:value={model} onchange={() => (dirty = true)}>
          <option value="" disabled>Select model</option>
          {#each models[provider] ?? [] as m}
            <option value={m}>{m}</option>
          {/each}
        </select>
      </label>
    </div>
    {#if Object.keys(models).length === 0}
      <p class="muted">No models available. Configure a provider and refresh its catalog in the Providers section.</p>
    {/if}
    <p class="muted" style="font-size:var(--ds-font-nav)">Prompt managed in <code>~/.zbot/config/distillation.json</code></p>
    {#if error}<p class="muted" role="alert" style="color:var(--ds-color-danger)">{error}</p>{/if}
    {#if saved}<p class="muted" role="status" style="color:var(--ds-color-success)">Saved — the next distillation pass uses these settings.</p>{/if}
    <div class="actions">
      <button class="primary" onclick={() => void save()} disabled={saving || !provider || !model || (!dirty && !saved)}>Save changes</button>
      {#if dirty}<button class="secondary" onclick={() => void load()}>Discard</button>{/if}
    </div>
  {/if}
</CapabilityCard>
