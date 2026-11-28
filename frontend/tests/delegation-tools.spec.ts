import { test, expect, type Page } from '@playwright/test';

// Delegation on the ADK spine (engine-adk-spine, decision D1): the engine
// offers one task tool per agent — `delegate_<agent>` with a single
// `task` argument — instead of the legacy dispatch tool. The thread's
// pills, the slideout, the Work tab's nesting and turn boundaries, and the
// Context roster must recognize the per-agent shape exactly as they
// recognize `delegate_agent` (legacy rows keep working — run-legibility
// spec). Route-mocked daemon; two calls in ONE root row (the hard case).

const NOW = '2026-11-27T20:00:00Z';
const SID = 'r-spine';
const ROOT = 'exec-root-spine';
const CHILD_RESEARCH = 'exec-child-research-agent-1';
const CHILD_BUILDER = 'exec-child-builder-agent-2';

const call = (id: string, name: string, args: unknown) => ({ id, function: { name, arguments: JSON.stringify(args) } });

const TRANSCRIPT = [
  { id: 'u1', seq: 1, role: 'user', agent: 'root', execution_id: ROOT, content: 'Research the rupee and build the charts.', created_at: NOW },
  // ONE root intent row, TWO per-agent delegation calls (task only — no agent argument).
  { id: 'a1', seq: 2, role: 'assistant', agent: 'root', execution_id: ROOT, content: 'Delegating research and the charts.', tool_calls: JSON.stringify([call('c1', 'delegate_research-agent', { task: 'research INR pressures' }), call('c2', 'delegate_builder-agent', { task: 'build charts' })]), created_at: NOW },
  { id: 'u2', seq: 3, role: 'user', agent: 'research-agent', execution_id: CHILD_RESEARCH, content: 'research INR pressures', created_at: NOW },
  { id: 'r1', seq: 4, role: 'assistant', agent: 'research-agent', execution_id: CHILD_RESEARCH, content: '**Pressures on the Indian Rupee** — oil imports, FPI outflows, trade deficit, RBI intervention, Fed policy.', created_at: NOW },
  { id: 't1', seq: 5, role: 'tool', agent: 'root', execution_id: ROOT, tool_name: 'delegate_research-agent', tool_call_id: 'c1', content: '**Pressures on the Indian Rupee** — oil imports, FPI outflows, trade deficit, RBI intervention, Fed policy.', created_at: NOW },
  { id: 'u3', seq: 6, role: 'user', agent: 'builder-agent', execution_id: CHILD_BUILDER, content: 'build charts', created_at: NOW },
  { id: 'a3', seq: 7, role: 'assistant', agent: 'builder-agent', execution_id: CHILD_BUILDER, content: '', tool_calls: JSON.stringify([call('c3', 'read', { path: 'reports/data.csv' }), call('c4', 'write', { path: 'reports/chart.png' })]), created_at: NOW },
  { id: 't2', seq: 8, role: 'tool', agent: 'builder-agent', execution_id: CHILD_BUILDER, tool_name: 'read', tool_call_id: 'c3', content: 'date,rate\n…', created_at: NOW },
  { id: 't3', seq: 9, role: 'tool', agent: 'builder-agent', execution_id: CHILD_BUILDER, tool_name: 'write', tool_call_id: 'c4', content: 'wrote 12 bytes', created_at: NOW },
  { id: 'r2', seq: 10, role: 'assistant', agent: 'builder-agent', execution_id: CHILD_BUILDER, content: 'Built two charts: USDINR_12m.png and Peer_comparison.png.', created_at: NOW },
  { id: 't4', seq: 11, role: 'tool', agent: 'root', execution_id: ROOT, tool_name: 'delegate_builder-agent', tool_call_id: 'c2', content: 'Built two charts…', created_at: NOW },
  { id: 'a4', seq: 12, role: 'assistant', agent: 'root', execution_id: ROOT, content: '**Pressures on the Indian Rupee.** The rupee depreciated 6.4% in two legs.', created_at: NOW },
  // A later user turn: the Work panel must start a NEW turn, not absorb it
  // into the delegation's turn (the per-agent result has to close the open
  // delegation exactly like a `delegate_agent` result does).
  { id: 'u5', seq: 13, role: 'user', agent: 'root', execution_id: ROOT, content: 'Which driver is largest?', created_at: NOW },
  { id: 'a5', seq: 14, role: 'assistant', agent: 'root', execution_id: ROOT, content: 'Oil import costs — the largest driver.', created_at: NOW },
];

