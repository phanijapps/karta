import { test, expect, type Page } from '@playwright/test';

// The shell's base routes (models, the chat default model) — the same
// stubs sessions.spec.ts uses, inlined (test files cannot import each
// other).
export async function mockBase(page: Page) {
  await page.route((url) => url.pathname === '/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ provider: 'zai', id: 'glm-5.3' }], refreshed_at: { zai: NOW } }) }),
  );
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }),
  );
}

// Run legibility (docs/architecture/run-legibility-plan.md): the thread
// separates sub-agent reports from THE answer, the answer exposes the
// model's reasoning and its narration, and the Work tab attributes
// steps, shows delegation cards and totals the run. Route-mocked daemon.

const NOW = new Date().toISOString();

/** A research session whose run delegated twice: the transcript carries
 *  agent + execution attribution on every row (as the daemon now
 *  writes them). */
const INR = 'r-inr';
const CHILD_RESEARCH = 'exec-child-research-agent-1';
const CHILD_BUILDER = 'exec-child-builder-agent-2';
const TRANSCRIPT = [
  { id: 'u1', seq: 1, role: 'user', agent: 'root', execution_id: 'exec-root-r-inr', content: 'Research the rupee… and write a briefing with charts.', created_at: NOW },
  { id: 'a1', seq: 2, role: 'assistant', agent: 'root', execution_id: 'exec-root-r-inr', content: 'Delegating the research first.', tool_calls: JSON.stringify([{ id: 'c1', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'research-agent', task: 'research INR pressures' }) } }]), created_at: NOW },
  { id: 'u2', seq: 3, role: 'user', agent: 'research-agent', execution_id: CHILD_RESEARCH, content: 'research INR pressures', created_at: NOW },
  { id: 'r1', seq: 4, role: 'assistant', agent: 'research-agent', execution_id: CHILD_RESEARCH, content: 'REPORT '.repeat(600), reasoning: 'I will rank the drivers first.', created_at: NOW },
  { id: 't1', seq: 5, role: 'tool', agent: 'root', execution_id: 'exec-root-r-inr', tool_name: 'delegate_agent', tool_call_id: 'c1', content: '**Pressures on the Indian Rupee** — oil imports, FPI outflows, trade deficit, RBI intervention, Fed policy. Full report follows…', created_at: NOW },
  { id: 'a2', seq: 6, role: 'assistant', agent: 'root', execution_id: 'exec-root-r-inr', content: '', tool_calls: JSON.stringify([{ id: 'c2', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'builder-agent', task: 'build charts' }) } }]), created_at: NOW },
  { id: 'u3', seq: 7, role: 'user', agent: 'builder-agent', execution_id: CHILD_BUILDER, content: 'build charts', created_at: NOW },
  { id: 'a3', seq: 8, role: 'assistant', agent: 'builder-agent', execution_id: CHILD_BUILDER, content: '', reasoning: 'Need the series first.', tool_calls: JSON.stringify([{ id: 'c3', function: { name: 'read', arguments: JSON.stringify({ path: 'reports/data.csv' }) } }, { id: 'c4', function: { name: 'write', arguments: JSON.stringify({ path: 'reports/chart.png' }) } }]), created_at: NOW },
  { id: 't2', seq: 9, role: 'tool', agent: 'builder-agent', execution_id: CHILD_BUILDER, tool_name: 'read', tool_call_id: 'c3', content: 'date,rate\n…', created_at: NOW },
  { id: 't3', seq: 10, role: 'tool', agent: 'builder-agent', execution_id: CHILD_BUILDER, tool_name: 'write', tool_call_id: 'c4', content: 'wrote 12 bytes', created_at: NOW },
  { id: 'r2', seq: 11, role: 'assistant', agent: 'builder-agent', execution_id: CHILD_BUILDER, content: 'Built two charts: USDINR_12m.png and Peer_comparison.png.', created_at: NOW },
  { id: 't4', seq: 12, role: 'tool', agent: 'root', execution_id: 'exec-root-r-inr', tool_name: 'delegate_agent', tool_call_id: 'c2', content: 'Built two charts…', created_at: NOW },
  { id: 'a4', seq: 13, role: 'assistant', agent: 'root', execution_id: 'exec-root-r-inr', content: 'Verified the intervention figures against three sources, then wrote the brief around the builder\'s charts.', tool_calls: JSON.stringify([{ id: 'c5', function: { name: 'write', arguments: JSON.stringify({ path: 'reports/brief.md' }) } }]), created_at: NOW },
  { id: 't5', seq: 14, role: 'tool', agent: 'root', execution_id: 'exec-root-r-inr', tool_name: 'write', tool_call_id: 'c5', content: 'wrote 2366 bytes', created_at: NOW },
  { id: 'a5', seq: 15, role: 'assistant', agent: 'root', execution_id: 'exec-root-r-inr', reasoning: 'The report ranks oil and FPI flows first; I will lead with the ranked drivers and flag the reserve-figure divergence.', content: '**Pressures on the Indian Rupee.** The rupee depreciated 6.4% in two legs.\n\n- Oil import costs — the largest driver.\n- Foreign portfolio flows — $14bn of outflows.', created_at: NOW },
];

