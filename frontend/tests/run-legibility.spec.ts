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
  { id: 't1', seq: 5, role: 'tool', agent: 'root', execution_id: 'exec-root-r-inr', tool_name: 'delegate_agent', tool_call_id: 'c1', content: 'done', created_at: NOW },
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

test('reports never pose as answers: sub-agent rows are collapsed cards, the root\'s last row is THE answer', async ({ page }) => {
  await mockBase(page);
  await mockDaemon(page);
  await openInrSession(page);
  const reports = page.locator('details.report');
  await expect(reports).toHaveCount(2);
  await expect(reports.nth(0)).toContainText('Sub-agent report');
  await expect(reports.nth(0)).toContainText('research-agent');
  await expect(reports.nth(0)).toContainText('KB'); // size, not the whole text at the top level
  await expect(reports.nth(0)).not.toHaveAttribute('open', '');
  await expect(reports.nth(1)).toContainText('builder-agent');
  // Exactly ONE answer-styled root block, tagged as the answer, and the
  // narration before it is labelled as narration.
  const answers = page.getByRole('article', { name: 'Final answer' });
  await expect(answers).toHaveCount(1);
  await expect(answers).toContainText('The answer');
  await expect(answers).toContainText('Pressures on the Indian Rupee');
  await expect(page.getByRole('article', { name: 'Assistant message' })).toHaveCount(0);
  await expect(page.locator('.narration')).toContainText('Narration');
  await expect(page.locator('.narration')).toContainText('Verified the intervention figures');
  // The report body is readable on demand, with its own thinking.
  await reports.nth(0).locator(':scope > summary').click();
  await expect(reports.nth(0)).toContainText('I will rank the drivers first.');
});

test('thinking is the final step\'s reasoning', async ({ page }) => {
  await mockBase(page);
  await mockDaemon(page);
  await openInrSession(page);
  const answer = page.getByRole('article', { name: 'Final answer' });
  const thinking = answer.locator('details.answer-thinking');
  await expect(thinking).not.toHaveAttribute('open', '');
  await thinking.locator('summary').click();
  await expect(thinking).toContainText('flag the reserve-figure divergence');
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
  await expect(cards.nth(0)).toContainText('1.1k → 8.0k');
  await expect(cards.nth(0)).toContainText('1m 24s');
  await expect(cards.nth(1)).toContainText('builder-agent');
  await expect(cards.nth(1)).toContainText('284.1k → 16.8k');
  // The footer totals every execution: root + analyst + both children.
  const foot = work.locator('footer.run-foot');
  await expect(foot).toContainText('Run: 1.0M prompt');
  await expect(foot).toContainText('61.9k completion');
  await expect(foot).toContainText('2 delegations');
  await expect(foot).toContainText('14m 0s');
});