const EXECUTIONS = [
  { id: ROOT, agent: 'root', role: 'root', status: 'completed', tokens_in: 721214, tokens_out: 36832, started_at: NOW, finished_at: NOW },
  { id: CHILD_RESEARCH, parent_id: ROOT, agent: 'research-agent', role: 'delegated', status: 'completed', tokens_in: 1102, tokens_out: 7961, started_at: NOW, finished_at: NOW },
  { id: CHILD_BUILDER, parent_id: ROOT, agent: 'builder-agent', role: 'delegated', status: 'completed', tokens_in: 284089, tokens_out: 16814, started_at: NOW, finished_at: NOW },
];

async function mockDaemon(page: Page) {
  await page.route((url) => url.pathname === '/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ provider: 'zai', id: 'glm-5.3' }], refreshed_at: { zai: NOW } }) }));
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }));
  await page.route('**/api/sessions?limit=100', (route) => route.fulfill({
    status: 200, contentType: 'application/json',
    body: JSON.stringify({ sessions: [
      { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
      { id: SID, type: 'research', status: 'completed', ward: 'stock-analysis', title: 'Spine delegations', updated_at: NOW },
    ], truncated: false }),
  }));
  await page.route('**/api/wards', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ wards: [] }) }));
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const u = new URL(route.request().url());
    const sid = u.searchParams.get('session_id') ?? 'default';
    const after = Number(u.searchParams.get('after_seq') ?? '0');
    const rows = sid === SID ? TRANSCRIPT.filter((m) => m.seq > after) : [];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: rows, next_cursor: rows.length ? rows[rows.length - 1].seq : after, truncated: false }) });
  });
  await page.route((url) => /^\/api\/sessions\/[^/]+\/state$/.test(url.pathname), (route) => {
    const sid = new URL(route.request().url()).pathname.split('/')[3];
    const body: Record<string, unknown> = { session: { id: sid, type: sid === 'default' ? 'chat' : 'research', status: 'completed', ward: sid === 'default' ? undefined : 'stock-analysis', updated_at: NOW } };
    if (sid === SID) {
      body.intent_analysis = { status: 'analyzed', primary_intent: 'analyze-inr-usd-pressures', complexity: 'M', step_budget: 8, approach: 'graph', ward_name: 'stock-analysis' };
      body.executions = EXECUTIONS;
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
  await page.route((url) => url.pathname === `/api/sessions/${SID}/events`, (route) => {
    const frames = [
      'event: delegation_started\ndata: ' + JSON.stringify({ id: 'e1', kind: 'delegation_started', agent: 'research-agent', execution_id: CHILD_RESEARCH, text: 'Delegated to research-agent: research INR pressures', delegation: { child_agent: 'research-agent', task: 'research INR pressures', execution_id: CHILD_RESEARCH } }),
      'event: delegation_completed\ndata: ' + JSON.stringify({ id: 'e2', kind: 'delegation_completed', agent: 'research-agent', execution_id: CHILD_RESEARCH, text: 'research-agent finished (completed)', delegation: { child_agent: 'research-agent', task: 'research INR pressures', execution_id: CHILD_RESEARCH, tokens_in: 1102, tokens_out: 7961, duration_ms: 84000, status: 'completed' } }),
      'event: delegation_started\ndata: ' + JSON.stringify({ id: 'e3', kind: 'delegation_started', agent: 'builder-agent', execution_id: CHILD_BUILDER, text: 'Delegated to builder-agent: build charts', delegation: { child_agent: 'builder-agent', task: 'build charts', execution_id: CHILD_BUILDER } }),
      'event: delegation_completed\ndata: ' + JSON.stringify({ id: 'e4', kind: 'delegation_completed', agent: 'builder-agent', execution_id: CHILD_BUILDER, text: 'builder-agent finished (completed)', delegation: { child_agent: 'builder-agent', task: 'build charts', execution_id: CHILD_BUILDER, tokens_in: 284089, tokens_out: 16814, duration_ms: 372000, status: 'completed' } }),
    ].join('\n\n') + '\n\n';
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body: frames });
  });
}

