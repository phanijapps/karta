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

/** Session roster: the default quick chat + one ward bot + one shell.
 * Also pins the WARD roster (hermetic: never the live daemon's). */
async function mockSessions(page: Page, extra: unknown[] = []) {
  await page.route('**/api/wards', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ vault_root: '/v', wards: [
        { name: 'stock-analysis', archetype: 'research', lint_status: 'clean', finding_count: 0, canonical_title: 'Stock Analysis' },
      ] }),
    }),
  );
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


/** The right sidebar's Work tab (tool history). The panel auto-opens
 * for research sessions on wide viewports; this makes it explicit. */
async function workTab(page: Page) {
  // The panel auto-opens for research sessions; only toggle when closed.
  if ((await page.getByRole('complementary', { name: 'Session context' }).count()) === 0) {
    await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  }
  const panel = page.getByRole('complementary', { name: 'Session context' });
  await panel.getByRole('tab', { name: /Work/ }).click();
  return panel.getByRole('tabpanel', { name: 'Work' });
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

  // Groups: Chat / Ward Bots (ONE unique row per ward) / Research.
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  const chatGroup = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Chat' }).first();
  const wardGroup = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Ward Bots' }).first();
  const researchGroup = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Research' }).first();
  await expect(chatGroup.getByRole('button', { name: /Quick Chat/ })).toBeVisible();
  await expect(wardGroup.getByRole('button', { name: /stock-analysis/ })).toHaveCount(1); // UNIQUE
  await expect(wardGroup.getByRole('button', { name: /1 session ·/ })).toBeVisible();
  await expect(researchGroup.getByRole('button', { name: /Cloud Pricing/ })).toBeVisible();
  await expect(chatGroup.getByRole('button', { name: /stock-analysis/ })).toHaveCount(0);

  // The quick chat is active first (aria-current): lean transcript, no ward bar.
  await expect(chatGroup.getByRole('button', { name: /Quick Chat/ })).toHaveAttribute('aria-current', 'true');
  await expect(page.getByLabel('Your message')).toContainText('quick hello');
  await expect(page.getByLabel('Active ward')).toHaveCount(0);

  // Select the WARD: it expands and its LAST EXECUTED session activates.
  await wardGroup.locator('.ward-row').click();
  const child = wardGroup.locator('.session.child');
  await expect(child).toHaveCount(1); // the ward's session renders nested
  await expect(child).toHaveAttribute('aria-current', 'true');
  await expect(chatGroup.getByRole('button', { name: /Quick Chat/ })).not.toHaveAttribute('aria-current', 'true');
  await expect(page.getByLabel('Your message')).toContainText('AMD vs NVIDIA?');
  await expect(page.getByLabel('Active ward')).toContainText('stock-analysis');
  // Task Analysis: a compact CHIP (intent + complexity + ward from
  // session state) with an (i) that opens the detail popover — intent,
  // budget, provisioned skills, dropped names. Escape closes and
  // returns focus to the (i).
  const chip = page.locator('.analysis-chip');
  await expect(chip).toContainText('comparative-research');
  await expect(chip).toContainText('stock-analysis');
  await expect(page.getByRole('dialog', { name: 'Task Analysis' })).toHaveCount(0);
  const info = page.getByRole('button', { name: 'Task analysis details' });
  await info.click();
  const pop = page.getByRole('dialog', { name: 'Task Analysis' });
  await expect(pop).toContainText('8 steps');
  await expect(pop).toContainText('pdf');
  await expect(pop.getByText('duckduckgo ✕ dropped')).toBeVisible();
  await expect(pop).toContainText('analyzed');
  await page.keyboard.press('Escape');
  await expect(pop).toHaveCount(0);
  await expect(info).toBeFocused();
  // The tool history lives in the SIDEBAR's Work tab — the thread is
  // dialogue only (user message + answer bubbles, nothing between).
  const work = await workTab(page);
  await expect(work).toContainText('Delegated research-agent');
  await expect(page.getByRole('main').getByRole('button', { name: /Delegated/ })).toHaveCount(0);

  // Back to quick chat: lean again, no ward bar, no analysis card.
  await chatGroup.getByRole('button', { name: /Quick Chat/ }).click();
  await expect(page.getByLabel('Your message')).toContainText('quick hello');
  await expect(page.getByLabel('Active ward')).toHaveCount(0);
  await expect(page.locator('.analysis-chip')).toHaveCount(0); // chat: no analysis chip
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

test('context tab: ward card + skills + agents for a ward-bound session; none for chat', async ({ page }) => {
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
  // The panel auto-opened on the research session (Work tab). Switch
  // to Context for the ward card / skills / agents.
  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  await nav.locator('.ward-row').click();
  const panel = page.getByRole('complementary', { name: 'Session context' });
  await panel.getByRole('tab', { name: 'Context' }).click();
  const ctx = panel.getByRole('tabpanel', { name: 'Context' });
  await expect(ctx.getByLabel('Active ward')).toBeVisible();
  await expect(ctx.getByText('Stock Analysis — equity research method')).toBeVisible();
  await expect(ctx.getByLabel('Ward layout')).toContainText('canonical');
  await expect(ctx.getByLabel('Ward layout')).toContainText('brief');
  await expect(ctx.getByLabel('Provisioned skills')).toContainText('pdf');
  await expect(ctx.getByLabel('Provisioned skills')).toContainText('clean-code');
  await expect(ctx.getByLabel('Agents')).toContainText('research-agent');

  // Quick chat: the panel lands on Context with no ward card, and the
  // Work tab has nothing to show.
  await nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Chat' }).first().getByRole('button', { name: /Quick Chat/ }).click();
  await expect(ctx.getByLabel('Active ward')).toHaveCount(0);
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
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row').click({ force: true });
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
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row').click({ force: true });
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

test('unique ward rows: expand + latest selected; empty wards ensure their warden', async ({ page }) => {
  await mockBase(page);
  // Two wards: stock-analysis (one bound session r-stock) and
  // empty-ward (no sessions — selecting it must ENSURE the warden).
  let ensured = 0;
  await page.route('**/api/wards', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ vault_root: '/v', wards: [
        { name: 'stock-analysis', archetype: 'research', lint_status: 'clean', finding_count: 0 },
        { name: 'empty-ward', archetype: 'generic', lint_status: 'clean', finding_count: 0 },
      ] }),
    }),
  );
  await page.route((url) => url.pathname === '/api/sessions', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const sessions: unknown[] = [
      { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
      { id: 'r-stock', type: 'research', status: 'completed', ward: 'stock-analysis', updated_at: NOW },
    ];
    if (ensured > 0) {
      sessions.push({ id: 'ward-empty-ward', type: 'research', status: 'created', ward: 'empty-ward', updated_at: NOW });
    }
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ sessions, truncated: false }),
    });
  });
  await page.route((url) => url.pathname === '/api/wards/empty-ward/warden', (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    ensured++;
    return route.fulfill({
      status: 201, contentType: 'application/json',
      body: JSON.stringify({ id: 'ward-empty-ward', type: 'research', status: 'created', ward: 'empty-ward', created_at: NOW, updated_at: NOW }),
    });
  });
  await mockTranscripts(page);
  await mockStates(page);
  await page.goto('/');

  const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
  const group = nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Ward Bots' }).first();

  // ONE unique row per ward (both wards), sessions hidden until expanded.
  await expect(group.locator('.ward-row')).toHaveCount(2);
  await expect(group.locator('.session.child')).toHaveCount(0);

  // Selecting a ward WITH sessions: expands, the LAST EXECUTED session
  // (r-stock) activates — NO warden ensure.
  await group.locator('.ward-row', { hasText: 'stock-analysis' }).click({ force: true });
  await expect(group.locator('.session.child', { hasText: 'r-stock' })).toHaveCount(1);
  await expect(group.locator('.session.child', { hasText: 'r-stock' })).toHaveAttribute('aria-current', 'true');
  expect(ensured).toBe(0);
  await expect(page.getByLabel('Your message')).toContainText('AMD vs NVIDIA?');

  // Selecting a ward WITHOUT sessions: the warden is ENSURED, becomes
  // its child, and activates.
  await group.locator('.ward-row', { hasText: 'empty-ward' }).click({ force: true });
  await expect.poll(() => ensured).toBe(1);
  await expect(group.locator('.ward-row', { hasText: 'empty-ward' })).toHaveAttribute('aria-expanded', 'true');
  await expect(group.locator('.session.child', { hasText: 'Warden' })).toHaveAttribute('aria-current', 'true');
  await expect(page.getByLabel('Research question')).toBeVisible();

  // Re-selecting the ward reuses the warden (no re-ensure).
  await nav.getByRole('list', { name: 'Sessions' }).locator('li', { hasText: 'Chat' }).first().getByRole('button', { name: /Quick Chat/ }).click();
  await group.locator('.ward-row', { hasText: 'empty-ward' }).click({ force: true });
  await page.waitForTimeout(300);
  expect(ensured).toBe(1);
});

