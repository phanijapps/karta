<script lang="ts">
  import { Plug, Trash2, Pencil, FlaskConical, Plus, TriangleAlert, Check, X, SkipForward, AlertTriangle } from '@lucide/svelte';
  import {
    listMcps,
    createMcp,
    updateMcp,
    deleteMcp,
    testMcp,
    importMcps,
    type McpServer,
    type McpTransport,
    type McpTestResult,
    type RegistryLoadError,
    type RegistryFailure,
    type McpUpdateInput,
    type McpImportOutcome,
  } from '../../shared/api/client';

  // Live MCP management view (Open WebUI-flavored). Add and edit live in
  // a dialog with two modes — Form (segmented transport selector: the
  // fields follow the selection) and Paste JSON (standard mcpServers
  // blocks; the daemon is the only parser). Every mutation acts on
  // daemon-managed manifests through the daemon; the connection test
  // runs server-side per declared transport.

  type LoadState =
    | { kind: 'loading' }
    | { kind: 'ready'; servers: McpServer[]; loadErrors: RegistryLoadError[] }
    | { kind: 'error'; message: string };

  type Transport = 'local' | 'remote' | 'both';

  let load = $state<LoadState>({ kind: 'loading' });
  let selectedIndex = $state(0);
  let testResult = $state<McpTestResult | null>(null);
  let testing = $state(false);

  // Dialog state (add + edit share it; each mode keeps its own draft
  // while the dialog is open).
  let dialog = $state<{ mode: 'form' | 'json'; editing: McpServer | null } | null>(null);
  let formName = $state('');
  let formTransport = $state<Transport>('local');
  let formCommand = $state('');
  let formArgs = $state('');
  let formEnv = $state('');
  let formUrl = $state('');
  let formError = $state<RegistryFailure | null>(null);
  let saving = $state(false);
  let jsonText = $state('');
  let jsonName = $state('');
  let jsonError = $state<{ code: string; message: string } | null>(null);
  let importResults = $state<McpImportOutcome[] | null>(null);
  let importing = $state(false);
  let detailFor = $state<McpServer | null>(null);
  let detailCard = $state<HTMLDivElement | null>(null);
  let detailOpener = $state<HTMLElement | null>(null);
  let confirmingDelete = $state<McpServer | null>(null);
  let opError = $state<RegistryFailure | null>(null);
  let probeError = $state<RegistryFailure | null>(null);

  const managedNamePattern = /^[a-z0-9][a-z0-9-]{0,63}$/;
  const reservedNames = new Set(['con', 'prn', 'aux', 'nul', ...Array.from({ length: 9 }, (_, i) => `com${i + 1}`), ...Array.from({ length: 9 }, (_, i) => `lpt${i + 1}`)]);
  const validManagedName = (name: string) => managedNamePattern.test(name) && !reservedNames.has(name);

  const stdioOf = (s: McpServer): McpTransport | undefined => s.transports.find((t) => t.kind === 'stdio');
  const httpOf = (s: McpServer): McpTransport | undefined => s.transports.find((t) => t.kind === 'http');

  // Advisory single-object detection (affordance only — the daemon is
  // the parser and the authority).
  const pasteLooksSingle = $derived.by(() => {
    if (jsonText.trim() === '') return false;
    try {
      const parsed = JSON.parse(jsonText) as Record<string, unknown>;
      return typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed) && ('command' in parsed || 'url' in parsed);
    } catch {
      return false;
    }
  });

  $effect(() => {
    void reload();
  });

  $effect(() => {
    if (detailFor && detailCard) detailCard.focus();
  });

  async function reload() {
    load = { kind: 'loading' };
    testResult = null;
    opError = null;
    probeError = null;
    try {
      const { servers, loadErrors } = await listMcps();
      load = { kind: 'ready', servers, loadErrors };
      if (selectedIndex >= servers.length) selectedIndex = 0;
    } catch (err) {
      load = { kind: 'error', message: err instanceof Error ? err.message : String(err) };
    }
  }

  const selected = $derived(load.kind === 'ready' ? load.servers[selectedIndex] : undefined);

  function openCreate() {
    editingReset(null);
    dialog = { mode: 'form', editing: null };
  }

  function editingReset(server: McpServer | null) {
    formError = null;
    jsonError = null;
    importResults = null;
    if (server === null) {
      formName = '';
      formTransport = 'local';
      formCommand = '';
      formArgs = '';
      formEnv = '';
      formUrl = '';
      jsonText = '';
      jsonName = '';
      return;
    }
    formName = server.name;
    const stdio = stdioOf(server);
    const http = httpOf(server);
    formTransport = stdio && http ? 'both' : http && !stdio ? 'remote' : 'local';
    formCommand = stdio?.command ?? '';
    // One argument per line and KEY=value lines keep every value
    // byte-exact (spaces, quotes, empty arguments) across edits.
    formArgs = stdio?.args?.join('\n') ?? '';
    formEnv = stdio?.env ? Object.entries(stdio.env).map(([k, v]) => `${k}=${v}`).join('\n') : '';
    formUrl = http?.url ?? '';
    // Paste JSON prefills the server as a standard block (edit-as-JSON).
    const block: Record<string, unknown> = {};
    if (stdio) {
      block.command = stdio.command;
      if (stdio.args?.length) block.args = stdio.args;
      if (stdio.env && Object.keys(stdio.env).length) block.env = stdio.env;
    }
    if (http) block.url = http.url;
    jsonText = JSON.stringify({ mcpServers: { [server.name]: block } }, null, 2);
    jsonName = '';
  }

  function openEdit(s: McpServer) {
    editingReset(s);
    dialog = { mode: 'form', editing: s };
  }

  function closeDialog() {
    dialog = null;
  }

  // Popup-local modality: focus moves in (and restores to the opener on
  // close), Tab cycles inside the card, and Escape closes ONLY this
  // popup (the event is stopped before the Settings dialog's own cancel
  // handling).
  function closeDetail() {
    detailFor = null;
    detailOpener?.focus();
    detailOpener = null;
  }

  function detailKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      // preventDefault keeps the enclosing native Settings dialog from
      // also cancelling; stopPropagation keeps other handlers quiet.
      e.preventDefault();
      e.stopPropagation();
      closeDetail();
      return;
    }
    if (e.key !== 'Tab' || !detailCard) return;
    const focusable = Array.from(detailCard.querySelectorAll<HTMLElement>('button, input, [tabindex]:not([tabindex="-1"])')).filter((el) => !el.hasAttribute('disabled'));
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }

  function parseEnv(raw: string): { env: Record<string, string>; error?: string } {
    const env: Record<string, string> = {};
    for (const line of raw.split('\n')) {
      if (!line.trim()) continue;
      const eq = line.indexOf('=');
      if (eq <= 0) return { env, error: `Env entries are KEY=value lines; this line has no =: ${line}` };
      // The value is the rest of the line verbatim (no trimming), so
      // trailing and inner spaces round-trip.
      env[line.slice(0, eq)] = line.slice(eq + 1);
    }
    return { env };
  }

  async function saveForm() {
    if (!dialog) return;
    formError = null;
    const editing = dialog.editing;
    const targetName = formName.trim();
    const renaming = !!editing && targetName !== editing.name;
    if (!editing || renaming) {
      if (!validManagedName(targetName)) {
        formError = { code: 'invalid_name', message: 'Names are lowercase kebab-case (1-64 chars, not a reserved device name).' };
        return;
      }
    }
    const envParsed = parseEnv(formEnv);
    if (envParsed.error) {
      formError = { code: 'invalid_manifest', message: envParsed.error };
      return;
    }
    const wantStdio = formTransport !== 'remote';
    const wantHttp = formTransport !== 'local';
    if (!wantStdio && !formUrl.trim()) {
      formError = { code: 'invalid_manifest', message: 'A remote server needs its URL.' };
      return;
    }
    if (wantStdio && !formCommand.trim()) {
      formError = { code: 'invalid_manifest', message: 'A local process needs its command.' };
      return;
    }
    const args = formArgs === '' ? [] : formArgs.split('\n');
    const transports = {
      ...(wantStdio ? { stdio: { command: formCommand.trim(), ...(args.length ? { args } : {}), ...(Object.keys(envParsed.env).length ? { env: envParsed.env } : {}) } } : {}),
      ...(wantHttp && formUrl.trim() ? { http: { url: formUrl.trim() } } : {}),
    };
    saving = true;
    let outcome;
    if (editing) {
      // Full replace: carry the CURRENT enablement so an ordinary edit
      // or rename never silently re-enables a disabled server.
      const patch: McpUpdateInput = { ...transports, enabled: editing.enabled };
      if (renaming) patch.name = targetName;
      outcome = await updateMcp(editing.name, patch);
    } else {
      outcome = await createMcp({ name: targetName, ...transports });
    }
    saving = false;
    if (!outcome.ok) {
      formError = outcome;
      return;
    }
    closeDialog();
    await reload();
    if (load.kind === 'ready') {
      const idx = load.servers.findIndex((s) => s.name === targetName);
      if (idx >= 0) selectedIndex = idx;
    }
  }

  async function runImport() {
    if (!dialog) return;
    const initiator = dialog; // identity: guards against close/reopen mid-flight
    jsonError = null;
    importResults = null;
    importing = true;
    const outcome = await importMcps({
      document: jsonText,
      ...(initiator.editing === null && pasteLooksSingle && jsonName.trim() ? { name: jsonName.trim() } : {}),
      ...(initiator.editing !== null ? { replaceName: initiator.editing.name } : {}),
    });
    importing = false;
    await reload(); // ALWAYS reconcile the list behind the dialog
    if (dialog !== initiator) return; // closed or replaced mid-flight
    if (!outcome.ok) {
      jsonError = outcome;
      return;
    }
    importResults = outcome.result.results;
    // Edit-as-JSON: a CLEAN replace (no warnings) closes the dialog;
    // warnings keep the report visible — tolerance is never silent.
    const first = outcome.result.results[0];
    if (initiator.editing !== null && outcome.result.results.length === 1 && first?.status === 'created' && !first?.warnings?.length) {
      closeDialog();
    }
  }

  // Enable/disable: a full-replace PATCH carrying the current
  // transports plus the flipped state (absent field would re-enable).
  async function toggle(s: McpServer) {
    const stdio = stdioOf(s);
    const http = httpOf(s);
    const patch: McpUpdateInput = {
      ...(stdio?.command ? { stdio: { command: stdio.command, ...(stdio.args?.length ? { args: stdio.args } : {}), ...(stdio.env && Object.keys(stdio.env).length ? { env: stdio.env } : {}) } } : {}),
      ...(http?.url ? { http: { url: http.url } } : {}),
      enabled: !s.enabled,
    };
    const outcome = await updateMcp(s.name, patch);
    await reload(); // reconcile first
    opError = outcome.ok ? null : outcome; // then retain the outcome
  }

  async function remove(s: McpServer) {
    const outcome = await deleteMcp(s.name);
    confirmingDelete = null;
    await reload(); // reconcile: the list shows what the daemon now has
    opError = outcome.ok ? null : outcome; // after reload (which resets it)
  }

  async function runTest(s: McpServer) {
    testing = true;
    testResult = null;
    probeError = null;
    const outcome = await testMcp(s.name);
    if (!outcome.ok) {
      probeError = outcome; // request-level failure (404, daemon down)
    } else if (selected?.name === s.name) {
      testResult = outcome.result; // per-transport outcomes are data
    }
    testing = false;
  }

  const probeGuidance: Record<string, string> = {
    spawn_failed: 'The command could not be started. Check the binary name and that it is on the daemon’s PATH (absolute paths are safest).',
    connect_failed: 'The HTTP url refused the connection. Check the server is running and the url points at its MCP endpoint.',
    timeout: 'The server accepted the connection but never completed the MCP handshake within the deadline.',
    protocol_error: 'The server answered, but not as a valid MCP peer. Verify it speaks the MCP protocol (stdio on stdout, or streamable HTTP).',
    daemon: 'The daemon connection was lost. Is zbotd running? Retry once it is back.',
    not_found: 'This server is no longer registered. Reload the list.',
  };

  const outcomeIcon: Record<string, typeof Check> = { created: Check, skipped: SkipForward, failed: X };
