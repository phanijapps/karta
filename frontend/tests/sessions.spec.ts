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

/** Per-session transcript ROWS (full history; the mock pages by seq). */
const TRANSCRIPTS: Record<string, unknown[]> = {
  'r-stock': [
    { id: 'u1', seq: 1, role: 'user', content: 'AMD vs NVIDIA?', created_at: NOW },
    { id: 't1', seq: 2, role: 'tool', tool_name: 'intent_analysis', content: JSON.stringify({ intent_analysis: { primary_intent: 'comparative-research', complexity: 'M', approach: 'graph', ward_name: 'stock-analysis', recommended_skills: ['pdf', 'clean-code'], dropped: ['duckduckgo'] } }), created_at: NOW },
    { id: 'a1', seq: 3, role: 'assistant', content: '', tool_calls: JSON.stringify([{ function: { name: 'delegate_agent', arguments: JSON.stringify({ agent: 'research-agent', task: 'market share estimates' }) } }]), created_at: NOW },
    { id: 't2', seq: 4, role: 'tool', tool_name: 'delegate_agent', content: 'done: 3 sources', created_at: NOW },
    { id: 'a2', seq: 5, role: 'assistant', content: 'NVIDIA holds 90-98% of datacenter share.', created_at: NOW },
  ],
  default: [
    { id: 'c-u1', seq: 1, role: 'user', content: 'quick hello', created_at: NOW },
    { id: 'c-a1', seq: 2, role: 'assistant', content: 'quick reply', created_at: NOW },
  ],
};

/** Session-state mock: research sessions carry an analysis; the quick
 * chat carries none (lean — analysis never runs). */
async function mockStates(page: Page) {
  await page.route((url) => /^\/api\/sessions\/[^/]+\/state$/.test(url.pathname), (route) => {
    const sid = new URL(route.request().url()).pathname.split('/')[3];
    const session = sid === 'default'
      ? { id: 'default', type: 'chat', status: 'completed', updated_at: NOW }
      : { id: sid, type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW };
    const body: Record<string, unknown> = { session };
    if (sid !== 'default') {
      body.intent_analysis = {
        status: 'analyzed',
        primary_intent: 'comparative-research',
        complexity: 'M',
        step_budget: 8,
        approach: 'graph',
        ward_action: 'use_existing',
        ward_name: 'stock-analysis',
        recommended_skills: ['pdf', 'clean-code'],
        recommended_agents: ['research-agent'],
        dropped: ['duckduckgo'],
      };
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
}

/** Cursor-aware transcript mock: honors after_seq per session (a mock
 * that ignores the cursor masks remount/incremental-sync regressions). */
async function mockTranscripts(page: Page) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const u = new URL(route.request().url());
    const sid = u.searchParams.get('session_id') ?? 'default';
    const after = Number(u.searchParams.get('after_seq') ?? '0');
    if (sid !== 'default' && !(sid in TRANSCRIPTS)) {
      // Sessions the roster introduced but the transcript fixture does
      // not know (freshly created shells): an EMPTY page.
      return route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({ messages: [], next_cursor: after, truncated: false }),
      });
    }
    const rows = (TRANSCRIPTS[sid] ?? []).filter(
      (m) => (m as { seq: number }).seq > after,
    );
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        messages: rows,
        next_cursor: rows.length ? (rows[rows.length - 1] as { seq: number }).seq : after,
        truncated: false,
      }),
    });
  });
}

