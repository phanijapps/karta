<script lang="ts">
  import { CalendarClock, CalendarPlus, Pencil, Trash2, PlayCircle, AlertTriangle } from '@lucide/svelte';
  import { listSchedules, createSchedule, updateSchedule, deleteSchedule, type Schedule, type ScheduleInput } from '../../shared/api/client';
  import { tick } from 'svelte';

  let { onopenSession }: { onopenSession: (sessionID: string) => void } = $props();

  let schedules = $state<Schedule[]>([]);
  let loaded = $state(false);
  let showForm = $state(false);
  let editing = $state<Schedule | null>(null);
  let confirmDelete = $state<string | null>(null);
  let error = $state('');

  let name = $state('');
  let scheduleExpr = $state('');
  let goal = $state('');
  let agent = $state('');
  let model = $state('');
  let missedPolicy = $state<'skip' | 'catch_up'>('skip');

  async function refresh() {
    schedules = await listSchedules();
    loaded = true;
  }

  $effect(() => { void refresh(); });

  function openCreate() {
    editing = null; name = ''; scheduleExpr = '@daily'; goal = ''; agent = ''; model = ''; missedPolicy = 'skip';
    error = ''; showForm = true;
  }

  function openEdit(s: Schedule) {
    editing = s; name = s.name; scheduleExpr = s.schedule; goal = s.goal;
    agent = s.agent ?? ''; model = s.model ?? ''; missedPolicy = s.missed_policy ?? 'skip';
    error = ''; showForm = true;
  }

  async function submit() {
    error = '';
    const input: ScheduleInput = { name: name.trim(), schedule: scheduleExpr.trim(), goal: goal.trim() };
    if (input.agent) input.agent = agent.trim();
    if (input.model) input.model = model.trim();
    input.missed_policy = missedPolicy;
    const res = editing ? await updateSchedule(editing.name, input) : await createSchedule(input);
    if (!res.ok) { error = res.error ?? 'failed'; return; }
    showForm = false;
    await refresh();
  }

  async function remove(name: string) {
    if (await deleteSchedule(name)) { confirmDelete = null; await refresh(); }
  }

  function nextFireLabel(s: Schedule): string {
    if (!s.next_fire) return '—';
    const next = new Date(s.next_fire);
    const diff = next.getTime() - Date.now();
    if (diff <= 0) return 'due now';
    const mins = Math.round(diff / 60000);
    if (mins < 60) return `in ${mins}m`;
    const hours = Math.round(mins / 60);
    if (hours < 48) return `in ${hours}h`;
    return next.toLocaleDateString();
  }

  function outcomeLabel(s: Schedule): string {
    switch (s.last_outcome) {
      case 'fired': return 'last run fired';
      case 'skipped_overlapping': return 'last fire skipped (run active)';
      case 'catch_up_fired': return 'caught up after wake';
      case 'skipped_missed': return 'missed (skipped)';
      default: return 'never fired';
    }
  }

  function describe(expr: string): string {
    switch (expr.trim()) {
      case '@hourly': return 'Every hour';
      case '@daily': case '0 0 * * *': return 'Every day at midnight';
      case '@weekly': return 'Every week';
      case '@monthly': return 'Every month';
      default: return expr;
    }
  }
</script>