test('a live research run shows its latest thinking in the thread; the analysis lands in the chip mid-run', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await mockTranscripts(page);
  await mockStates(page);
  let sendStarted = false;
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') return route.fallback();
    sendStarted = true;
    return new Promise(() => {});
  });
  await page.route((url) => url.pathname === '/api/sessions/r-stock/events', (route) => {
    // The intent-analyst trail: started (work-stream row only — never the
    // chip) then completed (the chip lands from the terminal event).
    const frames = [
      'event: analysis_started\ndata: ' + JSON.stringify({ id: 'e0', kind: 'analysis_started', text: 'Intent analysis started — zai/glm-5.3-flash (window 128000, table)', intent_analysis: { status: 'started', model: 'zai/glm-5.3-flash', context_window: 128000, window_source: 'table' } }),
      'event: analysis_completed\ndata: ' + JSON.stringify({ id: 'e1', kind: 'analysis_completed', text: 't', intent_analysis: { status: 'analyzed', primary_intent: 'nvda-check', complexity: 'M', step_budget: 8, approach: 'graph', recommended_skills: ['pdf'], tokens_in: 1200, tokens_out: 180, duration_ms: 900, attempts: 1, model: 'zai/glm-5.3-flash', context_window: 128000, window_source: 'table' } }),
      'event: agent_step\ndata: ' + JSON.stringify({ id: 'e2', kind: 'agent_step', text: 'Entering the ward and staging the search.' }),
      'event: agent_step\ndata: ' + JSON.stringify({ id: 'e3', kind: 'agent_step', text: 'Reading the prior brief before searching.' }),
      'event: tool_call\ndata: ' + JSON.stringify({ id: 'e4', kind: 'tool_call', text: 'mcp_brave-mcp_brave_web_search' }),
    ].join('\n\n') + '\n\n';
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body: frames });
  });
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row').click();
  await page.getByLabel('Research question').fill('live streaming check');
  await page.getByLabel('Research question').press('Enter');
  await expect.poll(() => sendStarted).toBe(true);
  // The MAIN THREAD shows the run's LATEST narration as live thinking
  // (the newest agent_step), not a log of every event.
  const working = page.getByLabel('Assistant is working');
  await expect(working).toBeVisible();
  await expect(working).toContainText('Reading the prior brief before searching.');
  await expect(working).not.toContainText('Entering the ward and staging');
  // The analysis chip lands mid-run from the TERMINAL event, with its
  // cost in the popover.
  await expect(page.locator('.analysis-chip')).toContainText('nvda-check');
  await page.getByRole('button', { name: 'Task analysis details' }).click();
  const popover = page.getByRole('dialog', { name: 'Task Analysis' });
  await expect(popover).toContainText('1.2k in / 180 out');
  await expect(popover).toContainText('900 ms');
  await expect(popover).toContainText('zai/glm-5.3-flash');
});

