<script lang="ts">
  import { X, FolderOpen, Layers, Users, ListTree, Microscope } from '@lucide/svelte';
  import { getWard, type Ward } from '../shared/api/client';
  import type { Sessions } from '../entities/session/session.svelte';
  import WorkSteps from '../features/chat-stream/WorkSteps.svelte';
  import { createTimeline } from '../shared/sse/timeline.svelte';
  import { analysisText, fmtTokens, isAnalysisKind } from '../entities/timeline-event/analysisText';
  import { fmtDuration } from '../entities/timeline-event/duration';

  // FSD note: the app layer composes features — importing the step view
  // here is app → features (allowed), not a cross-feature import.
  let { sessions, onclose }: { sessions: Sessions; onclose: () => void } = $props();

  // Tabs: Work (the tool history — the live one, default for research)
  // and Context (ward, skills, agents).
  type Tab = 'work' | 'context';
  let tab = $state<Tab>('work');
  // Switching to a research session lands on Work; chat has none.
  $effect(() => {
    tab = sessions.isResearch ? 'work' : 'context';
  });
  const running = $derived(sessions.isRunning(sessions.activeId));
  // The session's Timeline (SSE with durable replay): the Work tab
  // renders ward transitions (📁 entered / created / switched) —
  // hosting changes are events, not transcript rows, so they live only
  // here. The stream stays connected after a run completes (the replay
  // keeps the transitions visible; it only drops on session switch).
  const live = createTimeline();
  $effect(() => {
    const id = sessions.activeId;
    live.connect(id);
    return () => live.close();
  });
  const wardLines = $derived.by(() => {
    const out: { id: string; text: string }[] = [];
    for (const ev of live.events) {
      if (ev.kind === 'ward_entered' || ev.kind === 'ward_created') out.push({ id: ev.id, text: ev.text });
    }
    return out;
  });
  // Delegations as cards, live from the stream: one per child execution
  // (started carries agent + task; completed adds tokens, time, status).
  const delegations = $derived.by<DelegationCard[]>(() => {
    const byExec = new Map<string, DelegationCard>();
    for (const ev of live.events) {
      const d = ev.delegation;
      if (!d || !d.execution_id) continue;
      const card = byExec.get(d.execution_id) ?? { execId: d.execution_id, agent: d.child_agent, task: d.task ?? '', status: 'running', tokensIn: 0, tokensOut: 0, durationMs: 0 };
      if (ev.kind === 'delegation_started') {
        card.agent = d.child_agent;
        card.task = d.task ?? card.task;
      } else if (ev.kind === 'delegation_completed') {
        card.status = d.status || 'completed';
        card.tokensIn = d.tokens_in ?? 0;
        card.tokensOut = d.tokens_out ?? 0;
        card.durationMs = d.duration_ms ?? 0;
      }
      byExec.set(d.execution_id, card);
    }
    // Replay shows the whole trail; the state surface fills what the
    // stream has not (a session opened after its runs).
    for (const e of sessions.executions) {
      if (e.role !== 'delegated') continue;
      if (!byExec.has(e.id)) byExec.set(e.id, { execId: e.id, agent: e.agent, task: '', status: e.status === 'running' ? 'running' : e.status, tokensIn: e.tokens_in, tokensOut: e.tokens_out, durationMs: e.started_at && e.finished_at ? Date.parse(e.finished_at) - Date.parse(e.started_at) : 0 });
    }
    return [...byExec.values()];
  });
  // Each delegation's tool mix, derived from the transcript rows that
  // carry the child's execution id.
  const toolMix = $derived.by(() => {
    const out = new Map<string, Map<string, number>>();
    for (const m of sessions.messages) {
      if (m.role !== 'tool' || !m.tool_name || !m.execution_id || !m.agent || m.agent === 'root') continue;
      const counts = out.get(m.execution_id) ?? new Map<string, number>();
      counts.set(m.tool_name, (counts.get(m.tool_name) ?? 0) + 1);
      out.set(m.execution_id, counts);
    }
    const rendered = new Map<string, string>();
    for (const [exec, counts] of out) {
      rendered.set(exec, [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([t, n]) => `${t.replace(/^mcp_.*_/, '')} ×${n}`).join(' · '));
    }
    return rendered;
  });
  // The run footer: every execution's cost (root, analyst, children).
  const runCost = $derived.by(() => {
    const ex = sessions.executions;
    const sum = (k: 'tokens_in' | 'tokens_out') => ex.reduce((n, e) => n + (e[k] ?? 0), 0);
    const started = ex.length ? Math.min(...ex.map((e) => Date.parse(e.started_at))) : NaN;
    const finished = ex.filter((e) => e.finished_at).map((e) => Date.parse(e.finished_at ?? ''));
    const elapsed = Number.isNaN(started) || !finished.length ? 0 : Math.max(...finished) - started;
    return { tokensIn: sum('tokens_in'), tokensOut: sum('tokens_out'), delegations: ex.filter((e) => e.role === 'delegated').length, elapsedMs: elapsed, count: ex.length };
  });
  type DelegationCard = { execId: string; agent: string; task: string; status: string; tokensIn: number; tokensOut: number; durationMs: number };
  // The intent analyst's trail (analysis_started → one terminal event,
  // with the decision's cost) — the run's first observable steps.
  const analysisLines = $derived.by(() => {
    const out: { id: string; kind: string; text: string }[] = [];
    for (const ev of live.events) {
      if (!isAnalysisKind(ev.kind)) continue;
      const text = analysisText(ev);
      if (text) out.push({ id: ev.id, kind: ev.kind, text });
    }
    return out;
  });
  // The live turn's latest intent row (auto-opens in the step view).
  const liveTurnId = $derived.by<string | undefined>(() => {
    if (!running) return undefined;
    for (let i = sessions.messages.length - 1; i >= 0; i--) {
      const m = sessions.messages[i];
      if (m.role === 'assistant' && m.tool_calls) return m.id;
    }
    return undefined;
  });

  let wardDetail = $state<Ward | null>(null);
  let wardLoadError = $state('');
  let wardGeneration = 0;

  // The active session's ward binding drives the ward card.
  const ward = $derived(sessions.activeWard);
  $effect(() => {
    const name = ward;
    wardDetail = null;
    wardLoadError = '';
    if (!name) return;
    // Generation guard: only the LATEST ward's response may land — a
    // slow earlier fetch must not overwrite the active card.
    const mine = ++wardGeneration;
    getWard(name)
      .then((w) => {
        if (mine !== wardGeneration) return;
        if (w) wardDetail = w;
        else wardLoadError = `Ward ${name} is no longer in the vault.`;
      })
      .catch(() => {
        if (mine !== wardGeneration) return;
        wardLoadError = 'Ward detail unavailable.';
      });
  });

  // Provisioned skills come from the SESSION STATE surface (the latest
  // intent analysis); the delegation roster derives from the transcript
  // (delegate tool-call intents name the agents that actually ran).
  const analysis = $derived<Record<string, any> | null>(
    sessions.analysis as Record<string, any> | null,
  );
  const skills = $derived<string[]>((analysis?.recommended_skills as string[]) ?? []);
  const agents = $derived.by<{ agent: string; task: string }[]>(() => {
    const out: { agent: string; task: string }[] = [];
    for (const m of sessions.messages) {
      if (m.role !== 'assistant' || !m.tool_calls) continue;
      try {
        const calls = JSON.parse(m.tool_calls) as Array<{ function?: { name?: string; arguments?: string } }>;
        for (const c of calls) {
          if (c.function?.name !== 'delegate_agent') continue;
          try {
            const args = JSON.parse(c.function.arguments ?? '{}') as { agent?: string; task?: string };
            if (args.agent) out.push({ agent: args.agent, task: args.task ?? '' });
          } catch { /* skip malformed */ }
        }
      } catch { /* skip malformed */ }
    }
    return out;
  });

  // Work turns: rows split at USER rows; a user row right after a
  // delegate intent is the child's injected context (absorbed).
  type WorkTurn = { key: string; user: import('../shared/api/client').ChatMessage | null; rows: import('../shared/api/client').ChatMessage[]; live: boolean };
  const workTurns = $derived.by<WorkTurn[]>(() => {
    const out: WorkTurn[] = [];
    let cur: WorkTurn | null = null;
    for (const m of sessions.messages) {
      if (m.role === 'user') {
        const prevIntent = cur?.rows[cur.rows.length - 1];
        // An attributed child's injected context absorbs into the current
        // turn whatever precedes it; legacy rows keep the adjacency rule.
        const isChildContext = !!m.agent && m.agent !== 'root';
        if (cur && (isChildContext || (prevIntent?.tool_calls && prevIntent.tool_calls.includes('"delegate_agent"')))) {
          cur.rows.push(m);
          continue;
        }
        cur = { key: m.id, user: m, rows: [], live: false };
        out.push(cur);
        continue;
      }
      if (!cur) continue;
      if (m.role === 'assistant' && !m.tool_calls && (m.content || '').trim() !== '') continue; // the ANSWER stays in the chat
      cur.rows.push(m);
    }
    if (out.length) out[out.length - 1].live = running;
    return out;
  });

  /** Render the ward's layout node ids as a compact tree list. */
  function layoutLines(node: unknown, depth: number, out: string[]): void {
    if (!node || typeof node !== 'object' || out.length > 24) return;
    const n = node as { id?: string; kind?: string; children?: unknown[] };
    const label = n.id ?? (n.kind === 'directory' ? '·' : 'file');
    out.push(`${'  '.repeat(depth)}${n.kind === 'directory' ? '▸' : '·'} ${label}`);
    for (const child of n.children ?? []) layoutLines(child, depth + 1, out);
  }
  const layout = $derived.by(() => {
    if (!wardDetail?.layout) return [];
    const out: string[] = [];
    layoutLines(wardDetail.layout, 0, out);
    return out;
  });