<div class="schedules-page">
  <header class="page-head">
    <div><h1>Schedules</h1><p class="muted">Goals Karta runs on its own calendar.</p></div>
    <button class="primary" onclick={openCreate}><CalendarPlus size={16} /> New schedule</button>
  </header>

  {#if !loaded}
    <p class="muted">Loading schedules…</p>
  {:else if schedules.length === 0}
    <div class="empty">
      <CalendarClock size={32} strokeWidth={1.2} />
      <h3>No schedules</h3>
      <p class="muted">Create one and Karta will work toward its goal on the cadence you set — even while you're away.</p>
    </div>
  {:else}
    <ul class="sched-list" aria-label="Schedules">
      {#each schedules as s (s.name)}
        <li class="sched-card">
          <div class="sched-head">
            <CalendarClock size={16} aria-hidden="true" />
            <strong>{s.name}</strong>
            <code class="expr" title={s.schedule}>{describe(s.schedule)}</code>
          </div>
          <p class="goal">{s.goal}</p>
          <dl class="sched-meta">
            {#if s.agent}<div><dt>Agent</dt><dd>{s.agent}</dd></div>{/if}
            {#if s.model}<div><dt>Model</dt><dd><code>{s.model}</code></dd></div>{/if}
            <div><dt>Next fire</dt><dd>{nextFireLabel(s)}</dd></div>
            <div><dt>Status</dt><dd>{outcomeLabel(s)}</dd></div>
          </dl>
          <div class="sched-actions">
            <button class="icon-button" aria-label={`Open ${s.name} timeline`} title="Open timeline" onclick={() => onopenSession(`sched-${s.name}`)}><PlayCircle size={16} /></button>
            <button class="icon-button" aria-label={`Edit ${s.name}`} title="Edit" onclick={() => openEdit(s)}><Pencil size={15} /></button>
            <button class="icon-button danger" aria-label={`Delete ${s.name}`} title="Delete" onclick={() => (confirmDelete = s.name)}><Trash2 size={15} /></button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  {#if showForm}
    <div class="modal-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) showForm = false; }}>
      <div class="modal" role="dialog" aria-modal="true" aria-label={editing ? 'Edit schedule' : 'New schedule'}>
        <h2>{editing ? 'Edit schedule' : 'New schedule'}</h2>
        {#if error}<p class="form-error" role="alert"><AlertTriangle size={14} /> {error}</p>{/if}
        <form onsubmit={(e) => { e.preventDefault(); void submit(); }}>
          <label>Name<input bind:value={name} placeholder="morning-digest" required pattern="[a-z0-9][a-z0-9-]*" /></label>
          <label>Schedule (cron)<input bind:value={scheduleExpr} placeholder="@daily or 0 7 * * *" required aria-describedby="expr-help" /></label>
          <p id="expr-help" class="muted help">5-field cron or @hourly/@daily/@weekly/@monthly. The daemon validates it.</p>
          <label>Goal<textarea bind:value={goal} rows="3" placeholder="Summarize yesterday's timeline and list today's priorities." required></textarea></label>
          <div class="row">
            <label>Agent (optional)<input bind:value={agent} placeholder="research-agent" /></label>
            <label>Model (optional)<input bind:value={model} placeholder="provider/model-id" /></label>
          </div>
          <label>Missed fires
            <select bind:value={missedPolicy}>
              <option value="skip">Skip (default) — forget fires missed while offline</option>
              <option value="catch_up">Catch up — fire once on wake</option>
            </select>
          </label>
          <div class="form-actions">
            <button type="button" class="secondary" onclick={() => (showForm = false)}>Cancel</button>
            <button type="submit" class="primary">{editing ? 'Save' : 'Create'}</button>
          </div>
        </form>
      </div>
    </div>
  {/if}

  {#if confirmDelete}
    <div class="modal-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) confirmDelete = null; }}>
      <div class="modal" role="alertdialog" aria-modal="true" aria-label="Delete schedule">
        <h2>Delete “{confirmDelete}”?</h2>
        <p class="muted">The schedule file is removed. Past runs stay in the timeline.</p>
        <div class="form-actions">
          <button class="secondary" onclick={() => (confirmDelete = null)}>Cancel</button>
          <button class="danger" onclick={() => void remove(confirmDelete!)}>Delete</button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .schedules-page { max-width: 56rem; margin: 0 auto; padding: 1.5rem 1rem; display: grid; gap: 1rem; }
  .page-head { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: .75rem; }
  .page-head h1 { margin: 0; font-size: 1.4rem; }
  .empty { text-align: center; padding: 3rem 1rem; display: grid; gap: .5rem; justify-items: center; }
  .sched-list { list-style: none; margin: 0; padding: 0; display: grid; gap: .75rem; }
  .sched-card { border: 1px solid var(--border, #333); border-radius: 10px; padding: .9rem 1rem; display: grid; gap: .45rem; position: relative; }
  .sched-head { display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; }
  .expr { font-size: .78rem; padding: .1rem .45rem; border-radius: 6px; background: var(--bg-inset, #1c1c1c); }
  .goal { margin: 0; }
  .sched-meta { display: flex; gap: 1.5rem; flex-wrap: wrap; margin: 0; font-size: .85rem; }
  .sched-meta div { display: grid; gap: .1rem; }
  .sched-meta dt { color: var(--tx-muted, #888); font-size: .72rem; text-transform: uppercase; letter-spacing: .04em; }
  .sched-meta dd { margin: 0; }
  .sched-actions { position: absolute; top: .6rem; right: .7rem; display: flex; gap: .25rem; }
  .modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: grid; place-items: center; z-index: 50; }
  .modal { background: var(--bg-panel, #1a1a1a); border: 1px solid var(--border, #333); border-radius: 12px; padding: 1.25rem 1.5rem; width: min(34rem, 92vw); max-height: 88vh; overflow-y: auto; display: grid; gap: .75rem; }
  .modal h2 { margin: 0; font-size: 1.1rem; }
  .modal form { display: grid; gap: .6rem; }
  .modal label { display: grid; gap: .25rem; font-size: .85rem; }
  .row { display: grid; grid-template-columns: 1fr 1fr; gap: .6rem; }
  .help { font-size: .75rem; margin: -.3rem 0 0; }
  .form-error { display: flex; gap: .4rem; align-items: center; color: var(--tx-danger, #e5484d); margin: 0; font-size: .85rem; }
  .form-actions { display: flex; justify-content: flex-end; gap: .5rem; }
</style>
