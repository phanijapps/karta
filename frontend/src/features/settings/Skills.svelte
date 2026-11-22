<script lang="ts">
  import { BookOpen, Trash2, Pencil, Plus, TriangleAlert, Search, Upload, X, Check, SkipForward, ShieldAlert } from '@lucide/svelte';
  import {
    listSkills,
    getSkillDetail,
    updateSkill,
    deleteSkill,
    searchMarketplace,
    previewSkillSource,
    installSkillCapability,
    previewSkillArchive,
    commitSkillArchive,
    type Skill,
    type SkillDetail,
    type RegistryLoadError,
    type RegistryFailure,
    type MarketSkill,
    type RepoPreview,
    type SkillPreviewPayload,
    type ArchivePreview,
    type SkillInstallOutcomeRow,
  } from '../../shared/api/client';

  // Live skills management + marketplace (Installed | Browse). Third-party
  // SKILL.md renders INERTLY (plain text, no HTML interpretation, no
  // remote resources) and is labeled untrusted; every install path passes
  // the mandatory preview dialog; the dialog inherits the MCP popup's
  // modality (focus/Tab/Escape) and async bindings.

  type LoadState =
    | { kind: 'loading' }
    | { kind: 'ready'; skills: Skill[]; loadErrors: RegistryLoadError[] }
    | { kind: 'error'; message: string };

  type BrowseState =
    | { kind: 'idle' }
    | { kind: 'searching' }
    | { kind: 'results'; results: MarketSkill[] }
    | { kind: 'failure'; message: string };

  type PreviewSource =
    | { kind: 'repo'; source: string; preview: RepoPreview; chosen: Set<string> }
    | { kind: 'archive'; preview: ArchivePreview };

  let load = $state<LoadState>({ kind: 'loading' });
  let view = $state<'installed' | 'browse'>('installed');
  let selectedIndex = $state(0);
  let detail = $state<SkillDetail | null>(null);
  let detailError = $state('');

  let showForm = $state(false);
  let editing = $state<Skill | null>(null);
  let formName = $state('');
  let formDescription = $state('');
  let formBody = $state('');
  let formError = $state<RegistryFailure | null>(null);
  let saving = $state(false);
  let confirmingDelete = $state<Skill | null>(null);
  let opError = $state<RegistryFailure | null>(null);

  // Browse state.
  let browse = $state<BrowseState>({ kind: 'idle' });
  let browsePage = $state(0);
  let searchQuery = $state('');
  let pasteSource = $state('');
  let installOutcomes = $state<SkillInstallOutcomeRow[] | null>(null);
  let installError = $state<{ code: string; message: string } | null>(null);
  let installing = $state(false);

  // Preview dialog state + request generation: every async flow binds to
  // the generation it started in; stale completions are ignored.
  let previewGen = $state(0);
  let previewFor = $state<PreviewSource | null>(null);
  let previewError = $state<{ code: string; message: string } | null>(null);
  let previewBusy = $state(false);
  let previewCard = $state<HTMLDivElement | null>(null);
  let previewOpener = $state<HTMLElement | null>(null);
  let fileInput = $state<HTMLInputElement | null>(null);

  $effect(() => {
    void reload();
  });

  async function reload() {
    load = { kind: 'loading' };
    detail = null;
    detailError = '';
    opError = null;
    try {
      const { skills, loadErrors } = await listSkills();
      load = { kind: 'ready', skills, loadErrors };
      if (selectedIndex >= skills.length) selectedIndex = 0;
      await loadDetail();
    } catch (err) {
      load = { kind: 'error', message: err instanceof Error ? err.message : String(err) };
    }
  }

  async function loadDetail() {
    if (load.kind !== 'ready') return;
    const current = load.skills[selectedIndex];
    if (!current) return;
    try {
      detail = await getSkillDetail(current.name);
      detailError = '';
    } catch (err) {
      detail = null;
      detailError = err instanceof Error ? err.message : String(err);
    }
  }

  const selected = $derived(load.kind === 'ready' ? load.skills[selectedIndex] : undefined);

  async function openEdit(s: Skill) {
    editing = s;
    formError = null;
    showForm = true;
    try {
      const d = await getSkillDetail(s.name);
      formName = d.name;
      formDescription = d.description;
      formBody = d.body;
    } catch (err) {
      showForm = false;
      detailError = err instanceof Error ? err.message : String(err);
    }
  }

  async function save() {
    formError = null;
    if (!formDescription.trim()) {
      formError = { code: 'invalid_manifest', message: 'A skill needs a description.' };
      return;
    }
    saving = true;
    const outcome = await updateSkill(editing!.name, {
      ...(editing!.name === formName.trim() ? {} : { name: formName.trim() }),
      description: formDescription,
      body: formBody,
    });
    saving = false;
    if (!outcome.ok) {
      formError = outcome;
      return;
    }
    showForm = false;
    await reload();
    if (load.kind === 'ready') {
      const idx = load.skills.findIndex((s) => s.name === formName.trim());
      if (idx >= 0) selectedIndex = idx;
    }
  }

  async function remove(s: Skill) {
    const outcome = await deleteSkill(s.name);
    confirmingDelete = null;
    await reload();
    opError = outcome.ok ? null : outcome;
  }

  function select(i: number) {
    selectedIndex = i;
    detail = null;
    detailError = '';
    void loadDetail();
  }

  // --- Browse: search, paste, import ------------------------------------

  function submitPaste(e: SubmitEvent) {
    e.preventDefault();
    if (pasteSource.trim()) void openRepoPreview(pasteSource.trim());
  }

  async function runSearch() {
    const q = searchQuery.trim();
    browsePage = 0;
    if (!q) {
      browse = { kind: 'idle' };
      return;
    }
    browse = { kind: 'searching' };
    const res = await searchMarketplace(q);
    if (!res.ok) {
      browse = { kind: 'failure', message: res.message };
      return;
    }
    browse = { kind: 'results', results: res.skills };
  }

  async function openRepoPreview(source: string, opener?: HTMLElement) {
    const gen = ++previewGen;
    previewError = null;
    previewBusy = true;
    previewOpener = opener ?? null;
    const res = await previewSkillSource(source);
    previewBusy = false;
    if (gen !== previewGen) return; // superseded by a newer flow
    if (!res.ok) {
      previewError = res;
      previewFor = { kind: 'repo', source, preview: null as unknown as RepoPreview, chosen: new Set() };
      return;
    }
    const chosen = new Set(res.value.skills.map((s) => s.name));
    previewFor = { kind: 'repo', source, preview: res.value, chosen };
  }

  async function onFileChosen(event: Event) {
    const inputEl = event.currentTarget as HTMLInputElement;
    const file = inputEl.files?.[0];
    inputEl.value = '';
    if (!file) return;
    const gen = ++previewGen;
    previewError = null;
    previewBusy = true;
    previewOpener = null;
    const res = await previewSkillArchive(file);
    previewBusy = false;
    if (gen !== previewGen) return; // superseded
    if (!res.ok) {
      previewError = res;
      previewFor = { kind: 'archive', preview: null as unknown as ArchivePreview };
      return;
    }
    previewFor = { kind: 'archive', preview: res.value };
  }

  function toggleChosen(name: string) {
    if (!previewFor || previewFor.kind !== 'repo') return;
    const chosen = new Set(previewFor.chosen);
    if (chosen.has(name)) chosen.delete(name);
    else chosen.add(name);
    previewFor = { ...previewFor, chosen };
  }

  async function runInstall() {
    if (!previewFor) return;
    const initiator = previewFor;
    installError = null;
    installing = true;
    if (initiator.kind === 'repo') {
      const res = await installSkillCapability(initiator.preview.capability, [...initiator.chosen]);
      await reload(); // always reconcile
      installing = false;
      if (previewFor !== initiator) return;
      if (!res.ok) {
        installError = res;
        return;
      }
      installOutcomes = res.value.results;
      closePreview();
    } else {
      const res = await commitSkillArchive(initiator.preview.stagingId);
      await reload();
      installing = false;
      if (previewFor !== initiator) return;
      if (!res.ok) {
        installError = res;
        return;
      }
      installOutcomes = [res.value];
      closePreview();
    }
  }

  // --- preview dialog modality (inherited from the MCP popup) ------------

  function closePreview() {
    previewGen++; // invalidate every in-flight flow
    previewFor = null;
    previewError = null;
    previewOpener?.focus();
    previewOpener = null;
  }

  function previewKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      closePreview();
      return;
    }
    if (e.key !== 'Tab' || !previewCard) return;
    const focusable = Array.from(previewCard.querySelectorAll<HTMLElement>('button, input, [tabindex]:not([tabindex="-1"])')).filter((el) => !el.hasAttribute('disabled'));
    if (focusable.length === 0) {
      e.preventDefault();
      previewCard.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === previewCard)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && (active === last || active === previewCard)) {
      e.preventDefault();
      first.focus();
    }
  }

  $effect(() => {
    if (previewFor && previewCard) previewCard.focus();
  });

  const originBadge: Record<string, string> = { marketplace: 'Marketplace', import: 'Imported', local: 'Local' };

  const PAGE_SIZE = 8;
  const pagedResults = $derived(browse.kind === 'results' ? browse.results.slice(browsePage * PAGE_SIZE, (browsePage + 1) * PAGE_SIZE) : []);
  const pageCount = $derived(browse.kind === 'results' ? Math.max(1, Math.ceil(browse.results.length / PAGE_SIZE)) : 1);