</script>

<aside class="context" aria-label="Session context">
  <header>
    <div class="tabs" role="tablist" aria-label="Session panel">
      <button role="tab" aria-selected={tab === 'work'} class:active={tab === 'work'} onclick={() => (tab = 'work')}>
        <ListTree size={13} aria-hidden="true" /> Work
        {#if running}<span class="live-dot" aria-label="run in progress"></span>{/if}
      </button>
      <button role="tab" aria-selected={tab === 'context'} class:active={tab === 'context'} onclick={() => (tab = 'context')}>Context</button>
    </div>
    <button class="icon-button" onclick={onclose} aria-label="Close right sidebar"><X size={18} /></button>
  </header>
  {#if tab === 'work'}
    <!-- The tool history: every turn's steps (friendly verbs, folded
         detail), grouped under the user message that started them. -->
    <div class="context-body work" role="tabpanel" aria-label="Work">
      {#if sessions.messages.length === 0 && !running}
        <p class="muted small">No work yet — send a research question and the steps appear here as the run works.</p>
      {:else}
        {#each workTurns as turn (turn.key)}
          <section class="work-turn" aria-label="Turn">
            {#if turn.user}<p class="turn-user" title={turn.user.content}>{turn.user.content}</p>{/if}
            <WorkSteps messages={turn.rows} liveTurnId={turn.live ? liveTurnId : undefined} />
          </section>
        {/each}
        {#if analysisLines.length}
          <section class="work-turn" aria-label="Task analysis">
            {#each analysisLines as a (a.id)}
              <p class="ward-line analysis-line" class:failed={a.kind === 'analysis_failed'}><Microscope size={12} aria-hidden="true" /> {a.text}</p>
            {/each}
          </section>
        {/if}
        {#if delegations.length}
          <section class="work-turn" aria-label="Delegations">
            <p class="ctx-title"><Users size={12} aria-hidden="true" /> Delegations</p>
            {#each delegations as d (d.execId)}
              <div class="delegation-card" class:running={d.status === 'running'} class:failed={d.status === 'failed' || d.status === 'cancelled'}>
                <span class="del-agent">{d.agent}</span>
                {#if d.task}<span class="del-task" title={d.task}>{d.task.slice(0, 90)}{d.task.length > 90 ? '…' : ''}</span>{/if}
                <span class="del-cost">
                  {#if d.status === 'running'}<span class="pulse" aria-hidden="true"></span>{/if}
                  <!-- The changing outcome is announced once per card, with its agent. -->
                  <span role="status" aria-live="polite">{d.agent}: {d.status}</span>
                  {#if d.tokensIn || d.tokensOut} · {fmtTokens(d.tokensIn)} → {fmtTokens(d.tokensOut)}{/if}
                  {#if d.durationMs} · {fmtDuration(d.durationMs)}{/if}
                </span>
                {#if toolMix.get(d.execId)}<span class="del-tools">{toolMix.get(d.execId)}</span>{/if}
              </div>
            {/each}
          </section>
        {/if}
        {#if wardLines.length}
          <section class="work-turn" aria-label="Ward transitions">
            {#each wardLines as w (w.id)}
              <p class="ward-line"><FolderOpen size={12} aria-hidden="true" /> {w.text}</p>
            {/each}
          </section>
        {/if}
        {#if runCost.count > 1 || runCost.tokensIn}
          <footer class="run-foot" aria-label="Run cost">
            <span>Run: {fmtTokens(runCost.tokensIn)} prompt · {fmtTokens(runCost.tokensOut)} completion tokens{runCost.delegations ? ` · ${runCost.delegations} delegation${runCost.delegations > 1 ? 's' : ''}` : ''}{runCost.elapsedMs ? ` · ${fmtDuration(runCost.elapsedMs)}` : ''}</span>
            <details class="run-breakdown">
              <summary>Per execution</summary>
              <ul>
                {#each sessions.executions as e (e.id)}
                  <li><span class="be-role">{e.role}</span> {e.agent} · {fmtTokens(e.tokens_in)} → {fmtTokens(e.tokens_out)}{e.started_at && e.finished_at ? ` · ${fmtDuration(Date.parse(e.finished_at) - Date.parse(e.started_at))}` : ''} · {e.status}</li>
                {/each}
              </ul>
            </details>
          </footer>
        {/if}
      {/if}
    </div>
  {:else}
  <div class="context-body" role="tabpanel" aria-label="Context">
    {#if ward}
      <section class="ctx-section" aria-label="Active ward">
        <p class="ctx-title"><FolderOpen size={12} aria-hidden="true" /> Active Ward</p>
        {#if wardDetail}
          <div class="ward-card">
            <p class="name">{wardDetail.name}</p>
            <p class="type">{wardDetail.archetype} archetype · lint {wardDetail.lint_status}</p>
            {#if wardDetail.description}
              <p class="desc">{wardDetail.description}</p>
            {/if}
            {#if wardDetail.last_used}
              <p class="type">last used {new Date(wardDetail.last_used).toLocaleString()}</p>
            {/if}
            <p class="type orchestrator">
              {#if wardDetail.orchestrator}
                orchestrates on <strong>{wardDetail.orchestrator}</strong>
              {:else}
                orchestrates on the daemon default
              {/if}
            </p>
            {#if wardDetail.canonical_title}
              <p class="canonical">{wardDetail.canonical_title}</p>
            {/if}
            {#if layout.length}
              <div class="ward-files" aria-label="Ward layout">
                {#each layout as line}
                  <code>{line}</code>
                {/each}
              </div>
            {/if}
          </div>
        {:else if wardLoadError}
          <p class="muted small">{wardLoadError}</p>
        {:else}
          <p class="muted small">Loading ward detail…</p>
        {/if}
      </section>
    {:else if sessions.isResearch}
      <section class="ctx-section" aria-label="Active ward">
        <p class="ctx-title"><FolderOpen size={12} aria-hidden="true" /> Active Ward</p>
        <p class="muted small">No ward yet — this session is a shell. The model enters a ward via ward_use when the research calls for it.</p>
      </section>
    {/if}

    <section class="ctx-section" aria-label="Provisioned skills">
      <p class="ctx-title"><Layers size={12} aria-hidden="true" /> Provisioned Skills</p>
      {#if skills.length}
        {#each skills as s}
          <p class="ctx-row"><span class="dot skill" aria-hidden="true"></span> {s}</p>
        {/each}
      {:else}
        <p class="muted small">{sessions.isResearch ? 'Skills provision on the first exchange (intent analysis selects them from the packet).' : 'Quick chat provisions no skills — it runs lean.'}</p>
      {/if}
    </section>

    <section class="ctx-section" aria-label="Agents">
      <p class="ctx-title"><Users size={12} aria-hidden="true" /> Agents</p>
      {#if agents.length}
        {#each agents as a}
          <p class="ctx-row"><span class="dot agent" aria-hidden="true"></span> {a.agent}{#if a.task}<span class="muted"> — {a.task}</span>{/if}</p>
        {/each}
      {:else}
        <p class="muted small">{sessions.isResearch ? 'No delegations yet in this session.' : 'Delegation is a research-session capability.'}</p>
      {/if}
    </section>
  </div>
  {/if}
  <footer>Local by design. Yours to control.</footer>
</aside>

<style>
  .context { height: 100%; display: flex; flex-direction: column; border-left: 1px solid var(--ds-color-border); background: var(--ds-color-background); }
  .tabs { display: flex; gap: var(--ds-space-1); }
  .tabs [role="tab"] { display: inline-flex; align-items: center; gap: .3rem; padding: var(--ds-space-2); font-size: var(--ds-font-nav); min-height: 32px; color: var(--ds-color-muted); border-radius: 6px; }
  .tabs [role="tab"].active { color: var(--ds-color-text); background: var(--ds-color-hover); }
  .live-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--ds-color-success, #22c55e); animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: .35; } 50% { opacity: 1; } }
  .context-body.work { padding: var(--ds-space-3); gap: var(--ds-space-4); }
  .work-turn { display: grid; gap: .25rem; }
  .delegation-card { display: grid; grid-template-columns: max-content 1fr max-content; gap: .15rem .6rem; align-items: baseline; padding: .4rem .6rem; margin: .2rem 0; border: 1px solid color-mix(in oklab, var(--ds-color-primary, #3b82f6) 45%, transparent); border-radius: 8px; background: var(--ds-color-surface, #1e1e1e); font-size: .78rem; }
  .delegation-card .del-agent { font-weight: 600; }
  .delegation-card .del-task { color: var(--ds-color-muted, #888); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .delegation-card .del-cost { color: var(--ds-color-muted, #888); font-variant-numeric: tabular-nums; white-space: nowrap; }
  .delegation-card.failed .del-cost { color: var(--ds-color-danger, #ef4444); }
  .delegation-card .pulse { display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: var(--ds-color-success, #22c55e); margin-right: .3rem; }
  .delegation-card .del-tools { grid-column: 1 / -1; color: var(--ds-color-muted, #888); font-size: .7rem; }
  .run-foot { margin-top: .6rem; padding-top: .5rem; border-top: 1px solid var(--ds-color-border, #333); color: var(--ds-color-muted, #888); font-size: .74rem; font-variant-numeric: tabular-nums; }
  .run-breakdown { margin-top: .3rem; }
  .run-breakdown summary { cursor: pointer; }
  .run-breakdown ul { margin: .3rem 0 0; padding-left: 1.1rem; display: grid; gap: .1rem; }
  .run-breakdown .be-role { display: inline-block; min-width: 4.2rem; text-transform: capitalize; }

  .ward-line { display: flex; align-items: center; gap: .35rem; margin: 0; font-size: .76rem; color: var(--ds-color-success, #22c55e); }
  .turn-user { margin: 0 0 .15rem; font-size: .72rem; color: var(--ds-color-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; border-left: 2px solid var(--ds-color-primary, #6366f1); padding-left: .4rem; }
  header { height: 56px; display: flex; align-items: center; justify-content: space-between; padding: var(--ds-space-2) var(--ds-space-3); border-bottom: 1px solid var(--ds-color-border); }
  .context-body { flex: 1; min-height: 0; overflow-y: auto; padding: var(--ds-space-4); display: flex; flex-direction: column; gap: var(--ds-space-6); color: var(--ds-color-muted); font-size: var(--ds-font-control); line-height: 1.7; }
  .ctx-section { display: grid; gap: var(--ds-space-2); align-content: start; }
  .ctx-title { display: flex; align-items: center; gap: var(--ds-space-2); margin: 0; font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.07em; color: var(--ds-color-muted); }
  .ward-card { border: 1px solid color-mix(in oklch, var(--ds-color-success, #22c55e) 25%, transparent); border-radius: 10px; padding: var(--ds-space-3); background: var(--ds-color-surface, #1e1e1e); display: grid; gap: var(--ds-space-2); }
  .ward-card .name { margin: 0; font-weight: 600; color: var(--ds-color-success, #22c55e); }
  .ward-card .type { margin: 0; font-size: 0.72rem; color: var(--ds-color-muted); }
  .ward-card .desc { margin: 0; font-size: 0.76rem; color: var(--ds-color-text); }
  .ward-card .orchestrator { color: var(--ds-color-text); }
  .ward-card .orchestrator strong { font-weight: 500; color: var(--ds-color-primary, #6366f1); }
  .ward-card .canonical { margin: 0; font-size: 0.78rem; padding: var(--ds-space-2); border-left: 2px solid var(--ds-color-success, #22c55e); border-radius: 4px; background: var(--ds-color-field, #1a1a1a); color: var(--ds-color-text); }
  .ward-files { display: grid; gap: 2px; font-size: 0.7rem; }
  .ward-files code { font-family: ui-monospace, monospace; white-space: pre; color: var(--ds-color-muted); }
  .ctx-row { display: flex; align-items: center; gap: var(--ds-space-2); margin: 0; padding: var(--ds-space-1) var(--ds-space-2); border: 1px solid var(--ds-color-border); border-radius: 6px; background: var(--ds-color-surface, #1e1e1e); font-size: 0.78rem; }
  .dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
  .dot.skill { background: var(--ds-color-primary, #6366f1); }
  .dot.agent { background: var(--ds-color-success, #22c55e); }
  .muted { color: var(--ds-color-muted); }
  .small { font-size: 0.75rem; margin: 0; }
  footer { padding: var(--ds-space-4); font-size: 0.6875rem; color: var(--ds-color-muted); }
</style>
