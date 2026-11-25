import { test, expect, type Page } from '@playwright/test';

// The Intent Analysis card (Settings → Advanced) is live: a provider/model
// picker fed by the catalog, a READ-ONLY derivation from the selected
// model (no token knobs), and a Test analysis probe. Route-mocked daemon.

async function mockChat(page: Page) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: [], next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
}

const catalog = {
  models: [
    { provider: 'ollama', id: 'glm5:latest', context_window: 1048576 },
    { provider: 'ollama', id: 'qwen3:32b', context_window: 40960 },
    { provider: 'zai', id: 'glm-5.3-flash' },
    { provider: 'zai', id: 'glm-5.3' },
  ],
  refreshed_at: {},
};

async function mockDaemon(page: Page, opts: { status: Record<string, unknown>; catalog?: unknown; putBodies?: unknown[]; testResult?: unknown; testGoals?: string[] } = { status: {} }) {
  const eff = { model: '', enabled: true, output_reserve: 4000, packet_tokens: 2150, trimming: false, ...opts.status };
  await page.route('**/api/models', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(opts.catalog ?? catalog) }));
  await page.route((url) => url.pathname === '/api/intent-analysis', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ settings: { provider: eff.model ? 'zai' : '', model: eff.model ? 'glm-5.3-flash' : '' }, effective: eff }) });
    }
    opts.putBodies?.push(route.request().postDataJSON());
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ settings: route.request().postDataJSON(), effective: { ...eff, model: 'zai/glm-5.3-flash', context_window: 128000, window_source: 'table' } }) });
  });
  await page.route((url) => url.pathname === '/api/intent-analysis/test', (route) => {
    const body = route.request().postData();
    opts.testGoals?.push(body ? JSON.parse(body).goal ?? '' : '');
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(opts.testResult ?? {
      status: 'analyzed', model: 'zai/glm-5.3-flash', context_window: 128000, window_source: 'table',
      attempts: 2, tokens_in: 2400, tokens_out: 310, duration_ms: 4100, trimmed: false, packet_tokens: 2153,
      analysis: { status: 'analyzed', primary_intent: 'probe-intent', hidden_intents: ['compare energy use at idle', 'note licensing costs'], solution_path: 'benchmark, then write', explanation: 'needs two sources', complexity: 'M', step_budget: 8, approach: 'graph', ward_name: 'ai-agents-research', ward_reason: 'ai research lives here', recommended_skills: ['pdf'], recommended_mcp_tools: ['brave-mcp'], dropped: ['ghost'] },
    }) });
  });
}

