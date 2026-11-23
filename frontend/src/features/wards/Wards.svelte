<script lang="ts">
  import { Folder, FolderOpen, CircleAlert, CircleCheck, FileText, RefreshCw } from '@lucide/svelte';
  import { listWards, getWard, type WardSummary, type Ward, type WardNode, type WardFinding } from '../../shared/api/client';

  let wards = $state<WardSummary[]>([]);
  let vaultRoot = $state('');
  let loaded = $state(false);
  let listFailed = $state(false);
  let selected = $state<string | null>(null);
  // Request token: only the most recent detail request may render —
  // a slow earlier response must never overwrite the selected ward.
  let detailReq = 0;
  let detail = $state<Ward | null>(null);
  let detailState = $state<'idle' | 'loading' | 'loaded' | 'failed'>('idle');

  async function refresh() {
    listFailed = false;
    const roster = await listWards();
    if (roster === null) {
      listFailed = true;
    } else {
      wards = roster.wards;
      vaultRoot = roster.vaultRoot;
    }
    loaded = true;
  }

  $effect(() => { void refresh(); });

  function open(name: string) {
    if (selected === name) { selected = null; detailState = 'idle'; detail = null; return; }
    selected = name;
    void loadDetail(name);
  }

  async function loadDetail(name: string) {
    detail = null;
    detailState = 'loading';
    const req = ++detailReq;
    const w = await getWard(name);
    if (req !== detailReq || selected !== name) return; // a newer selection won
    if (w === null) { detailState = 'failed'; return; }
    detail = w;
    detailState = 'loaded';
  }

  function lintBadge(w: WardSummary): string {
    return w.lint_status === 'clean' ? 'clean' : `${w.finding_count} finding${w.finding_count === 1 ? '' : 's'}`;
  }

  /** Render the layout tree as indented lines (bounded depth guard). */
  function treeLines(node: WardNode, depth: number): { text: string; depth: number }[] {
    if (depth > 8) return [{ text: '…', depth }];
    const label = node.id ?? node.match ?? '(root)';
    const kind = node.kind === 'directory' ? 'dir' : 'file';
    const line = { text: `${kind}  ${label}${node.match && node.id ? `  (${node.match})` : ''}`, depth };
    const childLines = (node.children ?? []).flatMap((c) => treeLines(c, depth + 1));
    return [line, ...childLines];
  }

  function findingLabel(f: WardFinding): string {
    switch (f.kind) {
      case 'missing_required': return 'missing';
      case 'unexpected_entry': return 'unexpected';
      case 'format_mismatch': return 'format';
      case 'invalid_conf': return 'invalid conf';
      default: return f.kind;
    }
  }

  /** An unparseable conf is not a freeform ward — the UI must say which. */
  function hasInvalidConf(w: Ward): boolean {
    return w.findings.some((f) => f.kind === 'invalid_conf');
  }
</script>

