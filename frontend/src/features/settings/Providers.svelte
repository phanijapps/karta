<script lang="ts">
  import { Server } from '@lucide/svelte';
  import {
    listProviders,
    listProviderModels,
    refreshProviderModels,
    createProvider,
    updateProvider,
    deleteProvider,
    type CredentialInput,
    type ModelsFailureReason,
    type ModelsResult,
    type MutationFailure,
    type Provider,
  } from '../../shared/api/client';

  // Live, read-only-on-config / managed-on-files provider view.
  type LoadState =
    | { kind: 'loading' }
    | { kind: 'ready'; list: Provider[] }
    | { kind: 'error'; message: string };

  let load = $state<LoadState>({ kind: 'loading' });
  let providerIndex = $state(0);
  let testResult = $state<ModelsResult | null>(null);
  let testing = $state(false);

  // Add/edit form state (managed providers only).
  let showForm = $state(false);
  let editing = $state<Provider | null>(null);
  let formName = $state('');
  let formUrl = $state('');
  let formCredKind = $state<'none' | 'env' | 'keyring'>('none');
  let formCredEnv = $state('');
  let formCredKey = $state('');
  let formError = $state<MutationFailure | null>(null);
  let credError = $state(''); // client-side credential validation
  let saving = $state(false);
  let confirmingDelete = $state<Provider | null>(null);

  const managedNamePattern = /^[a-z0-9][a-z0-9-]{0,63}$/;
  // Windows device names can never be file stems (mirrors the backend).
  const reservedNames = new Set(['con', 'prn', 'aux', 'nul', ...Array.from({ length: 9 }, (_, i) => `com${i + 1}`), ...Array.from({ length: 9 }, (_, i) => `lpt${i + 1}`)]);
  const validManagedName = (name: string) => managedNamePattern.test(name) && !reservedNames.has(name);

  $effect(() => {
    void reload();
  });

  async function reload() {
    load = { kind: 'loading' };
    testResult = null;
    try {
      const list = await listProviders();
      load = { kind: 'ready', list };
      if (providerIndex >= list.length) providerIndex = 0;
    } catch (err) {
      load = { kind: 'error', message: err instanceof Error ? err.message : String(err) };
    }
  }

  const selected = $derived(load.kind === 'ready' ? load.list[providerIndex] : undefined);

  // Manual catalog refresh: live-fetches this provider's models and
  // updates the daemon's model catalog (the header picker reads it).
  async function refreshModels(name: string) {
    testing = true;
    testResult = null;
    const result = await refreshProviderModels(name);
    const current = selected;
    if (current?.name === name) {
      testResult = result;
    }
    testing = false;
    if (result.ok) window.dispatchEvent(new CustomEvent('karta:models-refreshed'));
  }

  async function testConnection(name: string) {
    testing = true;
    testResult = null;
    const result = await listProviderModels(name);
    const current = selected;
    if (current?.name === name) {
      testResult = result;
    }
    testing = false;
    if (result.ok) window.dispatchEvent(new CustomEvent('karta:models-refreshed'));
  }

  function openCreate() {
    editing = null;
    formName = '';
    formUrl = '';
    formCredKind = 'none';
    formCredEnv = '';
    formCredKey = '';
    formError = null;
    showForm = true;
  }

  function openEdit(p: Provider) {
    editing = p;
    formName = p.name;
    formUrl = p.base_url;
    // Existing credential reference (never its value) preselects the kind.
    formCredKind = p.credential === 'env' || p.credential === 'keyring' ? p.credential : 'none';
    formCredEnv = p.api_key_env ?? '';
    formCredKey = '';
    formError = null;
    showForm = true;
  }

  const testGuidance: Record<ModelsFailureReason, string> = {
    unreachable: 'Could not reach the provider. Check that it is running (for ollama: ollama serve) and that the base URL points at its API root (e.g. http://localhost:11434/v1).',
    unauthorized: 'The provider rejected the credential. Check the credential value — this page never shows secret values.',
    malformed: 'The provider answered, but its model list could not be read. Verify the endpoint is OpenAI-compatible and serves /models.',
    credential_unavailable: 'The stored credential could not be retrieved from the OS keyring (is the keyring service running and unlocked?). The request was not sent without your credential.',
    unknown: 'This provider is no longer in the daemon’s configuration. Reload or check its source file.',
    daemon: 'The daemon connection was lost. Is zbotd running? Retry once it is back.',
  };

  const mutationGuidance: Record<MutationFailure['code'], string> = {
    duplicate_name: 'A provider with that name already exists. Pick another name.',
    origin_protected: 'This provider is declared in ~/.zbot/config.yaml — edit that file instead.',
    provider_limit: 'The provider list is full. Delete one before adding another.',
    keyring_unavailable: 'The OS keyring is unavailable (is it running and unlocked?). The provider was not saved.',
    invalid_provider: 'Check the highlighted fields — names are lowercase kebab (1–64 chars), URLs absolute http(s).',
    invalid_request: 'The request was malformed. Check the fields and retry.',
    daemon: 'The daemon connection failed. Is zbotd running?',
  };

  async function save() {
    formError = null;
    credError = '';
    formName = formName.trim();
    formUrl = formUrl.trim();
    if (!validManagedName(formName) || !/^https?:\/\/.+/.test(formUrl)) {
      // Field-level hints are live under the inputs; name them here too.
      if (!validManagedName(formName)) credErrorUnusedGuard: ;
      formError = {
        code: 'invalid_provider',
        message: !validManagedName(formName)
          ? 'the provider name must be lowercase kebab (1–64 chars, not a reserved device name)'
          : 'the base URL must be absolute http(s), including the API root (e.g. https://api.z.ai/v1)',
      };
      return;
    }
    // Explicit credential intent. Absent credential on edit = retain:
    // keyring with no new key keeps the stored key; env requires a name.
    let credential: CredentialInput | undefined;
    if (formCredKind === 'env') {
      if (!formCredEnv) {
        formError = { code: 'invalid_provider', message: 'env credential needs a variable name' };
        return;
      }
      credential = { kind: 'env', env_name: formCredEnv };
    } else if (formCredKind === 'keyring') {
      if (formCredKey) {
        credential = { kind: 'keyring', key: formCredKey };
      } else if (editing === null || editing.credential !== 'keyring') {
        credError = 'Enter the API key to store in the OS keyring.';
        return;
      } else {
        credential = { kind: 'keyring' }; // edit, blank key: retain the stored key
      }
    } else if (formCredKind === 'none') {
      credential = editing ? { kind: 'none' } : undefined; // clear on edit; nothing to clear on create
    }
    saving = true;
    const result =
      editing === null
        ? await createProvider(formName, formUrl, credential)
        : await updateProvider(editing.name, {
            name: formName !== editing.name ? formName : undefined,
            base_url: formUrl !== editing.base_url ? formUrl : undefined,
            credential,
          });
    saving = false;
    if (result.ok) {
      showForm = false;
      window.dispatchEvent(new CustomEvent('karta:models-refreshed'));
      await reload();
      const name = result.provider?.name ?? formName;
      const list = load.kind === 'ready' ? load.list : [];
      providerIndex = Math.max(0, list.findIndex((p) => p.name === name));
    } else {
      formError = result; // draft stays editable
    }
  }

  async function confirmDelete() {
    if (!confirmingDelete) return;
    const result = await deleteProvider(confirmingDelete.name);
    confirmingDelete = null;
    if (!result.ok) {
      formError = result;
      showForm = true;
    }
    window.dispatchEvent(new CustomEvent('karta:models-refreshed'));
    await reload();
  }
  function credentialSummary(p: Provider): string {
    if (p.credential === 'env') return p.api_key_env ?? 'environment variable';
    if (p.credential === 'keyring') return 'OS keyring';
    return 'None';
  }

  function credentialHint(p: Provider): string {
    if (p.credential === 'none') return 'This provider needs no credential. No secret values are stored, read, or shown.';
    if (p.credential_status === 'ready') return 'The credential resolves; requests send it as a bearer token.';
    if (p.credential_status === 'unavailable') return 'The OS keyring cannot be reached — check that it is running and unlocked.';
    return 'The credential is referenced but currently unset; requests are sent without it.';
  }