test('the Intent Analysis card derives limits read-only, saves the selection, and tests with the cost', async ({ page }) => {
  await mockChat(page);
  const putBodies: unknown[] = [];
  const testGoals: string[] = [];
  await mockDaemon(page, { status: { model: 'zai/glm-5.3-flash', context_window: 128000, window_source: 'table' }, putBodies, testGoals });
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  await dialog.getByRole('button', { name: /Intent Analysis/ }).click();

  // The derivation names the model, window + source, the full-window
  // allowance, the packet estimate, and "no trimming" — nothing editable.
  const derivation = dialog.getByRole('status').filter({ hasText: 'Limits' });
  await expect(derivation).toContainText('zai/glm-5.3-flash');
  await expect(derivation).toContainText('window 128.0k (limits table)');
  await expect(derivation).toContainText('analyst: full window');
  await expect(derivation).toContainText('packet ~2.1k tokens');
  await expect(derivation).toContainText('no trimming');
  const card = dialog.locator('.capability-card', { hasText: 'Intent Analysis' });
  await expect(card.getByRole('spinbutton')).toHaveCount(0); // no token knobs
  await expect(card.getByRole('combobox', { name: 'Provider' })).toHaveValue('zai');
  await expect(card.getByRole('combobox', { name: 'Model' })).toHaveValue('glm-5.3-flash');
  // The picker lists windows when the catalog carries them.
  await card.getByRole('combobox', { name: 'Provider' }).selectOption('ollama');
  await expect(card.getByRole('option', { name: /glm5:latest/ })).toContainText('1.0M');
  await card.getByRole('combobox', { name: 'Model' }).selectOption('qwen3:32b');
  await card.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => putBodies.length).toBe(1);
  expect(putBodies[0]).toEqual({ provider: 'ollama', model: 'qwen3:32b' });
  await expect(card.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

  // Test analysis: the sample goal by default, the decision + its cost.
  await card.getByRole('button', { name: 'Test analysis' }).click();
  await expect.poll(() => testGoals.length).toBe(1);
  expect(testGoals[0]).toBe('');
  const result = card.locator('.test-result');
  await expect(result).toContainText('Analyzed');
  await expect(result).toContainText('2.4k in / 310 out');
  await expect(result).toContainText('4100 ms');
  await expect(result).toContainText('Attempts');
  await expect(result).toContainText('probe-intent · M · graph · ward ai-agents-research');
  await expect(result).toContainText('no trimming');
  // The whole decision, grouped like the chat popover, plus the raw JSON.
  await expect(result.getByRole('region', { name: 'What the analyst saw' }).getByRole('listitem')).toHaveText(['compare energy use at idle', 'note licensing costs']);
  await expect(result).toContainText('needs two sources');
  await expect(result.getByRole('region', { name: 'Solution path' })).toContainText('benchmark, then write');
  await expect(result.getByRole('region', { name: 'Resources' })).toContainText('brave-mcp');
  await expect(result).toContainText('ghost ✕ dropped');
  await expect(result).toContainText('Ward — ai research lives here');
  await expect(result).toContainText('"primary_intent": "probe-intent"'); // the raw JSON (details are open in the accessibility tree)
  // A custom goal rides the body.
  await card.getByRole('textbox', { name: /Goal/ }).fill('Compare AMD and NVIDIA');
  await card.getByRole('button', { name: 'Test analysis' }).click();
  await expect.poll(() => testGoals.length).toBe(2);
  expect(testGoals[1]).toBe('Compare AMD and NVIDIA');
});

test('a disabled kill-switch and an unreachable daemon show state, never fabricated values', async ({ page }) => {
  await mockChat(page);
  await mockDaemon(page, { status: { enabled: false, model: 'zai/glm-5.3-flash' } });
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  const card = dialog.locator('.capability-card', { hasText: 'Intent Analysis' });
  await expect(card.getByText('Disabled')).toBeVisible();
  await card.getByRole('button', { name: /Intent Analysis/ }).click();
  await expect(card.getByRole('status').filter({ hasText: 'Limits' })).toContainText('disabled (config.yaml');
  await expect(card.getByRole('button', { name: 'Test analysis' })).toBeDisabled();
  await page.keyboard.press('Escape');

  // Daemon unreachable: the connection state replaces the card body.
  const dead = await page.context().newPage();
  await mockChat(dead);
  await dead.route('**/api/models', (route) => route.abort());
  await dead.route('**/api/intent-analysis', (route) => route.abort());
  await dead.goto('/');
  await dead.getByRole('button', { name: 'Settings', exact: true }).click();
  const deadDialog = dead.getByRole('dialog', { name: 'Settings', exact: true });
  await deadDialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  const deadCard = deadDialog.locator('.capability-card', { hasText: 'Intent Analysis' });
  await expect(deadCard.getByText('Daemon unreachable')).toBeVisible();
  await deadCard.getByRole('button', { name: /Intent Analysis/ }).click();
  await expect(deadCard.getByRole('status')).toContainText('Daemon unreachable');
  await expect(deadCard.getByRole('button', { name: 'Save changes' })).toHaveCount(0);
  await expect(deadCard.getByRole('combobox')).toHaveCount(0);
});