test('leaving a ward: the strip offers Leave (non-warden), clears the binding, focus returns to the composer', async ({ page }) => {
  await mockBase(page);
  // r-stock is bound but NOT a warden → Leave shows.
  await mockSessions(page);
  await mockTranscripts(page);
  await mockStates(page);
  let left = 0;
  await page.route((url) => url.pathname === '/api/sessions/r-stock/leave-ward', (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    left++;
    return route.fulfill({ status: 204 });
  });
  // After leaving, the roster reports r-stock unbound.
  let leftOnce = false;
  await page.route((url) => url.pathname === '/api/sessions', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ sessions: [
        { id: 'default', type: 'chat', status: 'completed', updated_at: NOW },
        { id: 'r-stock', type: 'research', status: 'completed', ...(leftOnce ? {} : { ward: 'stock-analysis' }), updated_at: NOW },
      ], truncated: false }),
    });
  });
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row').click();
  await expect(page.getByLabel('Active ward')).toContainText('stock-analysis');
  const leave = page.getByRole('button', { name: 'Leave ward stock-analysis' });
  await expect(leave).toBeVisible();
  leftOnce = true;
  await leave.click();
  await expect.poll(() => left).toBe(1);
  await expect(page.getByLabel('Active ward')).toHaveCount(0);
  // Focus handed to the composer; the screen reader hears why.
  await expect(page.getByRole('textbox', { name: 'Research question' })).toBeFocused();
  // The announcement lives in the polite live region (sr-only).
  await expect(page.locator('[aria-live="polite"]')).toContainText('Left stock-analysis');
});

