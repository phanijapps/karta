<script lang="ts">
  import { ChevronRight } from '@lucide/svelte';
  import type { Snippet } from 'svelte';
  import type { Component } from 'svelte';

  let {
    icon,
    title,
    description,
    badge,
    badgeTone = '',
    summary = '',
    open = $bindable(false),
    children,
  }: {
    icon: Component<{ size?: number; strokeWidth?: number }>;
    title: string;
    description: string;
    badge: string;
    badgeTone?: 'ok' | 'warn' | '';
    summary?: string;
    open?: boolean;
    children: Snippet;
  } = $props();
</script>

<div class="capability-card">
  <button class="card-head" aria-expanded={open} onclick={() => (open = !open)}>
    <span class="card-icon"><icon size={18} strokeWidth={1.6} /></span>
    <span class="card-meta">
      <span class="card-title">
        {title}
        {#if badge}<span class="badge" class:ok={badgeTone === 'ok'} class:warn={badgeTone === 'warn'}>{badge}</span>{/if}
      </span>
      <span class="card-desc">{description}</span>
    </span>
    {#if summary && !open}<span class="card-summary">{summary}</span>{/if}
    <ChevronRight size={16} class="card-chevron" strokeWidth={1.8} />
  </button>
  {#if open}
    <div class="card-body">
      {@render children()}
    </div>
  {/if}
</div>