<div class="wards-page">
  <header class="page-head">
    <div><h1>Wards</h1><p class="muted">Documented-knowledge workspaces in your vault. Wards are yours — Karta reads and lints them.</p></div>
  </header>

  {#if !loaded}
    <p class="muted" role="status">Loading wards…</p>
  {:else if listFailed}
    <div class="load-failed" role="alert">
      <CircleAlert size={20} aria-hidden="true" />
      <p>Couldn't read the ward vault. Check that the daemon is running and the vault path is readable.</p>
      <button class="retry" onclick={() => void refresh()}><RefreshCw size={14} aria-hidden="true" /> Retry</button>
    </div>
  {:else if wards.length === 0}
    <div class="empty" aria-label="Wards empty state">
      <Folder size={32} strokeWidth={1.2} />
      <h3>No wards yet</h3>
      {#if vaultRoot}
        <p class="muted">Your vault lives at <code>{vaultRoot}</code> — create a ward directory there (a <code>ward-conf.yaml</code> makes it lint-checked; without one it is a freeform ward).</p>
      {:else}
        <p class="muted">Create a ward directory in your vault to begin.</p>
      {/if}
      <p class="muted small">Point the vault elsewhere with <code>wards.vault_root</code> in config.yaml (absolute path; default <code>~/Documents/wards</code>).</p>
    </div>
  {:else}
    <ul class="ward-list" aria-label="Wards">
      {#each wards as w (w.name)}
        <li class="ward-card">
          <button class="ward-open" aria-expanded={selected === w.name} onclick={() => open(w.name)}>
            {#if selected === w.name}<FolderOpen size={18} aria-hidden="true" />{:else}<Folder size={18} aria-hidden="true" />{/if}
            <strong>{w.name}</strong>
            <span class="badge archetype" title={`Archetype: ${w.archetype}`}>{w.archetype}</span>
            {#if w.lint_status === 'clean'}
              <span class="badge lint-clean"><CircleCheck size={13} aria-hidden="true" /> clean</span>
            {:else}
              <span class="badge lint-dirty"><CircleAlert size={13} aria-hidden="true" /> {lintBadge(w)}</span>
            {/if}
            {#if w.canonical_title}
              <span class="canonical" title="Canonical page"><FileText size={13} aria-hidden="true" /> {w.canonical_title}</span>
            {/if}
            <span class="sr-only">Open ward {w.name}</span>
          </button>

          {#if selected === w.name}
            <div class="ward-detail">
              {#if detailState === 'loading'}
                <p class="muted" role="status">Loading ward…</p>
              {:else if detailState === 'failed'}
                <div class="load-failed small" role="alert">
                  <CircleAlert size={16} aria-hidden="true" />
                  <p>Couldn't read this ward.</p>
                  <button class="retry" onclick={() => void loadDetail(w.name)}><RefreshCw size={13} aria-hidden="true" /> Retry</button>
                </div>
              {:else if detail}
                {#if detail.findings.length > 0}
                  <h3>Findings</h3>
                  <ul class="findings">
                    {#each detail.findings as f (f.message + (f.path ?? ''))}
                      <li><span class="badge finding">{findingLabel(f)}</span>{#if f.node_id}<code>{f.node_id}</code>{/if}{#if f.path}<code>{f.path}</code>{/if} {f.message}</li>
                    {/each}
                  </ul>
                {:else}
                  <p class="muted lint-ok"><CircleCheck size={14} aria-hidden="true" /> Lays out clean against its conf.</p>
                {/if}
                {#if detail.layout}
                  <h3>Layout</h3>
                  <pre class="layout">{#each treeLines(detail.layout, 0) as line}{'  '.repeat(line.depth)}{line.text}\n{/each}</pre>
                {:else if hasInvalidConf(detail)}
                  <p class="muted">ward-conf.yaml failed to parse — see the findings above.</p>
                {:else}
                  <p class="muted">No ward-conf.yaml — a freeform ward.</p>
                {/if}
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
    {#if vaultRoot}<p class="muted small vault-path" title="Vault root">Vault: <code>{vaultRoot}</code> · override with <code>wards.vault_root</code></p>{/if}
  {/if}
</div>

<style>
  .wards-page { max-width: 56rem; margin: 0 auto; padding: 1.5rem 1rem; display: grid; gap: 1rem; }
  .page-head h1 { margin: 0; font-size: 1.4rem; }
  .empty { text-align: center; padding: 3rem 1rem; display: grid; gap: .5rem; justify-items: center; }
  .ward-list { list-style: none; margin: 0; padding: 0; display: grid; gap: .6rem; }
  .ward-card { border: 1px solid var(--ds-color-border); border-radius: 10px; overflow: hidden; }
  .ward-open { all: unset; box-sizing: border-box; width: 100%; display: flex; align-items: center; gap: .55rem; flex-wrap: wrap; padding: .8rem 1rem; cursor: pointer; }
  .ward-open:hover, .ward-open:focus-visible { background: var(--ds-color-field); }
  .badge { display: inline-flex; align-items: center; gap: .25rem; font-size: .72rem; padding: .12rem .5rem; border-radius: 999px; border: 1px solid var(--ds-color-border); }
  .badge.archetype { text-transform: lowercase; }
  .badge.lint-clean { color: var(--ds-color-success); }
  .badge.lint-dirty { color: var(--ds-color-danger); }
  .canonical { display: inline-flex; align-items: center; gap: .3rem; color: var(--ds-color-muted); font-size: .85rem; margin-left: auto; }
  .ward-detail { border-top: 1px solid var(--ds-color-border); padding: .8rem 1rem 1rem; display: grid; gap: .5rem; }
  .ward-detail h3 { margin: .2rem 0 0; font-size: .85rem; text-transform: uppercase; letter-spacing: .04em; color: var(--ds-color-muted); }
  .findings { list-style: none; margin: 0; padding: 0; display: grid; gap: .3rem; font-size: .85rem; }
  .findings li { display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; }
  .findings code { font-size: .78rem; background: var(--ds-color-field); padding: .05rem .35rem; border-radius: 5px; }
  .badge.finding { color: var(--ds-color-danger); }
  .lint-ok { display: flex; align-items: center; gap: .35rem; color: var(--ds-color-success); }
  .layout { margin: 0; padding: .6rem .75rem; background: var(--ds-color-field); border-radius: 8px; font-size: .8rem; overflow-x: auto; }
  .small { font-size: .78rem; }
  .vault-path { margin: 0; }
  code { font-size: .82rem; }
  .load-failed { display: flex; align-items: center; gap: .6rem; flex-wrap: wrap; padding: 1rem; border: 1px solid var(--ds-color-border); border-radius: 10px; }
  .load-failed p { margin: 0; }
  .load-failed.small { padding: .4rem .6rem; }
  .retry { display: inline-flex; align-items: center; gap: .3rem; font-size: .8rem; padding: .25rem .7rem; border-radius: 8px; border: 1px solid var(--ds-color-border); background: transparent; color: inherit; cursor: pointer; }
  .retry:hover { background: var(--ds-color-field); }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
</style>
