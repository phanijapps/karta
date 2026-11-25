import { test, expect } from '@playwright/test';
// Markdown rendering in assistant bubbles: headers/bold/lists/links/
// code/tables render as HTML; the content is SANITIZED (scripts, event
// handlers, javascript: URLs, remote images never survive); user
// bubbles stay verbatim.

const NOW = '2026-11-24T10:00:00Z';

async function mockBase(page: import('@playwright/test').Page) {
  await page.route((url) => url.pathname === '/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ provider: 'zai', id: 'glm-5.3' }], refreshed_at: { zai: NOW } }) }));
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }));
}

test('assistant markdown renders; hostile markup is sanitized; user text is verbatim', async ({ page }) => {
  await mockBase(page);
  const answer = [
    '## Bottom line',
    'AMD is the **higher-beta way** to own the *second* AI-GPU franchise.',
    '- Gross margin 56% vs NVIDIA 74.9%',
    '- forward P/E ~9.9x with +103% growth',
    '',
    '> Size any position accordingly.',
    '',
    'Run `yfinance` for the cross-check; the script is at [the ward](https://example.com/ward).',
    '',
    '| Metric | AMD | NVDA |',
    '|---|---|---|',
    '| GM | 56% | 74.9% |',
    '',
    '```python',
    'print(6*7)',
    '```',
    '',
    '<script>window.__pwned = true</script>',
    '<img src="https://evil.example/pixel.png" onerror="window.__pwned = true">',
    '[bad](javascript:alert(1))',
    '<a href="https://ok.example" onclick="window.__pwned = true">ok link</a>',
  ].join('\n');
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      messages: [
        { id: 'u1', seq: 1, role: 'user', content: 'Is **AMD** a buy? # not a heading', created_at: NOW },
        { id: 'a1', seq: 2, role: 'assistant', content: answer, created_at: NOW },
      ], next_cursor: 0, truncated: false }) });
  });
  await page.route((url) => url.pathname === '/api/sessions', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ sessions: [{ id: 'default', type: 'chat', status: 'completed', updated_at: NOW }], truncated: false }) }));
  await page.goto('/');
  const assistant = page.getByLabel('Assistant message');
  // Rendered structure.
  await expect(assistant.locator('h2')).toHaveText('Bottom line');
  await expect(assistant.locator('strong')).toContainText('higher-beta way');
  await expect(assistant.locator('li')).toHaveCount(2);
  await expect(assistant.locator('blockquote')).toContainText('Size any position');
  await expect(assistant.locator('pre code')).toContainText('print(6*7)');
  await expect(assistant.locator('table th')).toHaveCount(3);
  await expect(assistant.locator('table td')).toHaveCount(3);
  await expect(assistant.getByRole('link', { name: 'the ward' })).toHaveAttribute('href', 'https://example.com/ward');
  // Sanitized.
  await expect(assistant.locator('script')).toHaveCount(0);
  await expect(assistant.locator('img')).toHaveCount(0);
  await expect(assistant.getByRole('link', { name: 'bad' })).toHaveCount(0);
  const ok = assistant.getByRole('link', { name: 'ok link' });
  await expect(ok).toHaveAttribute('href', 'https://ok.example');
  expect(await ok.getAttribute('onclick')).toBeNull();
  expect(await page.evaluate(() => (window as any).__pwned)).toBeUndefined();
  // The user's own words are NOT reinterpreted as markdown.
  const user = page.getByLabel('Your message');
  await expect(user).toHaveText('Is **AMD** a buy? # not a heading');
  await expect(user.locator('strong')).toHaveCount(0);
  await expect(user.locator('h1')).toHaveCount(0);
});