const EXECUTIONS = [
  { id: 'exec-root-r-inr', agent: 'root', role: 'root', status: 'completed', tokens_in: 721214, tokens_out: 36832, started_at: '2026-11-25T20:00:00Z', finished_at: '2026-11-25T20:14:00Z' },
  { id: 'exec-intent-1', agent: 'intent-analyst', role: 'analyst', status: 'completed', tokens_in: 1911, tokens_out: 317, started_at: '2026-11-25T20:00:01Z', finished_at: '2026-11-25T20:00:05Z' },
  { id: CHILD_RESEARCH, parent_id: 'exec-root-r-inr', agent: 'research-agent', role: 'delegated', status: 'completed', tokens_in: 1102, tokens_out: 7961, started_at: '2026-11-25T20:01:00Z', finished_at: '2026-11-25T20:02:24Z' },
  { id: CHILD_BUILDER, parent_id: 'exec-root-r-inr', agent: 'builder-agent', role: 'delegated', status: 'completed', tokens_in: 284089, tokens_out: 16814, started_at: '2026-11-25T20:03:00Z', finished_at: '2026-11-25T20:09:12Z' },
];

export async function mockDaemon(page: Page) {
  await page.route('**/api/sessions?limit=100', (route) => route.fulfill({
    status: 200, contentType: 'application/json',
    body: JSON.stringify({ sessions: [
      { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
      { id: INR, type: 'research', status: 'completed', ward: 'stock-analysis', title: 'INR vs USD pressures', updated_at: '2026-11-25T20:14:00Z' },
      { id: 'r-inter', type: 'research', status: 'completed', ward: 'stock-analysis', title: 'Interleaved children', updated_at: '2026-11-22T20:14:00Z' },
      { id: 'r-old', type: 'research', status: 'completed', ward: 'stock-analysis', title: 'Older run', updated_at: '2026-11-20T20:14:00Z' },
    ], truncated: false }),
  }));
  await page.route('**/api/wards', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ wards: [] }) }));
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const u = new URL(route.request().url());
    const sid = u.searchParams.get('session_id') ?? 'default';
    const after = Number(u.searchParams.get('after_seq') ?? '0');
    const rows = sid === INR ? TRANSCRIPT.filter((m) => m.seq > after) : [];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: rows, next_cursor: rows.length ? rows[rows.length - 1].seq : after, truncated: false }) });
  });
  await page.route((url) => /^\/api\/sessions\/[^/]+\/state$/.test(url.pathname), (route) => {
    const sid = new URL(route.request().url()).pathname.split('/')[3];
    const body: Record<string, unknown> = { session: { id: sid, type: sid === 'default' ? 'chat' : 'research', status: 'completed', ward: sid === 'default' ? undefined : 'stock-analysis', updated_at: NOW } };
    if (sid === INR) {
      body.intent_analysis = { status: 'analyzed', primary_intent: 'analyze-inr-usd-pressures', complexity: 'M', step_budget: 8, approach: 'graph', ward_name: 'stock-analysis' };
      body.executions = EXECUTIONS;
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
  // The live stream replays the delegation lifecycle (durable replay).
  await page.route((url) => url.pathname === `/api/sessions/${INR}/events`, (route) => {
    const frames = [
      'event: analysis_completed\ndata: ' + JSON.stringify({ id: 'e0', kind: 'analysis_completed', agent: 'intent-analyst', execution_id: 'exec-intent-1', text: 'Task Analysis: analyze-inr-usd-pressures', intent_analysis: { status: 'analyzed', primary_intent: 'analyze-inr-usd-pressures', complexity: 'M', step_budget: 8 } }),
      'event: delegation_started\ndata: ' + JSON.stringify({ id: 'e1', kind: 'delegation_started', agent: 'research-agent', execution_id: CHILD_RESEARCH, text: 'Delegated to research-agent: research INR pressures', delegation: { child_agent: 'research-agent', task: 'research INR pressures', execution_id: CHILD_RESEARCH } }),
      'event: delegation_completed\ndata: ' + JSON.stringify({ id: 'e2', kind: 'delegation_completed', agent: 'research-agent', execution_id: CHILD_RESEARCH, text: 'research-agent finished (completed)', delegation: { child_agent: 'research-agent', task: 'research INR pressures', execution_id: CHILD_RESEARCH, tokens_in: 1102, tokens_out: 7961, duration_ms: 84000, status: 'completed' } }),
      'event: delegation_started\ndata: ' + JSON.stringify({ id: 'e3', kind: 'delegation_started', agent: 'builder-agent', execution_id: CHILD_BUILDER, text: 'Delegated to builder-agent: build charts', delegation: { child_agent: 'builder-agent', task: 'build charts', execution_id: CHILD_BUILDER } }),
      'event: delegation_completed\ndata: ' + JSON.stringify({ id: 'e4', kind: 'delegation_completed', agent: 'builder-agent', execution_id: CHILD_BUILDER, text: 'builder-agent finished (completed)', delegation: { child_agent: 'builder-agent', task: 'build charts', execution_id: CHILD_BUILDER, tokens_in: 284089, tokens_out: 16814, duration_ms: 372000, status: 'completed' } }),
    ].join('\n\n') + '\n\n';
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body: frames });
  });
}