</script>

{#if load.kind === 'loading'}
  <p class="muted" role="status">Loading tool servers…</p>
{:else if load.kind === 'error'}
  <div class="panel error-panel" role="alert">
    <p><strong>Daemon unreachable.</strong> {load.message}</p>
    <button class="secondary" onclick={reload}>Retry</button>
  </div>
{:else if confirmingDelete}
  <div class="card danger-panel" role="alert">
    <h3><TriangleAlert size={18} /> Delete {confirmingDelete.name}?</h3>
    <p class="muted">Its manifest <code>{confirmingDelete.file}</code> will be removed and the server unregistered.</p>
    <div class="form-actions">
      <button class="secondary" onclick={() => (confirmingDelete = null)}>Cancel</button>
      <button class="danger" onclick={() => { if (confirmingDelete) remove(confirmingDelete); }}>Delete</button>
    </div>
  </div>
{:else}
  <div class="section-head">
    <div>
      <h3>Tool servers</h3>
      <p class="muted">Managed manifests in <code>~/.zbot/config/mcps/</code> — changes apply immediately, no restart.</p>
    </div>
    <button class="secondary" onclick={openCreate}><Plus size={15} /> Add server</button>
  </div>

  {#if opError && !detailFor}
    <div class="panel error-panel" role="alert">
      <p><strong>Operation failed.</strong> {opError.message} The list reflects the daemon's current state — retry, or edit the server by hand.</p>
    </div>
  {/if}
  {#if load.loadErrors.length}
    <div class="panel warn-panel">
      <p><strong>{load.loadErrors.length} file(s) failed to load</strong> (they are not listed):</p>
      <ul>{#each load.loadErrors as e}<li><code>{e.path}</code> — {e.message}</li>{/each}</ul>
    </div>
  {/if}

  {#if load.servers.length === 0}
    <div class="card empty">
      <Plug size={22} />
      <p class="muted">No MCP servers registered. Add one to give your agents tools.</p>
    </div>
  {:else}
    <ul class="item-list">
      {#each load.servers as server, i}
        <li class:disabled={!server.enabled}>
          <button class:selected={selectedIndex === i} aria-pressed={selectedIndex === i} onclick={(e) => { selectedIndex = i; testResult = null; detailOpener = e.currentTarget; detailFor = server; }}>
            <Plug size={18} />
            <span class="item-title"><strong>{server.name}</strong><small>{stdioOf(server) ? 'stdio' : ''}{stdioOf(server) && httpOf(server) ? ' · ' : ''}{httpOf(server) ? 'http' : ''}</small></span>
            <span class="pills">
              {#if !server.enabled}<span class="pill off">Disabled</span>{/if}
              {#if stdioOf(server)}<span class="pill">stdio</span>{/if}
              {#if httpOf(server)}<span class="pill">http</span>{/if}
            </span>
          </button>
          <label class="switch row-switch" title={server.enabled ? 'Disable (keeps the manifest; agents will skip it)' : 'Enable this server'}>
            <input type="checkbox" checked={server.enabled} onchange={() => void toggle(server)} />
            <span class="track" aria-hidden="true"></span>
            <span class="sr-only">{server.enabled ? 'Disable' : 'Enable'} {server.name}</span>
          </label>
        </li>
      {/each}
    </ul>

  {/if}
{/if}

{#if detailFor}
  {@const current = load.kind === 'ready' ? (load.servers.find((x) => x.name === detailFor!.name) ?? detailFor) : detailFor}
  <!-- eslint-disable-next-line svelte/no-static-element-interactions -- key handling implements the declared modality -->
  <div class="form-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) closeDetail(); }} onkeydown={detailKeydown}>
    <div class="dialog-card" bind:this={detailCard} tabindex="-1" role="dialog" aria-modal="true" aria-label={`MCP server ${current.name}`}>
      <div class="dialog-head">
        <div class="detail-head">
          <h3>{current.name}</h3>
          <code class="path">{current.file}</code>
        </div>
        <label class="switch" title={current.enabled ? 'Disable (keeps the manifest; agents will skip it)' : 'Enable this server'}>
          <input type="checkbox" checked={current.enabled} onchange={() => void toggle(current)} />
          <span class="track" aria-hidden="true"></span>
          <span class="sr-only">{current.enabled ? 'Disable' : 'Enable'} {current.name}</span>
        </label>
        <button class="icon-button" onclick={() => closeDetail()} aria-label="Close dialog"><X size={18} /></button>
      </div>
      <div class="fields">
        {#each current.transports as t}
          {#if t.kind === 'stdio'}
            <label class="full">Command<input readonly value={[t.command, ...(t.args ?? [])].join(' ')} spellcheck="false" /></label>
            {#if t.env && Object.keys(t.env).length}<label class="full">Environment<input readonly value={Object.keys(t.env).join(', ')} /></label>{/if}
          {:else}
            <label class="full">HTTP URL<input readonly value={t.url ?? ''} spellcheck="false" /></label>
          {/if}
        {/each}
      </div>
      {#if opError}
        <p class="form-error" role="alert"><strong>Operation failed:</strong> {opError.message} The list reflects the daemon's current state — retry, or edit the server by hand.</p>
      {/if}
      <div class="actions">
        <button class="primary" onclick={() => void runTest(current)} disabled={testing}><FlaskConical size={15} /> {testing ? 'Testing…' : 'Test connection'}</button>
        <button class="secondary" onclick={() => { openEdit(current); detailFor = null; }}><Pencil size={15} /> Edit</button>
        <button class="danger" onclick={() => { confirmingDelete = current; detailFor = null; }}><Trash2 size={15} /> Delete</button>
      </div>
      {#if probeError}
        <p class="form-error" role="alert"><strong>Test could not run:</strong> {probeError.message}</p>
      {/if}
      {#if testResult}
        <div class="probe">
          {#each testResult.results as r}
            <div class="probe-row" data-status={r.status}>
              <span class="status-dot" aria-hidden="true"></span>
              <strong>{r.transport}</strong>
              {#if r.status === 'ok'}
                <span class="muted">{r.tools?.length ?? 0} tool{(r.tools?.length ?? 0) === 1 ? '' : 's'}{r.truncated ? ` (list truncated at ${r.tools?.length ?? 0})` : ''}</span>
              {:else if r.failure}
                <span class="fail-reason">{r.failure.reason}</span>
                <p class="muted">{probeGuidance[r.failure.reason] ?? r.failure.message}</p>
              {/if}
            </div>
            {#if r.status === 'ok' && r.tools?.length}
              <ul class="tool-list">{#each r.tools.slice(0, 20) as tool}<li><code>{tool.name}</code>{#if tool.description}<span class="tool-desc">{tool.description}</span>{/if}</li>{/each}</ul>
            {/if}
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}

{#if dialog}
  <div class="form-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) closeDialog(); }}>
    <div class="dialog-card" role="dialog" aria-modal="true" aria-label={dialog.editing ? `Edit ${dialog.editing.name}` : 'Add MCP server'}>
      <div class="dialog-head">
        <h3>{dialog.editing ? `Edit ${dialog.editing.name}` : 'Add a tool server'}</h3>
        <button class="icon-button" onclick={closeDialog} aria-label="Close dialog"><X size={18} /></button>
      </div>
      <div class="mode-switch" role="tablist" aria-label="Input mode">
        <button role="tab" aria-selected={dialog.mode === 'form'} class:active={dialog.mode === 'form'} onclick={() => (dialog = dialog ? { ...dialog, mode: 'form' } : null)}>Form</button>
        <button role="tab" aria-selected={dialog.mode === 'json'} class:active={dialog.mode === 'json'} onclick={() => (dialog = dialog ? { ...dialog, mode: 'json' } : null)}>Paste JSON</button>
      </div>

      {#if dialog.mode === 'form'}
        <form class="dialog-body" onsubmit={(e) => { e.preventDefault(); void saveForm(); }}>
          <label>Name<input bind:value={formName} placeholder="github" disabled={!!dialog.editing && !validManagedName(dialog.editing.name)} /></label>
          <fieldset class="transport">
            <legend>Transport</legend>
            <div class="transport-options">
              <label class="radio"><input type="radio" name="transport" value="local" bind:group={formTransport} />Local process (stdio)</label>
              <label class="radio"><input type="radio" name="transport" value="remote" bind:group={formTransport} />Remote URL (http)</label>
              <label class="radio"><input type="radio" name="transport" value="both" bind:group={formTransport} />Both</label>
            </div>
          </fieldset>
          {#if formTransport !== 'remote'}
            <label>Command<input bind:value={formCommand} placeholder="npx -y @modelcontextprotocol/server-github" spellcheck="false" /></label>
            <label>Arguments (one per line — values are kept byte-exact)<textarea bind:value={formArgs} rows="2" placeholder={'--directory\n/home/me/My Project'}></textarea></label>
            <label>Environment (one KEY=value per line; the value is the rest of the line, verbatim)<textarea bind:value={formEnv} rows="3" placeholder="GITHUB_TOKEN=ghp_…"></textarea></label>
          {/if}
          {#if formTransport !== 'local'}
            <label>HTTP URL{#if formTransport === 'remote'}&nbsp;(required){/if}<input bind:value={formUrl} placeholder="https://mcp.example.com/mcp" spellcheck="false" /></label>
          {/if}
          <p class="muted hint">Both transports on one entry is valid. Env values are stored in the manifest file; newline-bearing values belong in Paste JSON mode.</p>
          {#if formError}
            <p class="form-error" role="alert">{formError.message}</p>
          {/if}
          <div class="form-actions">
            <button type="button" class="secondary" onclick={closeDialog} disabled={saving}>Cancel</button>
            <button type="submit" class="primary" disabled={saving}>{saving ? 'Saving…' : dialog.editing ? 'Save changes' : 'Add server'}</button>
          </div>
        </form>
      {:else}
        <div class="dialog-body">
          {#if dialog.editing === null}
            <label class={pasteLooksSingle ? '' : 'disabled-field'}>
              Server name{#if pasteLooksSingle}&nbsp;(single-object paste){/if}
              <input bind:value={jsonName} placeholder={pasteLooksSingle ? 'filesystem' : 'names come from the JSON keys'} disabled={!pasteLooksSingle} />
            </label>
          {:else}
            <p class="muted hint">Editing <strong>{dialog.editing.name}</strong> as JSON — the document must hold exactly this one server; its transports replace the manifest wholesale and the name stays {dialog.editing.name}.</p>
          {/if}
          <label>Paste standard mcpServers JSON<textarea class="mono" bind:value={jsonText} rows="10" spellcheck="false" placeholder={'{\n  "mcpServers": {\n    "filesystem": {\n      "command": "npx",\n      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/Users/username/Desktop"]\n    }\n  }\n}'}></textarea></label>
          {#if jsonError}
            <p class="form-error" role="alert">{jsonError.message}</p>
          {/if}
          {#if importResults}
            <div class="import-results" role="status">
              {#each importResults as r}
                {@const Icon = outcomeIcon[r.status] ?? AlertTriangle}
                <div class="import-row" data-status={r.status}>
                  <Icon size={15} />
                  <strong>{r.source_key}</strong>
                  {#if r.name && r.name !== r.source_key}<span class="muted">→ {r.name}</span>{/if}
                  <span class="import-status">{r.status}{r.reason ? ` · ${r.reason}` : ''}</span>
                  {#if r.message}<span class="muted import-msg">{r.message}</span>{/if}
                  {#if r.warnings?.length}{#each r.warnings as warning}<span class="muted import-msg">{warning}</span>{/each}{/if}
                </div>
              {/each}
            </div>
          {/if}
          <div class="form-actions">
            <button type="button" class="secondary" onclick={closeDialog} disabled={importing}>Close</button>
            <button type="button" class="primary" onclick={() => void runImport()} disabled={importing || jsonText.trim() === ''}>{importing ? 'Importing…' : dialog.editing ? 'Save changes' : 'Import'}</button>
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .section-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--ds-space-4); margin-bottom: var(--ds-space-6); }
  .section-head p { margin-top: var(--ds-space-1); font-size: 0.8125rem; }
  .card { border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-dialog); background: var(--ds-color-surface); padding: var(--ds-space-6); }
  .detail-head { display: grid; gap: var(--ds-space-1); }
  .path { font-size: 0.75rem; color: var(--ds-color-muted); overflow-wrap: anywhere; }
  .fields { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ds-space-4); }
  .fields .full { grid-column: 1 / -1; }
  .fields label { font-size: 0.8125rem; color: var(--ds-color-muted); font-weight: 500; gap: var(--ds-space-2); }
  .actions { display: flex; align-items: center; gap: var(--ds-space-2); flex-wrap: wrap; }
  .panel { display: grid; gap: var(--ds-space-3); padding: var(--ds-space-4); border-radius: var(--ds-radius-control); font-size: 0.8125rem; margin-bottom: var(--ds-space-4); }
  .error-panel { border: 1px solid var(--ds-color-border); background: var(--ds-color-danger-soft); }
  .warn-panel { border: 1px solid var(--ds-color-border); }
  .warn-panel ul { margin: 0; padding-left: var(--ds-space-4); display: grid; gap: var(--ds-space-1); overflow-wrap: anywhere; }
  .danger-panel { border-color: transparent; background: var(--ds-color-danger-soft); }
  .danger-panel .form-actions { justify-content: flex-end; }
  .empty { display: grid; justify-items: center; gap: var(--ds-space-3); padding: var(--ds-space-8); text-align: center; color: var(--ds-color-muted); }
  .item-list { display: grid; gap: var(--ds-space-2); list-style: none; margin: 0; padding: 0; }
  .item-list li { display: flex; align-items: center; gap: var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); transition: background-color 120ms ease, border-color 120ms ease, opacity 120ms ease; }
  .item-list li:hover { background: var(--ds-color-hover); }
  .item-list li.disabled { opacity: 0.62; }
  .item-list li > button { flex: 1; min-width: 0; display: flex; align-items: center; text-align: left; gap: var(--ds-space-3); padding: var(--ds-space-3) var(--ds-space-4); color: var(--ds-color-muted); }
  .row-switch { padding-right: var(--ds-space-3); }
  .item-title { flex: 1; min-width: 0; display: grid; gap: 2px; }
  .item-list strong { font-weight: 550; overflow-wrap: anywhere; color: var(--ds-color-text); }
  small { display: block; font-size: 0.75rem; color: var(--ds-color-muted); }
  .pills { display: flex; gap: var(--ds-space-1); }
  .pill { font-size: 0.6875rem; font-weight: 500; color: var(--ds-color-muted); background: var(--ds-color-hover); border-radius: var(--ds-radius-pill); padding: 3px var(--ds-space-2); }
  .pill.off { color: var(--ds-color-danger); background: var(--ds-color-danger-soft); }
  .probe { display: grid; gap: var(--ds-space-2); border-top: 1px solid var(--ds-color-border); padding-top: var(--ds-space-4); }
  .probe-row { display: flex; align-items: center; gap: var(--ds-space-2); flex-wrap: wrap; font-size: 0.8125rem; }
  .probe-row p { width: 100%; margin: 0; font-size: 0.8125rem; }
  .status-dot { width: 8px; height: 8px; border-radius: var(--ds-radius-pill); background: var(--ds-color-muted); }
  .probe-row[data-status='ok'] .status-dot { background: var(--ds-color-success); }
  .probe-row[data-status='failed'] .status-dot { background: var(--ds-color-danger); }
  .fail-reason { color: var(--ds-color-danger); font-weight: 500; font-size: 0.75rem; }
  .tool-list { display: flex; flex-wrap: wrap; gap: var(--ds-space-2); list-style: none; padding: 0; margin: 0; }
  .tool-list li { display: inline-flex; align-items: baseline; gap: var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-1) var(--ds-space-3); font-size: 0.8125rem; }
  .tool-list code { overflow-wrap: anywhere; }
  .tool-desc { color: var(--ds-color-muted); font-size: 0.75rem; }

  /* Dialog (add/edit) — the Providers modal pattern. */
  .form-backdrop { position: fixed; inset: 0; background: rgb(0 0 0 / 0.45); display: grid; place-items: center; z-index: 5; padding: var(--ds-space-4); }
  .dialog-card { background: var(--ds-color-background); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-dialog); padding: var(--ds-space-6); width: min(34rem, 100%); max-height: 90vh; overflow-y: auto; display: grid; gap: var(--ds-space-4); }
  .dialog-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ds-space-4); }
  .dialog-head h3 { font-size: 1.0625rem; font-weight: 600; }
  .mode-switch { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ds-space-1); background: var(--ds-color-field); border-radius: var(--ds-radius-control); padding: var(--ds-space-1); }
  .mode-switch button { padding: var(--ds-space-2); border-radius: calc(var(--ds-radius-control) - 2px); font-size: var(--ds-font-nav); font-weight: 500; color: var(--ds-color-muted); }
  .mode-switch button.active { background: var(--ds-color-surface); color: var(--ds-color-text); box-shadow: 0 1px 2px rgb(0 0 0 / 0.08); }
  .dialog-body { display: grid; gap: var(--ds-space-4); }
  .dialog-body label { font-size: 0.8125rem; color: var(--ds-color-muted); font-weight: 500; gap: var(--ds-space-2); }
  .dialog-body fieldset { border: 0; padding: 0; margin: 0; display: grid; gap: var(--ds-space-2); }
  .dialog-body legend { font-size: 0.8125rem; color: var(--ds-color-muted); font-weight: 500; padding: 0; float: left; }
  .transport-options { display: flex; gap: var(--ds-space-4); flex-wrap: wrap; }
  .radio { display: flex; align-items: center; gap: var(--ds-space-2); min-height: 32px; }
  .radio input { width: 16px; height: 16px; min-height: 16px; }
  .hint { font-size: 0.75rem; line-height: 1.6; }
  .mono { font-family: ui-monospace, 'SF Mono', 'Cascadia Code', Menlo, monospace; font-size: 0.8125rem; }
  .form-actions { display: flex; justify-content: flex-end; gap: var(--ds-space-2); }
  .disabled-field { opacity: 0.6; }
  .import-results { display: grid; gap: var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-3); }
  .import-row { display: flex; align-items: baseline; gap: var(--ds-space-2); flex-wrap: wrap; font-size: 0.8125rem; }
  .import-row :global(svg) { align-self: center; }
  .import-row[data-status='created'] :global(svg) { color: var(--ds-color-success); }
  .import-row[data-status='failed'] :global(svg) { color: var(--ds-color-danger); }
  .import-row[data-status='skipped'] :global(svg) { color: var(--ds-color-muted); }
  .import-status { font-weight: 500; font-size: 0.75rem; color: var(--ds-color-muted); }
  .import-msg { width: 100%; font-size: 0.75rem; }
  @media (max-width: 767px) { .fields { grid-template-columns: minmax(0, 1fr); } .section-head { flex-direction: column; } }
</style>
