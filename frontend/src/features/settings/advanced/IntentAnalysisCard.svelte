<script lang="ts">
  import { Crosshair, FlaskConical } from '@lucide/svelte';
  import CapabilityCard from './CapabilityCard.svelte';
  import {
    getIntentAnalysis,
    putIntentAnalysis,
    testIntentAnalysis,
    getModelCatalog,
  } from '../../../shared/api/client';
  import type { IntentAnalysisStatus, IntentAnalysisTestResult } from '../../../shared/api/client';
  import { fmtTokens } from '../../../entities/timeline-event/analysisText';

  type CatalogEntry = { id: string; context_window?: number };
  type Catalog = Record<string, CatalogEntry[]>;

  let open = $state(false);
  let status = $state<IntentAnalysisStatus | null>(null);
  let models = $state<Catalog>({});
  let provider = $state('');
  let model = $state('');
  let connected = $state<boolean | null>(null);
  let loading = $state(false);
  let saving = $state(false);
  let testing = $state(false);
  let error = $state('');
  let saved = $state(false);
  let dirty = $state(false);
  let testGoal = $state('');
  let testResult = $state<IntentAnalysisTestResult | null>(null);

  const eff = $derived(status?.effective ?? null);
  const selected = $derived(provider !== '' && model !== '');
  const badge = $derived(
    connected === false
      ? 'Daemon unreachable'
      : eff && !eff.enabled
        ? 'Disabled'
        : selected
          ? 'Configured'
          : loading
            ? 'Loading…'
            : 'Uses model defaults',
  );
  const badgeTone = $derived<'ok' | 'warn' | ''>(
    connected === false || (eff && !eff.enabled) ? 'warn' : selected ? 'ok' : '',
  );
  // The collapsed summary only shows a selection the daemon confirmed.
  const summary = $derived(connected === true && !dirty && selected ? `${provider} · ${model}` : '');
  // The probe runs the SAVED selection: a dirty picker is not what it
  // would test.
  const canTest = $derived(connected === true && eff?.enabled === true && !dirty && !saving && !testing);
  const advanced = $derived(() => {
    const s = status?.settings;
    const parts: string[] = [];
    if (s?.reasoning_effort) parts.push(`effort ${s.reasoning_effort}`);
    if (s?.deadline_seconds) parts.push(`deadline ${s.deadline_seconds}s`);
    if (s?.max_attempts) parts.push(`${s.max_attempts} attempts`);
    return parts.join(' · ');
  });
  // The read-only derivation line — nothing here is editable by design.
  const derivation = $derived.by<string>(() => {
    if (!eff) return '';
    if (!eff.enabled) return 'Analysis is disabled (config.yaml → intent_analysis.enabled: false).';
    if (!eff.model) return 'No model selected — defaults apply: the analyst agent file, config.yaml, the run\u2019s model (a ward\u2019s orchestrator model wins whenever a ward hosts the session).';
    const window = eff.context_window ? `${fmtTokens(eff.context_window)} (${sourceLabel(eff.window_source)})` : 'unknown';
    return `${eff.model} · window ${window} · analyst: full window · ${eff.packet_tokens ? `packet ~${fmtTokens(eff.packet_tokens)} tokens` : 'packet size unknown'} · ${
      eff.trimming ? 'packet would be trimmed to fit' : 'no trimming'
    }`;
  });

  function sourceLabel(s: string | undefined): string {
    switch (s) {
      case 'ollama': return 'reported by Ollama';
      case 'table': return 'limits table';
      case 'fallback': return 'unknown model, 32k fallback';
      default: return 'unknown';
    }
  }

  async function load() {
    loading = true;
    connected = null;
    try {
      const [s, catalog] = await Promise.all([getIntentAnalysis(), getModelCatalog()]);
      status = s;
      provider = s.settings.provider ?? '';
      model = s.settings.model ?? '';
      const grouped: Catalog = {};
      for (const m of catalog.models ?? []) {
        (grouped[m.provider] ??= []).push({ id: m.id, context_window: m.context_window });
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
      // Only the selection is sent — the advanced fields live in the
      // file and omitted fields keep their saved values.
      status = await putIntentAnalysis({ provider, model });
      saved = true;
      dirty = false;
      testResult = null; // a result for the previous selection no longer applies
    } catch (e) {
      error = e instanceof Error ? e.message : 'Save failed';
    } finally {
      saving = false;
    }
  }

  async function runTest() {
    testing = true;
    error = '';
    testResult = null;
    try {
      testResult = await testIntentAnalysis(testGoal.trim() || undefined);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Test failed';
    } finally {
      testing = false;
    }
  }

  function onProviderChange() {
    model = '';
    dirty = true;
    saved = false;
    testResult = null;
  }

  function onModelChange() {
    dirty = true;
    saved = false;
    testResult = null;
  }

  $effect(() => {
    load();
  });
</script>

<CapabilityCard
  icon={Crosshair}
  title="Intent Analysis"
  description="The pre-execution decision before every research run — model, limits, and a test probe"
  badge={badge}
  badgeTone={badgeTone}
  summary={summary}
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
        <select bind:value={model} onchange={onModelChange}>
          <option value="" disabled>Select model</option>
          {#each models[provider] ?? [] as m}
            <option value={m.id}>{m.id}{m.context_window ? ` · ${fmtTokens(m.context_window)}` : ''}</option>
          {/each}
        </select>
      </label>
    </div>
    {#if Object.keys(models).length === 0}
      <p class="muted">No models available. Configure a provider and refresh its catalog in the Providers section.</p>
    {/if}

    <!-- The derivation is read-only: limits are the selected model's own. -->
    <p class="derivation" role="status" aria-live="polite">
      <span class="derivation-label">Limits</span>
      {derivation}
    </p>
    <p class="muted small">
      Output asks for the full window; the packet shrinks only if the prompt would exceed window − {fmtTokens(eff?.output_reserve ?? 4000)}.
      Advanced fields ({advanced() || 'reasoning_effort, deadline_seconds, max_attempts'}) live in <code>~/.zbot/config/intent-analysis.json</code>.
    </p>

    {#if error}<p class="muted" role="alert" style="color:var(--ds-color-danger)">{error}</p>{/if}
    {#if saved}<p class="muted" role="status" style="color:var(--ds-color-success)">Saved — the next research run analyzes with this model unless a ward's orchestrator model overrides it.</p>{/if}
    <div class="actions">
      <button class="primary" onclick={() => void save()} disabled={saving || !selected || (!dirty && !saved)}>Save changes</button>
      {#if dirty}<button class="secondary" onclick={() => void load()}>Discard</button>{/if}
    </div>

    <section class="test" aria-labelledby="intent-test-heading">
      <h4 id="intent-test-heading" class="test-title"><FlaskConical size={14} aria-hidden="true" /> Test analysis</h4>
      <p class="muted small">Runs one real decision with the saved selection and reports its cost. Nothing is executed and no session is created.{dirty ? ' Save changes before testing.' : ''}</p>
      <label class="test-goal">Goal (optional)
        <input bind:value={testGoal} placeholder="Leave empty for a sample research goal" maxlength="8192" />
      </label>
      <div class="actions">
        <button class="secondary" onclick={() => void runTest()} disabled={!canTest}>{testing ? 'Analyzing…' : 'Test analysis'}</button>
      </div>
      {#if testResult}
        <div class="test-result" role="status">
          <p class="test-status" class:failed={testResult.status === 'failed'}>
            {testResult.status === 'analyzed' ? 'Analyzed' : 'Failed'}
            {#if testResult.status === 'failed' && testResult.reason}<span class="muted"> — {testResult.reason}</span>{/if}
          </p>
          <dl class="test-meta">
            {#if testResult.model}<div><dt>Model</dt><dd>{testResult.model}</dd></div>{/if}
            {#if testResult.context_window}<div><dt>Window</dt><dd>{fmtTokens(testResult.context_window)} ({sourceLabel(testResult.window_source)})</dd></div>{/if}
            <div><dt>Tokens</dt><dd>{fmtTokens(testResult.tokens_in)} in / {fmtTokens(testResult.tokens_out)} out</dd></div>
            <div><dt>Duration</dt><dd>{testResult.duration_ms} ms</dd></div>
            <div><dt>Attempts</dt><dd>{testResult.attempts}</dd></div>
            <div><dt>Prompt</dt><dd>{testResult.packet_tokens ? `~${fmtTokens(testResult.packet_tokens)} tokens` : 'size unknown'} · {testResult.trimmed ? 'trimmed to fit' : 'no trimming'}</dd></div>
            {#if testResult.analysis?.primary_intent}
              <div><dt>Decision</dt><dd>{testResult.analysis.primary_intent}{testResult.analysis.complexity ? ` · ${testResult.analysis.complexity}` : ''}{testResult.analysis.approach ? ` · ${testResult.analysis.approach}` : ''}{testResult.analysis.ward_name ? ` · ward ${testResult.analysis.ward_name}` : ''}</dd></div>
            {/if}
          </dl>
        </div>
      {/if}
    </section>
  {/if}
</CapabilityCard>

<style>
  .fields { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ds-space-4); margin: var(--ds-space-4) 0; }
  .fields label { display: flex; flex-direction: column; gap: var(--ds-space-1); font-size: 0.8125rem; color: var(--ds-color-muted); font-weight: 500; }
  .actions { display: flex; align-items: center; gap: var(--ds-space-2); flex-wrap: wrap; margin: var(--ds-space-3) 0; }
  .small { font-size: var(--ds-font-nav); }
  .derivation { margin: var(--ds-space-3) 0 var(--ds-space-1); padding: var(--ds-space-2) var(--ds-space-3); border: 1px solid var(--ds-color-border); border-radius: 8px; font-size: 0.8125rem; background: var(--ds-color-surface); }
  .derivation-label { display: inline-block; margin-right: var(--ds-space-2); font-weight: 600; color: var(--ds-color-muted); text-transform: uppercase; font-size: 0.6875rem; letter-spacing: 0.04em; }
  .test { margin-top: var(--ds-space-4); padding-top: var(--ds-space-4); border-top: 1px solid var(--ds-color-border); }
  .test-title { display: flex; align-items: center; gap: var(--ds-space-2); margin: 0 0 var(--ds-space-1); font-size: 0.875rem; }
  .test-goal { display: flex; flex-direction: column; gap: var(--ds-space-1); margin: var(--ds-space-2) 0; font-size: 0.8125rem; color: var(--ds-color-muted); font-weight: 500; }
  .test-result { margin-top: var(--ds-space-3); }
  .test-status { font-weight: 600; margin: 0 0 var(--ds-space-2); color: var(--ds-color-success); }
  .test-status.failed { color: var(--ds-color-danger); }
  .test-meta { display: grid; grid-template-columns: max-content 1fr; gap: var(--ds-space-1) var(--ds-space-4); margin: 0; font-size: 0.8125rem; }
  .test-meta dt { color: var(--ds-color-muted); }
  .test-meta dd { margin: 0; }
  @media (max-width: 720px) { .fields { grid-template-columns: minmax(0, 1fr); } }
</style>