export async function openInrSession(page: Page) {
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  // Selecting the ward activates its LAST EXECUTED session — the INR run.
  await nav.locator('.ward-row', { hasText: 'stock-analysis' }).click({ force: true });
  await expect(page.locator('.analysis-chip')).toContainText('analyze-inr-usd-pressures');
}

test('delegations are pills; the slideout shows the agent\'s calls and report; the answer is the only answer-styled block', async ({ page }) => {
  await mockBase(page);
  await mockDaemon(page);
  await openInrSession(page);
  const pills = page.locator('.delegation-pill');
  await expect(pills).toHaveCount(2);
  await expect(pills.nth(0)).toContainText('research-agent');
  await expect(pills.nth(1)).toContainText('builder-agent');
  // Exactly ONE answer-styled root block, tagged as the answer, with the
  // narration before it labelled as narration.
  const answers = page.getByRole('article', { name: 'Final answer' });
  await expect(answers).toHaveCount(1);
  await expect(answers).toContainText('The answer');
  await expect(answers).toContainText('Pressures on the Indian Rupee');
  await expect(page.getByRole('article', { name: 'Assistant message' })).toHaveCount(0);
  await expect(page.locator('.narration')).toContainText('Narration');
  await expect(page.locator('.narration')).toContainText('Verified the intervention figures');
  // A pill opens the slideout: agent, task, cost from the executions,
  // the calls, and the report. Escape closes it with focus back on the pill.
  const research = pills.nth(0);
  await research.click();
  const dialog = page.getByRole('dialog', { name: 'Delegation to research-agent' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('research INR pressures');
  await expect(dialog).toContainText('completed · 1.1k → 8k tokens · 1m 24s');
  await expect(dialog).toContainText('No tool calls');
  await expect(dialog.getByRole('region', { name: 'Report' })).toContainText('Pressures on the Indian Rupee');
  await expect(research).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(research).toBeFocused();
  await expect(research).toHaveAttribute('aria-expanded', 'false');
  // The builder's pill shows its calls (the Work tab's step tree) and cost.
  const builder = pills.nth(1);
  await builder.click();
  const bdialog = page.getByRole('dialog', { name: 'Delegation to builder-agent' });
  await expect(bdialog).toBeVisible();
  await expect(bdialog).toContainText('completed · 284.1k → 16.8k tokens · 6m 12s');
  // The step tree groups the child's read + write into one step; its
  // expansion lists both calls.
  const readStep = bdialog.getByRole('button', { name: /Read/ });
  await expect(readStep).toBeVisible();
  await readStep.click();
  await expect(bdialog).toContainText('reports/data.csv');
  await expect(bdialog).toContainText('reports/chart.png');
  await bdialog.getByRole('button', { name: 'Close delegation' }).click();
  await expect(bdialog).toHaveCount(0);
  await expect(builder).toBeFocused();
  // Modal by behaviour: focus enters the dialog, Tab wraps inside it
  // (last → first, first → last with Shift), and never reaches the page.
  await builder.click();
  await expect(bdialog).toBeVisible();
  await expect(bdialog).toBeFocused();
  const focusables = bdialog.locator('button:visible');
  await focusables.last().focus();
  await page.keyboard.press('Tab');
  await expect(focusables.first()).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(focusables.last()).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(bdialog).toHaveCount(0);
});

test('thinking is the final step\'s reasoning', async ({ page }) => {
  await mockBase(page);
  await mockDaemon(page);
  await openInrSession(page);
  const answer = page.getByRole('article', { name: 'Final answer' });
  const thinking = answer.locator('details.answer-thinking');
  await expect(thinking).not.toHaveAttribute('open', '');
  await expect(thinking.locator('p')).toBeHidden();
  await thinking.locator('summary').click();
  await expect(thinking).toHaveAttribute('open', '');
  await expect(thinking.locator('p')).toBeVisible();
  await expect(thinking.locator('p')).toHaveText('The report ranks oil and FPI flows first; I will lead with the ranked drivers and flag the reserve-figure divergence.');
});

test('the Work tab attributes steps, shows delegation cards with cost, and totals the run', async ({ page }) => {
  await mockBase(page);
  await mockDaemon(page);
  await openInrSession(page);
  if ((await page.getByRole('complementary', { name: 'Session context' }).count()) === 0) {
    await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  }
  const panel = page.getByRole('complementary', { name: 'Session context' });
  await panel.getByRole('tab', { name: /Work/ }).click();
  const work = panel.getByRole('tabpanel', { name: 'Work' });
  // Two separate delegation steps (never merged); expanding one shows its
  // children, each labelled with the agent that took it.
  const delegateSteps = work.locator('.step.delegation');
  await expect(delegateSteps).toHaveCount(2);
  await delegateSteps.nth(1).locator('button.step-row').click();
  await expect(work.locator('.step.child .agent').first()).toContainText('builder-agent');
  // Delegation cards: one per child, with tokens and duration.
  const cards = work.locator('.delegation-card');
  await expect(cards).toHaveCount(2);
  await expect(cards.nth(0)).toContainText('research-agent');
  await expect(cards.nth(0)).toContainText('research INR pressures');
  await expect(cards.nth(0)).toContainText('1.1k → 8k');
  await expect(cards.nth(0)).toContainText('1m 24s');
  await expect(cards.nth(1)).toContainText('builder-agent');
  await expect(cards.nth(1)).toContainText('284.1k → 16.8k');
  // The footer totals every execution: root + analyst + both children.
  const foot = work.locator('footer.run-foot');
  await expect(foot).toContainText('Session: 1M prompt');
  await expect(foot).toContainText('61.9k completion');
  await expect(foot).toContainText('2 delegations');
  await expect(foot).toContainText('14m'); // zero parts drop
  // Tool mix on the delegation card (from the child's attributed rows).
  await expect(cards.nth(1)).toContainText('read ×1 · write ×1');
  // The per-execution breakdown opens on demand.
  const breakdown = work.locator('details.run-breakdown');
  await expect(breakdown.locator('li').first()).toBeHidden();
  await breakdown.locator('summary').click();
  await expect(breakdown).toContainText('analyst intent-analyst · 1.9k → 317 · 4s · completed');
  await expect(breakdown).toContainText('delegated builder-agent · 284.1k → 16.8k · 6m 12s · completed');
});

test('a stale state response never overwrites newer run totals', async ({ page }) => {
  await mockBase(page);
  await mockDaemon(page);
  // The FIRST state fetch for the INR session is slow and stale (no
  // executions); later fetches carry them.
  let inrStateCalls = 0;
  await page.route((url) => url.pathname === `/api/sessions/${INR}/state`, async (route) => {
    inrStateCalls++;
    if (inrStateCalls === 1) {
      await new Promise((r) => setTimeout(r, 600));
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ session: { id: INR, type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW }, intent_analysis: { status: 'analyzed', primary_intent: 'stale', complexity: 'S', step_budget: 4 } }) });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ session: { id: INR, type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW }, intent_analysis: { status: 'analyzed', primary_intent: 'analyze-inr-usd-pressures', complexity: 'M', step_budget: 8 }, executions: EXECUTIONS }) });
  });
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  await nav.locator('.ward-row', { hasText: 'stock-analysis' }).click({ force: true }); // selects INR → stale fetch #1 pending
  await nav.locator('.session.child', { hasText: 'Older run' }).click();
  await nav.locator('.session.child', { hasText: 'INR vs USD' }).click(); // fetch #2 (fresh) lands before #1
  await expect(page.locator('.analysis-chip')).toContainText('analyze-inr-usd-pressures');
  if ((await page.getByRole('complementary', { name: 'Session context' }).count()) === 0) {
    await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  }
  const panel = page.getByRole('complementary', { name: 'Session context' });
  await panel.getByRole('tab', { name: /Work/ }).click();
  const work = panel.getByRole('tabpanel', { name: 'Work' });
  await expect(work.locator('footer.run-foot')).toContainText('Session: 1M prompt');
  // Wait out the stale response: the totals and the analysis must stand.
  await page.waitForTimeout(900);
  await expect(work.locator('footer.run-foot')).toContainText('Session: 1M prompt');
  await expect(page.locator('.analysis-chip')).toContainText('analyze-inr-usd-pressures');
});