test('the thread is dialogue only; tool history with friendly verbs lives in the sidebar Work tab', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      messages: [
        { id: 'u1', seq: 1, role: 'user', content: 'run the thing', created_at: NOW },
        { id: 'a1', seq: 2, role: 'assistant', content: 'Announcing, then executing one call: shell echo. Immediately after…', reasoning: 'Hmm — every call I put in these reply blocks seems to substitute…', tool_calls: JSON.stringify([{ id: 'c1', function: { name: 'shell', arguments: '{"command":"echo hi"}' } }]), created_at: NOW },
        { id: 't1', seq: 3, role: 'tool', tool_name: 'shell', tool_call_id: 'c1', content: '$ echo hi\nhi\n[12ms in stock-analysis]', created_at: NOW },
        { id: 'a2', seq: 4, role: 'assistant', content: 'Done — it printed hi.', created_at: NOW },
      ], next_cursor: 0, truncated: false }) });
  });
  await mockStates(page);
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row').click();
  // MAIN THREAD: your message + the answer. Nothing else.
  const main = page.getByRole('main');
  await expect(main.getByLabel('Your message')).toHaveCount(1);
  await expect(main.getByLabel('Assistant message')).toHaveCount(1);
  await expect(main.getByLabel('Assistant message')).toContainText('Done — it printed hi.');
  await expect(main.getByText('Announcing, then executing')).toHaveCount(0);
  await expect(main.getByText('Hmm — every call')).toHaveCount(0);
  // SIDEBAR: the turn grouped under its user message, a friendly-verb
  // step with the outcome; narration + brain folded until expanded.
  const work = await workTab(page);
  await expect(work.locator('.turn-user')).toContainText('run the thing');
  const step = work.getByRole('button', { name: /Ran echo hi/ });
  await expect(step).toContainText('exit 0');
  await expect(work.getByText('Announcing, then executing')).toBeHidden();
  await step.click();
  await expect(work.getByText('Announcing, then executing')).toBeVisible();
  await expect(work.getByText('Hmm — every call')).toBeHidden(); // the brain stays collapsed
  await work.getByText('Thinking').click();
  await expect(work.getByText('Hmm — every call')).toBeVisible();
  // The Context tab still has the ward/skills.
  const panel = page.getByRole('complementary', { name: 'Session context' });
  await panel.getByRole('tab', { name: 'Context' }).click();
  await expect(panel.getByRole('tabpanel', { name: 'Context' })).toContainText('Provisioned Skills');
});

