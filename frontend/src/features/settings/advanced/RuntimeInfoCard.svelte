<script lang="ts">
  import { Server } from '@lucide/svelte';
  import CapabilityCard from './CapabilityCard.svelte';

  let open = $state(false);

  // Read-only runtime info from config.yaml — the daemon doesn't expose
  // a runtime info endpoint yet; these are the config-file values,
  // honestly labeled. When a daemon endpoint lands, swap the source.
  const runtime = {
    Engine: 'eino',
    'Server address': '0.0.0.0:8787',
    'Max context tokens': '128,000',
    'Reserved output tokens': '8,192',
    'Wards root': 'wards',
  };
</script>

<CapabilityCard
  icon={Server}
  title="Runtime configuration"
  description="Daemon configuration and context budgets"
  badge="config.yaml"
  summary="eino · 0.0.0.0:8787"
  bind:open
>
  <p class="muted" style="font-size:var(--ds-font-nav);margin-bottom:var(--ds-space-3)">
    Values from <code>~/.zbot/config.yaml</code> — edit the file and restart to change.
  </p>
  <dl class="kv">
    {#each Object.entries(runtime) as [key, value]}
      <dt>{key}</dt>
      <dd>{value}</dd>
    {/each}
  </dl>
</CapabilityCard>