test('interleaved children nest under their own delegation by execution, not by order', async ({ page }) => {
  await mockBase(page);
  await mockDaemon(page);
  const SID = 'r-inter';
  const ALPHA = 'exec-child-alpha', BETA = 'exec-child-beta';
  const rows = [
    { id: 'u1', seq: 1, role: 'user', agent: 'root', execution_id: 'exec-root', content: 'two delegations, interleaved', created_at: NOW },
    { id: 'a1', seq: 2, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: '', tool_calls: JSON.stringify([{ id: 'c1', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'alpha', task: 'A' }) } }]), created_at: NOW },
    { id: 'a2', seq: 3, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: '', tool_calls: JSON.stringify([{ id: 'c2', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'beta', task: 'B' }) } }]), created_at: NOW },
    { id: 'ua', seq: 4, role: 'user', agent: 'alpha', execution_id: ALPHA, content: 'A', created_at: NOW },
    { id: 'aa', seq: 5, role: 'assistant', agent: 'alpha', execution_id: ALPHA, content: '', tool_calls: JSON.stringify([{ id: 'c3', function: { name: 'read', arguments: JSON.stringify({ path: 'a.md' }) } }]), created_at: NOW },
    { id: 'ub', seq: 6, role: 'user', agent: 'beta', execution_id: BETA, content: 'B', created_at: NOW },
    { id: 'ab', seq: 7, role: 'assistant', agent: 'beta', execution_id: BETA, content: '', tool_calls: JSON.stringify([{ id: 'c4', function: { name: 'write', arguments: JSON.stringify({ path: 'b.md' }) } }]), created_at: NOW },
    { id: 't3', seq: 8, role: 'tool', agent: 'alpha', execution_id: ALPHA, tool_name: 'read', tool_call_id: 'c3', content: 'a', created_at: NOW },
    { id: 't4', seq: 9, role: 'tool', agent: 'beta', execution_id: BETA, tool_name: 'write', tool_call_id: 'c4', content: 'b', created_at: NOW },
    { id: 't1', seq: 10, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'delegate_agent', tool_call_id: 'c1', content: 'A done', created_at: NOW },
    { id: 't2', seq: 11, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'delegate_agent', tool_call_id: 'c2', content: 'B done', created_at: NOW },
    { id: 'ar', seq: 12, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'read root.txt then conclude', tool_calls: JSON.stringify([{ id: 'c5', function: { name: 'read', arguments: JSON.stringify({ path: 'root.txt' }) } }]), created_at: NOW },
    { id: 't5', seq: 13, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'read', tool_call_id: 'c5', content: 'r', created_at: NOW },
    { id: 'af', seq: 14, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'Done.', created_at: NOW },
  ];
  await page.route((url) => url.pathname === `/api/sessions/${SID}/state`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ session: { id: SID, type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW }, intent_analysis: { status: 'analyzed', primary_intent: 'inter', complexity: 'S', step_budget: 4 } }) }));
  await page.route((url) => url.pathname === `/api/sessions/${SID}/events`, (route) => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const u = new URL(route.request().url());
    const sid = u.searchParams.get('session_id') ?? 'default';
    const after = Number(u.searchParams.get('after_seq') ?? '0');
    const list = sid === SID ? rows.filter((m) => m.seq > after) : [];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: list, next_cursor: list.length ? list[list.length - 1].seq : after, truncated: false }) });
  });
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  await nav.locator('.ward-row', { hasText: 'stock-analysis' }).click({ force: true });
  await nav.locator('.session.child', { hasText: 'Interleaved children' }).click();
  await expect(page.locator('.analysis-chip')).toContainText('inter');
  if ((await page.getByRole('complementary', { name: 'Session context' }).count()) === 0) {
    await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  }
  const panel = page.getByRole('complementary', { name: 'Session context' });
  await panel.getByRole('tab', { name: /Work/ }).click();
  const work = panel.getByRole('tabpanel', { name: 'Work' });
  const delegateSteps = work.locator('.step.delegation');
  await expect(delegateSteps).toHaveCount(2);
  // alpha's step (which arrived AFTER both delegations started) nests
  // under alpha's delegation; beta's under beta's; the root's step that
  // ran while delegations were open stays top-level.
  await delegateSteps.nth(0).locator('button.step-row').click();
  const alphaChildren = delegateSteps.nth(0).locator('.step.child');
  await expect(alphaChildren).toHaveCount(1);
  await expect(alphaChildren.first()).toContainText('Read');
  await expect(alphaChildren.first()).toContainText('alpha');
  await delegateSteps.nth(1).locator('button.step-row').click();
  const betaChildren = delegateSteps.nth(1).locator('.step.child');
  await expect(betaChildren).toHaveCount(1);
  await expect(betaChildren.first()).toContainText('Wrote');
  await expect(betaChildren.first()).toContainText('beta');
  await expect(work.locator('.step.delegation > .step-child-wrapper .step.child')).toHaveCount(0); // no child leaks out
  // The root's own read after the delegations is a top-level step.
  await expect(work.locator('.step:not(.delegation):not(.child) .verb', { hasText: 'Read' })).toHaveCount(1);
});