</script>

<section aria-label="Providers">
  {#if load.kind === 'loading'}
    <p class="muted" role="status">Loading providers…</p>
  {:else if load.kind === 'error'}
    <div class="daemon-error" role="alert">
      <p><strong>Daemon unreachable.</strong> {load.message}</p>
      <p class="muted">Start the daemon (<code>go run ./cmd/zbotd</code> from <code>backend/</code>) and retry. No fixture data is shown.</p>
      <button class="secondary" onclick={() => void reload()}>Retry</button>
    </div>
  {:else if load.list.length === 0}
    <div class="empty">
      <p class="muted">No providers configured.</p>
      <button class="secondary" onclick={openCreate}>Add provider</button>
    </div>
  {:else}
    <div class="item-list">
      {#each load.list as provider, i}
        <button class:selected={providerIndex === i} aria-pressed={providerIndex === i} onclick={() => { providerIndex = i; testResult = null; }}>
          <Server size={18} />
          <span><strong>{provider.name}</strong><small>{provider.type}{provider.origin === 'config' ? ' · from config.yaml' : ''}</small></span>
          {#if provider.origin === 'file'}<span class="badge">Managed</span>{:else}<span class="badge muted-badge">config.yaml</span>{/if}
          {#if provider.credential !== 'none'}<span class="badge" class:warn={provider.credential_status !== 'ready'}>{provider.credential_status === 'ready' ? 'Credential set' : provider.credential_status === 'unavailable' ? 'Keyring unavailable' : 'Credential missing'}</span>{/if}
        </button>
      {/each}
    </div>
    {#if selected}
      {@const editable = selected.origin === 'file'}
      <div class="fields">
        <label>Provider name<input readonly value={selected.name} /></label>
        <label>Connection type<input readonly value={selected.type} /></label>
        <label class="full">Base URL<input readonly value={selected.base_url} spellcheck="false" /></label>
        <label class="full">Credential<input readonly value={credentialSummary(selected)} spellcheck="false" /><small class="muted">{credentialHint(selected)}</small></label>
      </div>
      <div class="actions">
        <button class="secondary" disabled={testing} onclick={() => void testConnection(selected.name)}>{testing ? 'Testing…' : 'Test connection'}</button>
        <button class="secondary" disabled={testing} onclick={() => void refreshModels(selected.name)}>{testing ? 'Refreshing…' : 'Refresh models'}</button>
        {#if editable}<button class="secondary" onclick={() => openEdit(selected)}>Edit</button>{/if}
        {#if editable}<button class="secondary danger" onclick={() => (confirmingDelete = selected)}>Delete</button>{/if}
        <button class="secondary" onclick={openCreate}>Add provider</button>
      </div>
      {#if testResult}
        {#if testResult.ok}
          <div class="test-result" role="status">
            <h3>Connection OK — models ({testResult.models.length})</h3>
            {#if testResult.models.length === 0}
              <p class="muted">The provider returned an empty model list (for ollama: pull one with <code>ollama pull …</code>).</p>
            {:else}
              <ul class="model-list">{#each testResult.models as model}<li><code>{model.id}</code></li>{/each}</ul>
            {/if}
          </div>
        {:else}
          <div class="test-result" role="alert">
            <h3>Connection failed</h3>
            <p>{testGuidance[testResult.reason]}</p>
          </div>
        {/if}
      {/if}
    {/if}
  {/if}

  {#if showForm}
    <div class="form-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) showForm = false; }}>
      <form class="provider-form" aria-label={editing ? 'Edit provider' : 'Add provider'} onsubmit={(e) => { e.preventDefault(); void save(); }}>
        <h3>{editing ? `Edit ${editing.name}` : 'Add provider'}</h3>
        <label>Provider name<input bind:value={formName} placeholder="lowercase-kebab" spellcheck="false" />{#if !validManagedName(formName) && formName !== ''}<small class="muted">Lowercase letters, digits, dashes; 1–64 chars; not a reserved device name (e.g. con). The name becomes the file name — use "zai", not "z.ai".</small>{/if}</label>
        <label>Base URL<input bind:value={formUrl} placeholder="http://localhost:11434/v1" spellcheck="false" />{#if formUrl !== '' && !/^https?:\/\/.+/.test(formUrl.trim())}<small class="muted">Absolute http(s) URL including the API root — did the https:// prefix get lost?</small>{/if}</label>
        <fieldset>
          <legend>Credential</legend>
          <label class="radio"><input type="radio" name="cred" value="none" bind:group={formCredKind} />None (e.g. local ollama)</label>
          <label class="radio"><input type="radio" name="cred" value="env" bind:group={formCredKind} />Environment variable</label>
          {#if formCredKind === 'env'}<label>Variable name<input bind:value={formCredEnv} placeholder="ZAI_API_KEY" spellcheck="false" /></label>{/if}
          <label class="radio"><input type="radio" name="cred" value="keyring" bind:group={formCredKind} />Store an API key in the OS keyring</label>
          {#if formCredKind === 'keyring'}<label>API key<input type="password" bind:value={formCredKey} autocomplete="off" /><small class="muted">{editing && editing.credential === 'keyring' ? 'Leave blank to keep the stored key.' : 'Stored once in your OS keyring; never shown again.'}</small></label>{/if}
          {#if credError}<small class="form-error">{credError}</small>{/if}
        </fieldset>
        {#if formError}<p class="form-error" role="alert">{mutationGuidance[formError.code]}{#if formError.code === 'invalid_provider' && formError.message} — {formError.message}{/if}</p>{/if}
        <div class="form-actions">
          <button type="button" class="secondary" onclick={() => (showForm = false)}>Cancel</button>
          <button type="submit" class="secondary" disabled={saving}>{saving ? 'Saving…' : editing ? 'Save changes' : 'Create provider'}</button>
        </div>
      </form>
    </div>
  {/if}

  {#if confirmingDelete}
    <div class="form-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) confirmingDelete = null; }}>
      <div class="provider-form" role="alertdialog" aria-label="Delete provider">
        <h3>Delete {confirmingDelete.name}?</h3>
        <p class="muted">The provider file is removed{confirmingDelete.credential === 'keyring' ? ' and its stored API key is deleted from the OS keyring' : ''}. This cannot be undone.</p>
        <div class="form-actions">
          <button class="secondary" onclick={() => (confirmingDelete = null)}>Cancel</button>
          <button class="secondary danger" onclick={() => void confirmDelete()}>Delete</button>
        </div>
      </div>
    </div>
  {/if}
</section>


<style>
  .item-list { display: grid; gap: var(--ds-space-2); margin: var(--ds-space-4) 0 var(--ds-space-6); }
  .item-list button { display: flex; align-items: center; text-align: left; gap: var(--ds-space-3); padding: var(--ds-space-3); border: 1px solid var(--ds-color-border); }
  .item-list button.selected { background: var(--ds-color-hover); }
  .item-list span:not(.badge) { flex: 1; min-width: 0; }
  .item-list strong { font-weight: 550; overflow-wrap: anywhere; }
  small { display: block; font-size: 0.75rem; color: var(--ds-color-muted); margin-top: var(--ds-space-1); }
  .badge { font-size: 0.6875rem; border: 1px solid var(--ds-color-border); border-radius: 999px; padding: 2px var(--ds-space-2); color: var(--ds-color-muted); }
  .badge.warn { color: var(--ds-color-muted); }
  .muted-badge { opacity: 0.8; }
  .fields { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ds-space-6) var(--ds-space-4); margin: var(--ds-space-6) 0; }
  .fields .full { grid-column: 1 / -1; }
  input[readonly] { opacity: 0.9; }
  .actions { display: flex; align-items: center; gap: var(--ds-space-2); flex-wrap: wrap; margin: var(--ds-space-6) 0; }
  .secondary { display: inline-flex; align-items: center; justify-content: center; gap: var(--ds-space-2); padding: var(--ds-space-2) var(--ds-space-3); min-height: 36px; border: 1px solid var(--ds-color-border); font-size: var(--ds-font-nav); background: var(--ds-color-background); color: var(--ds-color-text); cursor: pointer; }
  .secondary.danger { color: #b3261e; border-color: #b3261e55; }
  .secondary:disabled { opacity: 0.55; cursor: default; }
  .daemon-error { display: grid; gap: var(--ds-space-3); border: 1px solid var(--ds-color-border); padding: var(--ds-space-4); border-radius: var(--ds-radius-control); }
  .empty { display: grid; gap: var(--ds-space-4); }
  .test-result { border: 1px solid var(--ds-color-border); padding: var(--ds-space-4); border-radius: var(--ds-radius-control); display: grid; gap: var(--ds-space-2); margin-bottom: var(--ds-space-6); }
  .model-list { display: flex; flex-wrap: wrap; gap: var(--ds-space-2); list-style: none; padding: 0; margin: 0; }
  .model-list li { border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-1) var(--ds-space-3); font-size: 0.8125rem; overflow-wrap: anywhere; }
  .form-backdrop { position: fixed; inset: 0; background: rgb(0 0 0 / 0.45); display: grid; place-items: center; z-index: 5; padding: var(--ds-space-4); }
  .provider-form { background: var(--ds-color-background); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-dialog); padding: var(--ds-space-6); width: min(28rem, 100%); display: grid; gap: var(--ds-space-4); max-height: 90vh; overflow-y: auto; }
  .provider-form h3 { font-size: 1rem; font-weight: 600; }
  .provider-form label { display: grid; gap: var(--ds-space-1); font-size: 0.8125rem; color: var(--ds-color-muted); }
  .provider-form input:not([type='radio']) { background: var(--ds-color-surface); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-2); color: var(--ds-color-text); min-height: 36px; }
  fieldset { display: grid; gap: var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-control); padding: var(--ds-space-3); }
  legend { font-size: 0.75rem; color: var(--ds-color-muted); padding: 0 var(--ds-space-2); }
  .radio { display: flex; align-items: center; gap: var(--ds-space-2); }
  .radio input { width: 16px; height: 16px; }
  .form-error { color: #b3261e; font-size: 0.8125rem; }
  .form-actions { display: flex; justify-content: flex-end; gap: var(--ds-space-2); }
</style>