test('work tab groups consecutive same-tool turns and nests a delegation\'s work', async ({ page }) => {
  await mockBase(page);
  await mockSessions(page);
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const call = (id: string, name: string, args: string) => ({ id, type: 'function', function: { name, arguments: args } });
    const intent = (id: string, seq: number, calls: unknown[]) => ({ id, seq, role: 'assistant', content: '', tool_calls: JSON.stringify(calls), created_at: NOW });
    const result = (id: string, seq: number, cid: string, name: string, content: string) => ({ id, seq, role: 'tool', tool_name: name, tool_call_id: cid, content, created_at: NOW });
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      messages: [
        { id: 'u1', seq: 1, role: 'user', content: 'research it', created_at: NOW },
        intent('a1', 2, [call('c1', 'ward_use', '{"name":"stock-analysis"}')]),
        result('t1', 3, 'c1', 'ward_use', 'ACTIVE WARD: stock-analysis'),
        intent('a2', 4, [call('c2', 'mcp_brave-mcp_brave_web_search', '{"query":"nvda revenue"}')]),
        result('t2', 5, 'c2', 'mcp_brave-mcp_brave_web_search', '7 results…'),
        intent('a3', 6, [call('c3', 'mcp_brave-mcp_brave_web_search', '{"query":"nvda q3"}')]),
        result('t3', 7, 'c3', 'mcp_brave-mcp_brave_web_search', '5 results…'),
        intent('a4', 8, [call('c4', 'write', '{"path":"notes.md","content":"x"}')]),
        result('t4', 9, 'c4', 'write', 'wrote notes.md'),
        intent('a5', 10, [call('c5', 'delegate_agent', '{"agent":"research-agent","task":"market share"}')]),
        { id: 'u-child', seq: 11, role: 'user', content: 'Execute… inside the ward', created_at: NOW },
        intent('a6', 12, [call('c6', 'mcp_brave-mcp_brave_web_search', '{"query":"trendforce"}')]),
        result('t6', 13, 'c6', 'mcp_brave-mcp_brave_web_search', '3 results'),
        result('t5', 14, 'c5', 'delegate_agent', 'done: 3 sources'),
        { id: 'a7', seq: 15, role: 'assistant', content: 'Here is the brief.', created_at: NOW },
      ], next_cursor: 0, truncated: false }) });
  });
  await mockStates(page);
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Workspace navigation' }).locator('.ward-row').click();
  const work = await workTab(page);
  await expect(work.getByRole('button', { name: /Entered ward stock-analysis/ })).toBeVisible();
  const searches = work.getByRole('button', { name: /Searched the web/ });
  await expect(searches).toHaveCount(1); // the merged pair; the child's nests
  await expect(searches.first()).toContainText('2 calls');
  await expect(searches.first()).toContainText('nvda revenue, nvda q3');
  await expect(work.getByRole('button', { name: /Wrote notes.md/ })).toBeVisible();
  // The child's user row is absorbed — ONE turn header, not a bubble.
  await expect(work.locator('.turn-user')).toHaveCount(1);
  const delegated = work.getByRole('button', { name: /Delegated research-agent/ });
  await delegated.click();
  await expect(work.locator('.children')).toContainText('Searched the web');
  await expect(page.getByRole('main').getByLabel('Assistant message')).toContainText('Here is the brief.');
});