async function openSession(page: Page) {
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  await nav.locator('.ward-row', { hasText: 'stock-analysis' }).click({ force: true });
  await expect(page.locator('.analysis-chip')).toContainText('analyze-inr-usd-pressures');
}

test('per-agent delegation tools render as pills and slideouts with the agent from the tool name', async ({ page }) => {
  await mockDaemon(page);
  await openSession(page);
  const pills = page.locator('.delegation-pill');
  await expect(pills).toHaveCount(2);
  await expect(pills.nth(0)).toContainText('research-agent');
  await expect(pills.nth(1)).toContainText('builder-agent');
  // One answer per turn (the root's last assistant row is THE answer).
  await expect(page.getByRole('article', { name: 'Final answer' })).toHaveCount(2);
  await pills.nth(1).click();
  const dialog = page.getByRole('dialog', { name: 'Delegation to builder-agent' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('build charts');
  await expect(dialog).toContainText('completed · 284.1k → 16.8k tokens');
  const readStep = dialog.getByRole('button', { name: /Read/ });
  await readStep.click();
  await expect(dialog).toContainText('reports/data.csv');
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(pills.nth(1)).toBeFocused();
});

test('the Work tab nests each agent\'s steps under the shared delegation step and starts a new turn for the next user row; Context lists both agents', async ({ page }) => {
  await mockDaemon(page);
  await openSession(page);
  const work = page.getByRole('tabpanel', { name: 'Work' });
  await expect(work).toBeVisible();
  // Two delegation calls in ONE root intent row are one Work step (a mixed
  // row is named by its majority/first tool; the other call lists in the
  // expansion) — named from the tool NAME, with the task gist, not raw JSON.
  const delegation = work.locator('.step.delegation');
  await expect(delegation).toHaveCount(1);
  await expect(delegation).toContainText('Delegated');
  await expect(delegation).toContainText('research-agent');
  await delegation.locator('button.step-row').first().click(); // expand: the calls list, the outcomes and the nested children
  await expect(delegation).toContainText('delegate_builder-agent');
  await expect(delegation).toContainText('build charts');
  // The builder's attributed tool rows nest under the step (per-call
  // matching): its read+write merge into one child row. The research
  // child's report is a content-only row — it shows through its pill and
  // slideout (see the first test), never as a Work step.
  await expect(delegation).toContainText('Read reports/data.csv');
  const childRows = delegation.locator('.children .step.child');
  await expect(childRows).toHaveCount(1);
  await expect(childRows.first()).toContainText('builder-agent');
  // The per-agent results close their delegations: the follow-up user row
  // opens a SECOND Work turn instead of being absorbed.
  await expect(work.locator('section.work-turn[aria-label="Turn"]')).toHaveCount(2);
  await expect(work).toContainText('Which driver is largest?');
  // The Context tab's Agents roster derives both agents from the tool names.
  await page.getByRole('tab', { name: 'Context' }).click();
  const agents = page.getByRole('region', { name: 'Agents' });
  await expect(agents).toContainText('research-agent');
  await expect(agents).toContainText('builder-agent');
});