test('sidebar groups sessions and switching swaps the transcript', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  await mockStates(page);
  await page.goto('/');

  // Groups: Chat / Ward Bots / Research — membership proven per group
  // list, not just globally.
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  const chatGroup = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Chat' }).first();
  const wardGroup = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Ward Bots' }).first();
  const researchGroup = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Research' }).first();
  await expect(chatGroup.getByRole('button', { name: /Quick Chat/ })).toBeVisible();
  await expect(wardGroup.getByRole('button', { name: /stock-analysis/ })).toBeVisible();
  await expect(researchGroup.getByRole('button', { name: /Cloud Pricing/ })).toBeVisible();
  await expect(chatGroup.getByRole('button', { name: /stock-analysis/ })).toHaveCount(0);

  // The quick chat is active first (aria-current): lean transcript, no ward bar.
  await expect(chatGroup.getByRole('button', { name: /Quick Chat/ })).toHaveAttribute('aria-current', 'true');
  await expect(page.getByLabel('Your message')).toContainText('quick hello');
  await expect(page.getByLabel('Active ward')).toHaveCount(0);

  // Switch to the ward-bound research session: transcript + ward bar +
  // the active marker MOVES with the selection.
  await wardGroup.getByRole('button', { name: /stock-analysis/ }).click();
  await expect(wardGroup.getByRole('button', { name: /stock-analysis/ })).toHaveAttribute('aria-current', 'true');
  await expect(chatGroup.getByRole('button', { name: /Quick Chat/ })).not.toHaveAttribute('aria-current', 'true');
  await expect(page.getByLabel('Your message')).toContainText('AMD vs NVIDIA?');
  await expect(page.getByLabel('Active ward')).toContainText('stock-analysis');
  // The research exchange renders its Task Analysis card (from session
  // state: intent, budget, skills, dropped) + the delegation tree.
  await expect(page.getByLabel('Task Analysis')).toContainText('comparative-research');
  await expect(page.getByLabel('Task Analysis')).toContainText('8 steps');
  await expect(page.getByLabel('Task Analysis')).toContainText('pdf');
  await expect(page.getByLabel('Task Analysis').getByText('duckduckgo ✕ dropped')).toBeVisible();
  await expect(page.getByLabel('Delegation')).toContainText('research-agent');

  // Back to quick chat: lean again, no ward bar, no analysis card.
  await nav.getByRole('button', { name: /Quick Chat/ }).click();
  await expect(page.getByLabel('Your message')).toContainText('quick hello');
  await expect(page.getByLabel('Active ward')).toHaveCount(0);
  await expect(page.getByLabel('Task Analysis')).toHaveCount(0);
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
  // The shell appears under Research and activates with an EMPTY
  // transcript; the composer adapts to the research type (no mode
  // toggle anywhere).
  await expect(page.getByRole('heading', { name: 'A research session' })).toBeVisible();
  await expect(page.getByLabel('Your message')).toHaveCount(0);
  await expect(page.getByLabel('Research question')).toBeVisible();
  await expect(page.getByRole('radiogroup')).toHaveCount(0);
});

test('context panel: ward card + skills + agents for a ward-bound session; none for chat', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  await mockStates(page);
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
  await expect(panel.getByLabel('Ward layout')).toContainText('canonical');
  await expect(panel.getByLabel('Ward layout')).toContainText('brief');
  await expect(panel.getByLabel('Provisioned skills')).toContainText('pdf');
  await expect(panel.getByLabel('Provisioned skills')).toContainText('clean-code');
  await expect(panel.getByLabel('Agents')).toContainText('research-agent');

  // Quick chat: no ward card (the section reports the lean posture).
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /Quick Chat/ }).click();
  await expect(panel.getByLabel('Active ward')).toHaveCount(0);
});

test('remounting the chat surface keeps the full history (no cursor-blanking)', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ vault_root: '/v', wards: [] }) }),
  );
  await page.goto('/');
  await expect(page.getByLabel('Your message')).toContainText('quick hello');

  // Leave to Wards (Chat unmounts), come back: the transcript reloads
  // in full — a cursor-starting reload would blank it with zero new rows.
  await page.getByRole('button', { name: 'Wards', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Wards', exact: true })).toBeVisible();
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /Quick Chat/ }).click();
  await expect(page.getByLabel('Your message')).toContainText('quick hello');
  await expect(page.getByLabel('Assistant message')).toContainText('quick reply');
});

