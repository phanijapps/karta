import { test, expect, type Page } from '@playwright/test';

// The session model (add-chat-modes): the sidebar IS the navigator —
// Chat / Ward Bots / Research groups, [+ New Research], session
// switching, type-aware composer, and the context panel. Route-mocked
// throughout; no daemon.

const NOW = '2026-11-24T10:00:00Z';

async function mockBase(page: Page) {
  await page.route((url) => url.pathname === '/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ provider: 'zai', id: 'glm-5.3' }], refreshed_at: { zai: NOW } }) }),
  );
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }),
  );
}

/** Session roster: the default quick chat + one ward bot + one shell. */
async function mockSessions(page: Page, extra: unknown[] = []) {
  await page.route((url) => url.pathname === '/api/sessions', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        sessions: [
          { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
          { id: 'r-stock', type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW },
          { id: 'r-cloud', type: 'research', status: 'created', title: 'Cloud Pricing', updated_at: NOW },
          ...extra,
        ],
        truncated: false,
      }),
    });
  });
}

/** Per-session transcripts: the chat lean exchange vs the research
 * pipeline rows (analysis + delegation + answer). */
async function mockTranscripts(page: Page) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const sid = new URL(route.request().url()).searchParams.get('session_id') ?? 'default';
    if (sid === 'r-stock') {
      return route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({
          messages: [
            { id: 'u1', seq: 1, role: 'user', content: 'AMD vs NVIDIA?', created_at: NOW },
            { id: 't1', seq: 2, role: 'tool', tool_name: 'intent_analysis', content: JSON.stringify({ intent_analysis: { primary_intent: 'comparative-research', complexity: 'M', approach: 'graph', ward_name: 'stock-analysis', recommended_skills: ['pdf', 'clean-code'], dropped: ['duckduckgo'] } }), created_at: NOW },
            { id: 'a1', seq: 3, role: 'assistant', content: '', tool_calls: JSON.stringify([{ function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'research-agent', task: 'market share estimates' }) } }]), created_at: NOW },
            { id: 't2', seq: 4, role: 'tool', tool_name: 'delegate_agent', content: 'done: 3 sources', created_at: NOW },
            { id: 'a2', seq: 5, role: 'assistant', content: 'NVIDIA holds 90-98% of datacenter share.', created_at: NOW },
          ],
          next_cursor: 0, truncated: false,
        }),
      });
    }
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        messages: [
          { id: 'c-u1', seq: 1, role: 'user', content: 'quick hello', created_at: NOW },
          { id: 'c-a1', seq: 2, role: 'assistant', content: 'quick reply', created_at: NOW },
        ],
        next_cursor: 0, truncated: false,
      }),
    });
  });
}

test('sidebar groups sessions and switching swaps the transcript', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  await page.goto('/');

  // Groups: Chat / Ward Bots / Research with the right members.
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  await expect(nav.getByRole('button', { name: /Quick Chat/ })).toBeVisible();
  await expect(nav.getByRole('button', { name: /stock-analysis/ })).toBeVisible();
  await expect(nav.getByRole('button', { name: /Cloud Pricing/ })).toBeVisible();

  // The quick chat is active first: lean transcript, lean composer note.
  await expect(page.getByLabel('Your message')).toContainText('quick hello');
  await expect(page.getByLabel('Active ward')).toHaveCount(0);

  // Switch to the ward-bound research session: transcript + ward bar.
  await nav.getByRole('button', { name: /stock-analysis/ }).click();
  await expect(page.getByLabel('Your message')).toContainText('AMD vs NVIDIA?');
  await expect(page.getByLabel('Active ward')).toContainText('stock-analysis');
  // The research exchange renders its Task Analysis card + delegation.
  await expect(page.getByLabel('Task Analysis')).toContainText('comparative-research');
  await expect(page.getByLabel('Task Analysis')).toContainText('pdf');
  await expect(page.getByLabel('Delegation')).toContainText('research-agent');

  // Back to quick chat: lean again, no ward bar.
  await nav.getByRole('button', { name: /Quick Chat/ }).click();
  await expect(page.getByLabel('Your message')).toContainText('quick hello');
  await expect(page.getByLabel('Active ward')).toHaveCount(0);
});

test('new research creates a shell and activates it', async ({ page }) => {
  await mockBase(page);
  await mockTranscripts(page);
  // One stateful roster route: POST creates, GET serves the roster
  // (including the new shell — the store re-lists after create).
  let created = false;
  await page.route((url) => url.pathname === '/api/sessions', (route) => {
    const method = route.request().method();
    if (method === 'POST') {
      created = true;
      return route.fulfill({
        status: 201, contentType: 'application/json',
        body: JSON.stringify({ id: 'r-new', type: 'research', status: 'created', created_at: NOW, updated_at: NOW }),
      });
    }
    if (method !== 'GET') return route.fallback();
    const sessions: unknown[] = [
      { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
    ];
    if (created) {
      sessions.push({ id: 'r-new', type: 'research', status: 'created', updated_at: NOW });
    }
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ sessions, truncated: false }),
    });
  });
  await page.goto('/');

  await page.getByRole('button', { name: 'New Research' }).click();
  await expect.poll(() => created).toBe(true);
  // The shell appears under Research and activates; the composer adapts
  // to the research type (no mode toggle anywhere).
  await expect(page.getByRole('button', { name: /Research$/ })).toBeVisible();
  await expect(page.getByLabel('Research question')).toBeVisible();
  await expect(page.getByRole('radiogroup')).toHaveCount(0);
});

test('context panel: ward card + skills + agents for a ward-bound session; none for chat', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  await page.route((url) => url.pathname === '/api/wards/stock-analysis', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        name: 'stock-analysis', archetype: 'research', lint_status: 'clean', finding_count: 0,
        canonical_title: 'Stock Analysis — equity research method',
        layout: { kind: 'directory', id: 'root', children: [
          { kind: 'file', id: 'canonical' },
          { kind: 'directory', id: 'pages', children: [{ kind: 'file', id: 'brief' }] },
        ] },
      }),
    }),
  );
  await page.goto('/');

  // Open the right panel and switch to the ward-bound session.
  await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /stock-analysis/ }).click();
  const panel = page.getByRole('complementary', { name: 'Session context' });
  await expect(panel.getByLabel('Active ward')).toBeVisible();
  await expect(panel.getByText('Stock Analysis — equity research method')).toBeVisible();
  await expect(panel.getByLabel('Provisioned skills')).toContainText('pdf');
  await expect(panel.getByLabel('Provisioned skills')).toContainText('clean-code');
  await expect(panel.getByLabel('Agents')).toContainText('research-agent');

  // Quick chat: no ward card (the section reports the lean posture).
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /Quick Chat/ }).click();
  await expect(panel.getByLabel('Active ward')).toHaveCount(0);
});