</script>

{#if load.kind === 'loading'}
  <p class="muted" role="status">Loading skills…</p>
{:else if load.kind === 'error'}
  <div class="panel error-panel" role="alert">
    <p><strong>Daemon unreachable.</strong> {load.message}</p>
    <button class="secondary" onclick={reload}>Retry</button>
  </div>
{:else if confirmingDelete}
  <div class="card danger-panel" role="alert">
    <h3><TriangleAlert size={18} /> Delete {confirmingDelete.name}?</h3>
    <p class="muted">The whole skill directory <code>{confirmingDelete.directory}</code> — including any bundled files — will be removed.</p>
    <div class="form-actions">
      <button class="secondary" onclick={() => (confirmingDelete = null)}>Cancel</button>
      <button class="danger" onclick={() => { if (confirmingDelete) remove(confirmingDelete); }}>Delete skill and bundle</button>
    </div>
  </div>
{:else}
  <div class="section-head">
    <div>
      <h3>Workspace skills</h3>
      <p class="muted">Managed directories in <code>~/.zbot/config/skills/</code> — changes apply immediately, no restart.</p>
    </div>
    <div class="view-switch" role="tablist" aria-label="Skills views">
      <button role="tab" aria-selected={view === 'installed'} class:active={view === 'installed'} onclick={() => (view = 'installed')}>Installed</button>
      <button role="tab" aria-selected={view === 'browse'} class:active={view === 'browse'} onclick={() => (view = 'browse')}>Browse</button>
    </div>
  </div>

  {#if view === 'browse'}
    <div class="browse">
      <form class="search-row" onsubmit={(e) => { e.preventDefault(); void runSearch(); }}>
        <label class="grow">Search the marketplace<input bind:value={searchQuery} placeholder="pdf, code review, tailwind…" /></label>
        <button type="submit" class="primary" disabled={browse.kind === 'searching'}><Search size={15} /> {browse.kind === 'searching' ? 'Searching…' : 'Search'}</button>
      </form>
      {#if browse.kind === 'failure'}
        <div class="panel error-panel" role="alert">
          <p><strong>Index unreachable.</strong> {browse.message}</p>
          <p class="muted">Paste any GitHub owner/repo below to install directly while the index is down.</p>
        </div>
      {:else if browse.kind === 'results' && browse.results.length === 0}
        <p class="muted empty-note">No matches. Try fewer words, or paste a GitHub owner/repo below.</p>
      {:else if browse.kind === 'results'}
        <ul class="item-list market">
          {#each pagedResults as skill}
            <li>
              <button onclick={(e) => void openRepoPreview(skill.source, e.currentTarget)}>
                <BookOpen size={18} />
                <span class="item-title"><strong>{skill.name}</strong><small>{skill.source}</small></span>
                {#if skill.installs}<span class="pill">{skill.installs >= 1000 ? `${Math.round(skill.installs / 1000)}K` : skill.installs} installs</span>{/if}
              </button>
            </li>
          {/each}
        </ul>
        {#if pageCount > 1}
          <div class="pager" role="navigation" aria-label="Search results pages">
            <button class="secondary" onclick={() => (browsePage = Math.max(0, browsePage - 1))} disabled={browsePage === 0}>Previous</button>
            <span class="muted">Page {browsePage + 1} of {pageCount}</span>
            <button class="secondary" onclick={() => (browsePage = Math.min(pageCount - 1, browsePage + 1))} disabled={browsePage >= pageCount - 1}>Next</button>
          </div>
        {/if}
      {/if}
      <form class="search-row" onsubmit={submitPaste}>
        <label class="grow">Install from GitHub<input bind:value={pasteSource} placeholder="anthropics/skills or https://github.com/owner/repo" spellcheck="false" /></label>
        <button type="submit" class="secondary" disabled={previewBusy}><Search size={15} /> Preview</button>
      </form>
      <div class="import-row">
        <button class="secondary" onclick={() => fileInput?.click()} disabled={previewBusy}><Upload size={15} /> Import .skill / .zip</button>
        <span class="muted hint">Your own packaged skills — previewed and staged by the daemon.</span>
      </div>
      {#if installError}
        <div class="panel error-panel" role="alert"><p><strong>Install failed.</strong> {installError.message}</p></div>
      {:else if installOutcomes}
        <div class="import-results" role="status">
          {#each installOutcomes as r}
            {@const Icon = r.status === 'created' ? Check : r.status === 'skipped' ? SkipForward : X}
            <div class="import-row outcome" data-status={r.status}>
              <Icon size={15} />
              <strong>{r.name}</strong>
              <span class="import-status">{r.status}{r.reason ? ` · ${r.reason}` : ''}</span>
              {#if r.message}<span class="muted import-msg">{r.message}</span>{/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {:else}
    {#if opError}
      <div class="panel error-panel" role="alert"><p><strong>Operation failed.</strong> {opError.message}</p></div>
    {/if}
    {#if load.loadErrors.length}
      <div class="panel warn-panel">
        <p><strong>{load.loadErrors.length} file(s) failed to load</strong> (they are not listed):</p>
        <ul>{#each load.loadErrors as e}<li><code>{e.path}</code> — {e.message}</li>{/each}</ul>
      </div>
    {/if}
    <div class="section-actions">
      <button class="secondary" onclick={() => fileInput?.click()} disabled={previewBusy}><Upload size={15} /> Import skill</button>
    </div>
    {#if load.skills.length === 0}
      <div class="card empty">
        <BookOpen size={22} />
        <p class="muted">No skills installed. Import a .skill file here, or browse the marketplace.</p>
      </div>
    {:else}
      <ul class="item-list">
        {#each load.skills as skill, i}
          <li>
            <button class:selected={selectedIndex === i} aria-pressed={selectedIndex === i} onclick={() => select(i)}>
              <BookOpen size={18} />
              <span class="item-title"><strong>{skill.name}</strong><small>{skill.description}</small></span>
              <span class="pill" class:market={skill.origin === 'marketplace'}>{originBadge[skill.origin ?? 'local'] ?? 'Local'}</span>
            </button>
          </li>
        {/each}
      </ul>
      {#if selected}
        <div class="card detail">
          <div class="detail-head">
            <h3>{selected.name}</h3>
            <code class="path">{selected.directory}</code>
          </div>
          <p>{selected.description}</p>
          {#if detailError}
            <p class="form-error" role="alert">{detailError}</p>
          {:else if detail}
            <label>SKILL.md body<textarea class="mono" readonly rows="12">{detail.body}</textarea></label>
          {/if}
          <div class="actions">
            <button class="secondary" onclick={() => openEdit(selected)}><Pencil size={15} /> Edit</button>
            <button class="danger" onclick={() => (confirmingDelete = selected)}><Trash2 size={15} /> Delete</button>
          </div>
        </div>
      {/if}
    {/if}
  {/if}
{/if}

<input type="file" accept=".skill,.zip" bind:this={fileInput} onchange={(e) => void onFileChosen(e)} class="sr-only" aria-label="Import a .skill or .zip archive" />

{#if showForm}
  <div class="form-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) showForm = false; }}>
    <form class="dialog-card" aria-label={`Edit ${editing?.name ?? ''}`} onsubmit={(e) => { e.preventDefault(); void save(); }}>
      <h3>Edit {editing?.name}</h3>
      <div class="dialog-body">
        <label>Name<input bind:value={formName} placeholder="Code review" /></label>
        <label class="full">Description<input bind:value={formDescription} placeholder="Review a change for correctness and maintainability." /></label>
        <label class="full">Body (markdown)<textarea class="mono" bind:value={formBody} rows="10" placeholder={'# Code review\n\nChecklist…'}></textarea></label>
        <p class="muted hint">Saving writes <code>SKILL.md</code> (frontmatter + body) under <code>~/.zbot/config/skills/&lt;slug&gt;/</code>. Renaming moves the directory; bundled files travel with it.</p>
        {#if formError}
          <p class="form-error" role="alert">{formError.message}</p>
        {/if}
        <div class="form-actions">
          <button type="button" class="secondary" onclick={() => (showForm = false)} disabled={saving}>Cancel</button>
          <button type="submit" class="primary" disabled={saving}>{saving ? 'Saving…' : 'Save changes'}</button>
        </div>
      </div>
    </form>
  </div>
{/if}

{#if previewFor}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="form-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) closePreview(); }} onkeydown={previewKeydown}>
    <div class="dialog-card wide" bind:this={previewCard} tabindex="-1" role="dialog" aria-modal="true" aria-label={previewFor.kind === 'repo' ? `Preview ${previewFor.source}` : 'Preview imported skill'}>
      {#if previewFor.kind === 'repo' && previewFor.preview}
        <div class="dialog-head">
          <div class="detail-head">
            <h3>{previewFor.source}</h3>
            <p class="muted hint">Pinned commit {previewFor.preview.commit.slice(0, 10)} · <ShieldAlert size={13} /> third-party content — untrusted</p>
          </div>
          <button class="icon-button" onclick={closePreview} aria-label="Close dialog"><X size={18} /></button>
        </div>
        <ul class="skill-cards">
          {#each previewFor.preview.skills as skill}
            <li class="skill-card">
              <label class="radio">
                <input type="checkbox" checked={previewFor.chosen.has(skill.name)} onchange={() => toggleChosen(skill.name)} />
                <span class="item-title"><strong>{skill.name}</strong><small>{skill.description}</small></span>
              </label>
              <details>
                <summary>SKILL.md ({skill.files.length} files{skill.license ? ` · ${skill.license}` : ''})</summary>
                <pre class="mono inert">{skill.body}</pre>
                <ul class="file-list">{#each skill.files as f}<li><code>{f.path}</code> <span class="muted">{f.size} B{f.executable ? ' · executable' : ''}</span></li>{/each}</ul>
              </details>
            </li>
          {/each}
        </ul>
        <div class="form-actions">
          <button type="button" class="secondary" onclick={closePreview} disabled={installing}>Cancel</button>
          <button type="button" class="primary" onclick={() => void runInstall()} disabled={installing || previewFor.chosen.size === 0}>
            {installing ? 'Installing…' : `Install ${previewFor.chosen.size} skill${previewFor.chosen.size === 1 ? '' : 's'}`}
          </button>
        </div>
      {:else if previewFor.kind === 'archive' && previewFor.preview}
        <div class="dialog-head">
          <div class="detail-head">
            <h3>{previewFor.preview.skill.name}</h3>
            <p class="muted hint"><ShieldAlert size={13} /> Imported file — untrusted; staged by the daemon</p>
          </div>
          <button class="icon-button" onclick={closePreview} aria-label="Close dialog"><X size={18} /></button>
        </div>
        <p>{previewFor.preview.skill.description}</p>
        <details open>
          <summary>SKILL.md ({previewFor.preview.skill.files.length} files)</summary>
          <pre class="mono inert">{previewFor.preview.skill.body}</pre>
          <ul class="file-list">{#each previewFor.preview.skill.files as f}<li><code>{f.path}</code> <span class="muted">{f.size} B{f.executable ? ' · executable' : ''}</span></li>{/each}</ul>
        </details>
        <div class="form-actions">
          <button type="button" class="secondary" onclick={closePreview} disabled={installing}>Cancel</button>
          <button type="button" class="primary" onclick={() => void runInstall()} disabled={installing}>{installing ? 'Installing…' : 'Install skill'}</button>
        </div>
      {:else}
        <div class="dialog-head">
          <h3>Preview failed</h3>
          <button class="icon-button" onclick={closePreview} aria-label="Close dialog"><X size={18} /></button>
        </div>
        <p class="form-error" role="alert">{previewError?.message ?? 'The preview could not be loaded.'}</p>
        <div class="form-actions"><button type="button" class="secondary" onclick={closePreview}>Close</button></div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .section-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--ds-space-4); margin-bottom: var(--ds-space-6); }
  .section-head p { margin-top: var(--ds-space-1); font-size: 0.8125rem; }
  .section-actions { display: flex; gap: var(--ds-space-2); margin-bottom: var(--ds-space-4); }
  .view-switch { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ds-space-1); background: var(--ds-color-field); border-radius: var(--ds-radius-control); padding: var(--ds-space-1); }
  .view-switch button { padding: var(--ds-space-2) var(--ds-space-3); border-radius: calc(var(--ds-radius-control) - 2px); font-size: var(--ds-font-nav); font-weight: 500; color: var(--ds-color-muted); }
  .view-switch button.active { background: var(--ds-color-surface); color: var(--ds-color-text); box-shadow: 0 1px 2px rgb(0 0 0 / 0.08); }
  .card { border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-dialog); background: var(--ds-color-surface); padding: var(--ds-space-6); }
  .card.detail { display: grid; gap: var(--ds-space-4); margin-top: var(--ds-space-2); }
  .detail-head { display: grid; gap: var(--ds-space-1); }
  .path { font-size: 0.75rem; color: var(--ds-color-muted); overflow-wrap: anywhere; }
  .card label, .dialog-body label { font-size: 0.8125rem; color: var(--ds-color-muted); font-weight: 500; gap: var(--ds-space-2); }
  .actions { display: flex; align-items: center; gap: var(--ds-space-2); flex-wrap: wrap; }
  .form-actions { display: flex; justify-content: flex-end; gap: var(--ds-space-2); }
  .mono { font-family: ui-monospace, 'SF Mono', 'Cascadia Code', Menlo, monospace; font-size: 0.8125rem; }
  .panel { display: grid; gap: var(--ds-space-3); padding: var(--ds-space-4); border-radius: var(--ds-radius-control); font-size: 0.8125rem; margin-bottom: var(--ds-space-4); }
  .error-panel { border: 1px solid var(--ds-color-border); background: var(--ds-color-danger-soft); }
  .warn-panel { border: 1px solid var(--ds-color-border); }
  .warn-panel ul { margin: 0; padding-left: var(--ds-space-4); display: grid; gap: var(--ds-space-1); overflow-wrap: anywhere; }
  .danger-panel { border-color: transparent; background: var(--ds-color-danger-soft); }
  .danger-panel .form-actions { justify-content: flex-end; }
  .empty { display: grid; justify-items: center; gap: var(--ds-space-3); padding: var(--ds-space-8); text-align: center; color: var(--ds-color-muted); }
  .empty-note { padding: var(--ds-space-4); }
  .item-list { display: grid; gap: var(--ds-space-2); list-style: none; margin: 0 0 var(--ds-space-4); padding: 0; }
  .item-list li { border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); transition: background-color 120ms ease, border-color 120ms ease; }
  .item-list li:hover { background: var(--ds-color-hover); }
  .item-list button { width: 100%; display: flex; align-items: center; text-align: left; gap: var(--ds-space-3); padding: var(--ds-space-3) var(--ds-space-4); color: var(--ds-color-muted); }
  .item-title { flex: 1; min-width: 0; display: grid; gap: 2px; }
  .item-list strong { font-weight: 550; overflow-wrap: anywhere; color: var(--ds-color-text); }
  small { display: block; font-size: 0.75rem; color: var(--ds-color-muted); }
  .pill { font-size: 0.6875rem; font-weight: 500; color: var(--ds-color-muted); background: var(--ds-color-hover); border-radius: var(--ds-radius-pill); padding: 3px var(--ds-space-2); white-space: nowrap; }
  .pill.market { color: var(--ds-color-success); }
  .hint { font-size: 0.75rem; line-height: 1.6; }
  .pager { display: flex; align-items: center; justify-content: center; gap: var(--ds-space-3); font-size: 0.8125rem; }
  /* Browse */
  .browse { display: grid; gap: var(--ds-space-4); }
  .search-row { display: flex; gap: var(--ds-space-2); align-items: end; }
  .search-row label { flex: 1; font-size: 0.8125rem; color: var(--ds-color-muted); font-weight: 500; }
  .import-row { display: flex; align-items: center; gap: var(--ds-space-3); flex-wrap: wrap; }
  .import-row.outcome { font-size: 0.8125rem; }
  .import-row.outcome :global(svg) { align-self: center; }
  .import-row[data-status='created'] :global(svg) { color: var(--ds-color-success); }
  .import-row[data-status='failed'] :global(svg) { color: var(--ds-color-danger); }
  .import-row[data-status='skipped'] :global(svg) { color: var(--ds-color-muted); }
  .import-status { font-weight: 500; font-size: 0.75rem; color: var(--ds-color-muted); }
  .import-msg { width: 100%; font-size: 0.75rem; }
  .import-results { display: grid; gap: var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-3); }
  /* Dialogs */
  .form-backdrop { position: fixed; inset: 0; background: rgb(0 0 0 / 0.45); display: grid; place-items: center; z-index: 5; padding: var(--ds-space-4); }
  .dialog-card { background: var(--ds-color-background); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-dialog); padding: var(--ds-space-6); width: min(34rem, 100%); max-height: 90vh; overflow-y: auto; display: grid; gap: var(--ds-space-4); }
  .dialog-card.wide { width: min(40rem, 100%); }
  .dialog-card:focus-visible { outline: 2px solid var(--ds-color-focus); outline-offset: 3px; }
  .dialog-card h3, .card h3 { font-size: 1.0625rem; font-weight: 600; display: flex; align-items: center; gap: var(--ds-space-2); }
  .dialog-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--ds-space-4); }
  .dialog-body { display: grid; gap: var(--ds-space-4); }
  .form-error { color: var(--ds-color-danger); font-size: 0.8125rem; }
  /* Preview cards: INERT third-party markdown (plain text, no HTML). */
  .skill-cards { list-style: none; margin: 0; padding: 0; display: grid; gap: var(--ds-space-3); }
  .skill-card { border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-3); display: grid; gap: var(--ds-space-2); }
  .skill-card .radio { display: flex; align-items: flex-start; gap: var(--ds-space-2); }
  .skill-card .radio input { width: 16px; height: 16px; min-height: 16px; margin-top: 3px; }
  pre.inert { margin: 0; padding: var(--ds-space-3); background: var(--ds-color-field); border-radius: var(--ds-radius-control); max-height: 16rem; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 0.75rem; }
  .file-list { list-style: none; margin: var(--ds-space-2) 0 0; padding: 0; display: grid; gap: 2px; font-size: 0.75rem; }
  summary { cursor: pointer; font-size: 0.8125rem; color: var(--ds-color-muted); }
  @media (max-width: 767px) { .section-head { flex-direction: column; } .search-row { flex-direction: column; align-items: stretch; } }
</style>
