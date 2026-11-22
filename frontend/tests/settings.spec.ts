import { test, expect, type Page } from '@playwright/test';

// The chat surface loads /api/chat/messages on mount; every test stubs it.
async function mockChat(page: import('@playwright/test').Page, messages: unknown[] = []) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages, next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
}


// Route-mocked daemon: the Settings Providers section is live, so tests
// stub the provider endpoints instead of asserting fixtures.
async function mockProviders(page: Page, providers: unknown[]) {
  await page.route('**/api/providers', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ providers }),
    }),
  );
}

const liveProviders = [
  { name: 'ollama', type: 'openai-compatible', base_url: 'http://localhost:11434/v1', credential_configured: false },
  { name: 'zai', type: 'openai-compatible', base_url: 'https://api.z.ai/v1', api_key_env: 'ZAI_API_KEY', credential_configured: true },
];

const mockServers = [
  { name: 'github', file: '/home/u/.zbot/config/mcps/github.yaml', transports: [
    { kind: 'stdio', command: 'gh-mcp', args: ['serve'] },
    { kind: 'http', url: 'https://mcp.example.com/mcp' },
  ] },
];
const mockSkills = [
  { name: 'Code review', description: 'Review a change for correctness.', directory: '/home/u/.zbot/config/skills/code-review' },
];

test('integration details and logging/advanced fields are honest previews', async ({ page }) => {
  await mockProviders(page, liveProviders);
  await page.route('**/api/mcps', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers: mockServers }) }),
  );
  await page.route('**/api/skills', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: mockSkills }) }),
  );
  await page.route('**/api/skills/Code%20review', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...mockSkills[0], body: '# Code review\n\nChecklist…' }) }),
  );
  await mockChat(page);
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  // MCP and Skills are live managed views: daemon data, no mock badges.
  await dialog.getByRole('button', { name: 'MCP', exact: true }).click();
  await dialog.getByRole('button', { name: /github/ }).click();
  await expect(dialog.getByRole('textbox', { name: 'Command' })).toHaveValue('gh-mcp serve');
  await expect(dialog.getByRole('textbox', { name: 'HTTP URL' })).toHaveValue('https://mcp.example.com/mcp');
  await expect(dialog.getByText('Mock')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Test connection' })).toBeEnabled();
  // The detail popup covers the nav — close it before switching sections.
  await page.getByRole('button', { name: 'Close dialog' }).click();
  await dialog.getByRole('button', { name: 'Skills', exact: true }).click();
  await dialog.getByRole('button', { name: /Code review/ }).click();
  await expect(dialog.getByText('/home/u/.zbot/config/skills/code-review')).toBeVisible();
  await expect(dialog.getByRole('textbox', { name: 'SKILL.md body' })).toHaveValue('# Code review\n\nChecklist…');
  // Logging/Advanced remain honest shell previews with drafts.
  await dialog.getByRole('button', { name: 'Logging', exact: true }).click();
  await dialog.getByRole('combobox', { name: 'Log level' }).selectOption('Debug');
  await dialog.getByRole('checkbox', { name: 'Detailed diagnostics preview' }).check();
  await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  await dialog.getByRole('textbox', { name: 'Server address' }).fill('127.0.0.1:9999');
  await dialog.getByRole('spinbutton', { name: 'Max context tokens' }).fill('64000');
  await dialog.getByRole('button', { name: 'Logging', exact: true }).click();
  await expect(dialog.getByRole('combobox', { name: 'Log level' })).toHaveValue('Debug');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await dialog.getByRole('button', { name: 'Logging', exact: true }).click();
  await expect(dialog.getByRole('combobox', { name: 'Log level' })).toHaveValue('Info');
});

test('narrow settings, internal scroll and focus recovery from drawer', async ({ page }) => {
  // Deterministic offline daemon for the live Providers section.
  await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort());
  await page.setViewportSize({ width: 375, height: 600 });
  await mockChat(page);
  await page.goto('/');
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(1);
  // The footer (drafts/reset) exists only on fixture-backed sections; the
  // default Providers section is a live read-only view.
  await page.getByRole('button', { name: 'Advanced', exact: true }).click();
  await page.getByRole('button', { name: 'Reset preview' }).scrollIntoViewIfNeeded();
  await expect(page.getByRole('button', { name: 'Reset preview' })).toBeInViewport();
  for (const name of ['Advanced', 'Logging', 'Skills', 'MCP', 'Providers']) {
    await page.getByRole('button', { name, exact: true }).click();
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  }
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'Toggle left sidebar' })).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('sidebar shows the single persistent conversation', async ({ page }) => {
  await mockChat(page);
  await page.goto('/');
  await expect(page.getByText('One persistent conversation').first()).toBeVisible();
  await expect(page.getByText('lives here forever')).toBeVisible();
});