test('a legacy run with two delegations in one step and a report between intent and result stays ONE turn with one answer', async ({ page }) => {
  // Mirrors the live INR transcript's shape (pre-attribution): the root
  // issued two delegate_agent calls in ONE assistant row; the research
  // agent's 26 KB report sits between its intent and its result; the
  // builder's context follows. Before the fix the turn split at the
  // builder's context, the report was tagged "The answer", and the
  // builder pill showed "running" forever.
  await mockBase(page);
  await mockDaemon(page);
  const SID = 'r-legacy-shape';
  const rows = [
    { id: 'u1', seq: 1, role: 'user', agent: 'root', execution_id: 'exec-root', content: 'Research the rupee…', created_at: NOW },
    { id: 'a1', seq: 2, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'Delegating both streams in parallel.', tool_calls: JSON.stringify([{ id: 'c1', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'research-agent', task: 'Ward: x (you are hosted inside it; all paths relative). TASK: research the pressures' }) } }, { id: 'c2', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'builder-agent', task: 'build the charts' }) } }]), created_at: NOW },
    { id: 'u2', seq: 3, role: 'user', agent: 'root', execution_id: 'exec-root', content: 'Ward: x (you are hosted inside it). TASK: research…', created_at: NOW },
    { id: 'r1', seq: 4, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: '**Research report** — the drivers ranked. '.repeat(300), created_at: NOW },
    { id: 't1', seq: 5, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'delegate_agent', tool_call_id: 'c1', content: 'report received', created_at: NOW },
    { id: 'u3', seq: 6, role: 'user', agent: 'root', execution_id: 'exec-root', content: 'build the charts', created_at: NOW },
    { id: 'a2', seq: 7, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: '', tool_calls: JSON.stringify([{ id: 'c3', function: { name: 'write', arguments: JSON.stringify({ path: 'chart.png' }) } }]), created_at: NOW },
    { id: 't2', seq: 8, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'write', tool_call_id: 'c3', content: 'wrote', created_at: NOW },
    { id: 'r2', seq: 9, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'Built the charts.', created_at: NOW },
    { id: 't3', seq: 10, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'delegate_agent', tool_call_id: 'c2', content: 'Built the charts.', created_at: NOW },
    { id: 'a3', seq: 11, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'Writing the brief.', tool_calls: JSON.stringify([{ id: 'c4', function: { name: 'write', arguments: JSON.stringify({ path: 'brief.md' }) } }]), created_at: NOW },
    { id: 't4', seq: 12, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'write', tool_call_id: 'c4', content: 'wrote', created_at: NOW },
    { id: 'af', seq: 13, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'Done. The briefing is at outputs/brief.md.', created_at: NOW },
  ];
  await page.route((url) => url.pathname === `/api/sessions/${SID}/state`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ session: { id: SID, type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW }, intent_analysis: { status: 'analyzed', primary_intent: 'legacy-shape', complexity: 'L', step_budget: 16 } }) }));
  await page.route((url) => url.pathname === `/api/sessions/${SID}/events`, (route) => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const u = new URL(route.request().url());
    const sid = u.searchParams.get('session_id') ?? 'default';
    const after = Number(u.searchParams.get('after_seq') ?? '0');
    const list = sid === SID ? rows.filter((m) => m.seq > after) : [];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: list, next_cursor: list.length ? list[list.length - 1].seq : after, truncated: false }) });
  });
  // Roster: add the session under the ward as the most recent one.
  await page.route('**/api/sessions?limit=100', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ sessions: [
    { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
    { id: SID, type: 'research', status: 'completed', ward: 'stock-analysis', title: 'Legacy shape', updated_at: '2026-11-26T20:14:00Z' },
  ], truncated: false }) }));
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row', { hasText: 'stock-analysis' }).click({ force: true });
  await expect(page.locator('.analysis-chip')).toContainText('legacy-shape');
  const main = page.getByRole('main');
  // ONE turn: one user bubble, both pills resolved, one answer.
  await expect(main.getByLabel('Your message')).toHaveCount(1);
  const pills = main.locator('.delegation-pill');
  await expect(pills).toHaveCount(2);
  await expect(pills.nth(1)).toContainText('builder-agent');
  await expect(main.locator('.delegation-pill .pill-live')).toHaveCount(0); // nothing "running"
  const answers = main.getByRole('article', { name: 'Final answer' });
  await expect(answers).toHaveCount(1);
  await expect(answers).toContainText('Done. The briefing is at outputs/brief.md.');
  await expect(answers).not.toContainText('Research report');
  // The research report is the pill's report, not an answer.
  await pills.first().click();
  const dialog = page.getByRole('dialog', { name: 'Delegation to research-agent' });
  await expect(dialog.getByRole('region', { name: 'Report' })).toContainText('report received');
  await expect(dialog.locator('details.task > summary')).toContainText('TASK: research the pressures'); // the ward preamble is dropped from the gist
  await expect(dialog.locator('details.task > summary')).not.toContainText('Ward: x');
  // The task disclosure is part of the modal's Tab cycle (Close → the
  // summary when there are no calls), and expands with the keyboard.
  await page.keyboard.press('Shift+Tab');
  await expect(dialog.locator('details.task > summary')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(dialog.locator('pre.task-full')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
});