test('switching sessions mid-send does not leak live output; returning reconciles', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  // A hanging stream in the stock ward: the request never completes —
  // the pending bubble is live (documented Playwright behavior for
  // hanging handlers, same technique as shell.spec.ts).
  let streamStarted = false;
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') return route.fallback();
    streamStarted = true; // the research send (unit completion)
    return new Promise(() => {}); // never resolves: the send stays live
  });
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /stock-analysis/ }).click();
  await page.getByLabel('Research question').fill('long question');
  await page.getByLabel('Research question').press('Enter');
  await expect.poll(() => streamStarted).toBe(true);
  // The pending indicator is live in the research session.
  await expect(page.getByLabel('Assistant is working')).toBeVisible();

  // Switch to the quick chat MID-SEND: no pending bubble leaks into the
  // lean transcript.
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /Quick Chat/ }).click();
  await expect(page.getByLabel('Assistant is working')).toHaveCount(0);
  await expect(page.getByLabel('Your message')).toContainText('quick hello');

  // Back to the research session: its transcript stands (the terminal
  // exchange never arrived; reconciliation is the daemon's truth).
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /stock-analysis/ }).click();
  await expect(page.getByLabel('Your message')).toContainText('AMD vs NVIDIA?');
  await expect(page.getByLabel('Assistant message').last()).toContainText('NVIDIA holds 90-98%');
});

test('the draft clears the instant a send starts (not on completion)', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  await mockStates(page);
  // A send that never completes: if the draft waited for it, the text
  // would sit in the composer for the whole run. Quick chat streams —
  // hang the SSE endpoint it actually posts to.
  let sendStarted = false;
  await page.route('**/api/chat/send', async (route) => {
    sendStarted = true;
    await new Promise(() => {});
  });
  await page.goto('/');
  const input = page.getByRole('textbox', { name: 'Message Karta' });
  await input.fill('long research question');
  await input.press('Enter');
  await expect.poll(() => sendStarted).toBe(true);
  await expect(page.getByLabel('Assistant is working')).toBeVisible();
  await expect(input).toHaveValue('');
});

test('ward rows select wardens: ensure on first select, reuse after, one row per ward', async ({ page }) => {
  await mockBase(page);
  // A ward WITHOUT a warden in the roster + an ordinary bound session
  // (NOT the warden) that must not be picked as the ward's bot.
  // Stateful roster: the warden joins the list once ensured.
  let ensured = 0;
  await page.route((url) => url.pathname === '/api/sessions', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const sessions: unknown[] = [
      { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
      { id: 'r-stock', type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW },
    ];
    if (ensured > 0) {
      sessions.push({ id: 'ward-stock-analysis', type: 'research', status: 'created', ward: 'stock-analysis', updated_at: NOW });
    }
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ sessions, truncated: false }),
    });
  });
  await page.route('**/api/wards', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ vault_root: '/v', wards: [
        { name: 'stock-analysis', archetype: 'research', lint_status: 'clean', finding_count: 0 },
      ] }),
    }),
  );
  await page.route((url) => url.pathname === '/api/wards/stock-analysis/warden', (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    ensured++;
    return route.fulfill({
      status: 201, contentType: 'application/json',
      body: JSON.stringify({ id: 'ward-stock-analysis', type: 'research', status: 'created', ward: 'stock-analysis', created_at: NOW, updated_at: NOW }),
    });
  });
  await mockTranscripts(page);
  await mockStates(page);
  await page.goto('/');

  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  const group = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Ward Bots' }).first();
  // The ordinary bound session renders; the ward itself renders a row
  // too (its warden doesn't exist yet).
  await expect(group.getByRole('button', { name: /r-stock|stock-analysis/ })).toHaveCount(2);

  // Selecting the WARD row ensures the warden (not r-stock).
  await group.getByRole('button', { name: /warden · tap to open/ }).click();
  await expect.poll(() => ensured).toBe(1);
  await expect(page.getByLabel('Research question')).toBeVisible();
});