test('a mixed id / id-less result sequence never strands a turn open', async ({ page }) => {
  // Two delegations open (c1, c2); the first result is a legacy row
  // without an id (closes the NEWEST, c2 — as the grouping does), then
  // c1's result arrives with its id. Everything closes; the final answer
  // is an answer; the NEXT user message starts a new turn.
  await mockBase(page);
  await mockDaemon(page);
  const SID = 'r-mixed';
  const rows = [
    { id: 'u1', seq: 1, role: 'user', agent: 'root', execution_id: 'exec-root', content: 'first goal', created_at: NOW },
    { id: 'a1', seq: 2, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: '', tool_calls: JSON.stringify([{ id: 'c1', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'alpha', task: 'A' }) } }, { id: 'c2', function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'beta', task: 'B' }) } }]), created_at: NOW },
    { id: 't2', seq: 3, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'delegate_agent', content: 'B done (no id)', created_at: NOW },
    { id: 't1', seq: 4, role: 'tool', agent: 'root', execution_id: 'exec-root', tool_name: 'delegate_agent', tool_call_id: 'c1', content: 'A done', created_at: NOW },
    { id: 'af', seq: 5, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'Final answer one.', created_at: NOW },
    { id: 'u2', seq: 6, role: 'user', agent: 'root', execution_id: 'exec-root', content: 'second goal', created_at: NOW },
    { id: 'ag', seq: 7, role: 'assistant', agent: 'root', execution_id: 'exec-root', content: 'Final answer two.', created_at: NOW },
  ];
  await page.route((url) => url.pathname === `/api/sessions/${SID}/state`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ session: { id: SID, type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW }, intent_analysis: { status: 'analyzed', primary_intent: 'mixed', complexity: 'S', step_budget: 4 } }) }));
  await page.route((url) => url.pathname === `/api/sessions/${SID}/events`, (route) => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const u = new URL(route.request().url());
    const sid = u.searchParams.get('session_id') ?? 'default';
    const after = Number(u.searchParams.get('after_seq') ?? '0');
    const list = sid === SID ? rows.filter((m) => m.seq > after) : [];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: list, next_cursor: list.length ? list[list.length - 1].seq : after, truncated: false }) });
  });
  await page.route('**/api/sessions?limit=100', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ sessions: [
    { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
    { id: SID, type: 'research', status: 'completed', ward: 'stock-analysis', title: 'Mixed ids', updated_at: '2026-11-27T20:14:00Z' },
  ], truncated: false }) }));
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row', { hasText: 'stock-analysis' }).click({ force: true });
  await expect(page.locator('.analysis-chip')).toContainText('mixed');
  const main = page.getByRole('main');
  await expect(main.getByLabel('Your message')).toHaveCount(2);
  await expect(main.getByRole('article', { name: 'Final answer' })).toHaveCount(2);
  await expect(main.getByRole('article', { name: 'Final answer' }).first()).toContainText('Final answer one.');
  await expect(main.locator('.delegation-pill .pill-live')).toHaveCount(0);
});
