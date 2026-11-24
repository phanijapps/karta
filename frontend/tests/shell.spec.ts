import { test, expect } from '@playwright/test';

// The chat surface loads /api/chat/messages on mount; every test stubs it.
async function mockChat(page: import('@playwright/test').Page, messages: unknown[] = []) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages, next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
}

import AxeBuilder from '@axe-core/playwright';

async function settings(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  return page.getByRole('dialog', { name: 'Settings', exact: true });
}

test('offline shell, composer basics, no persistence', async ({ page, context }) => {
  test.setTimeout(60000);
  const unexpected: string[] = [];
  // Single handler: chat + models are fulfilled; other API calls aborted;
  // same-origin assets continue; cross-origin is unexpected.
  await page.route('**/*', route => {
    const url = new URL(route.request().url());
    if (url.pathname === '/api/chat/messages' && route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: [], next_cursor: 0, truncated: false }) });
    }
    if (url.pathname === '/api/models') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ provider: 'ollama', id: 'glm-4.7' }, { provider: 'zai', id: 'glm-5.3' }], refreshed_at: { ollama: '2026-11-20T10:00:00Z', zai: '2026-11-20T10:00:00Z' } }) });
    }
    if (url.pathname.startsWith('/api/') || url.pathname === '/healthz') { return route.abort(); }
    if (url.origin !== 'http://127.0.0.1:9245') { unexpected.push(url.pathname); return route.abort(); }
    return route.continue();
  });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'A conversation that stays yours' })).toBeAttached();
  // Composer basics: blank Send disabled, text enables it.
  const input = page.getByRole('textbox', { name: 'Message Karta' });
  await input.fill('  ');
  await expect(page.getByRole('button', { name: 'Send message' })).toBeDisabled();
  await input.fill('hello');
  await expect(page.getByRole('button', { name: 'Send message' })).toBeEnabled();
  // No persistence, no unexpected cross-origin requests.
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0]);
  expect(await page.evaluate(() => indexedDB.databases())).toEqual([]);
  expect(await context.cookies()).toEqual([]);
  expect(unexpected).toEqual([]);
});

test('settings sections, drafts, reset, dismissal and keyboard trap', async ({ page }) => {
  // Deterministic offline daemon: the live Providers view shows its
  // daemon-connection state regardless of what is running locally.
  // Match by path only — a `**/api/*` glob would also swallow Vite's own
  // /src/shared/api/*.ts module URLs and blank the app.
  await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort());
  await mockChat(page);
  await page.goto('/');
  const dialog = await settings(page);
  await expect(dialog).toBeVisible();
  // Providers is a live read-only view; drafts live in the fixture sections.
  await expect(page.getByText('Daemon unreachable.')).toBeVisible();
  await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  const draftField = page.getByRole('textbox', { name: 'Server address' });
  await draftField.fill('Draft only');
  for (const section of ['MCP', 'Skills', 'Logging', 'Advanced', 'Providers']) {
    await dialog.getByRole('button', { name: section, exact: true }).click();
    await expect(dialog.getByRole('heading', { name: section, exact: true })).toBeVisible();
  }
  await expect(draftField).not.toBeVisible();
  await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  await expect(draftField).toHaveValue('Draft only');
  await page.getByRole('button', { name: 'Reset preview' }).click();
  await expect(draftField).toHaveValue('127.0.0.1:9246');
  for (let i = 0; i < 30; i++) { await page.keyboard.press('Tab'); expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true); }
  await draftField.fill('Discard me');
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Settings', exact: true })).toBeFocused();
  await settings(page);
  await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  await expect(draftField).toHaveValue('127.0.0.1:9246');
  await page.mouse.click(2, 2);
  await expect(dialog).toHaveCount(0);
});

test('sidebars, placeholders, responsive transitions and keyboard dismissal', async ({ page }) => {
  // Deterministic catalog for the pickers — this test does not stub the
  // whole daemon surface, so pin the one read the pickers make.
  await page.route('**/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [], refreshed_at: {} }) }),
  );
  await mockChat(page);
  await page.goto('/');
  await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  await expect(page.getByRole('complementary', { name: 'Session context' })).toBeVisible();
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await expect(page.getByRole('button', { name: 'Toggle left sidebar' })).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('complementary', { name: 'Session context' })).toBeVisible();
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  for (const width of [375, 1024, 1440]) {
    await page.setViewportSize({ width, height: 812 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.setViewportSize({ width: 375, height: 812 });
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await expect(page.getByRole('dialog', { name: 'Workspace navigation' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'Toggle left sidebar' })).toBeFocused();
  await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  await expect(page.getByRole('dialog', { name: 'Session context' })).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  await expect(page.getByRole('heading', { name: 'A place for context' })).toHaveCount(0);
});

for (const theme of ['dark', 'light'] as const) {
  test(`accessibility and visible targets in ${theme} theme`, async ({ page }) => {
    // Deterministic offline daemon: Settings opens on the live Providers
    // section, which must not depend on anything running locally.
    await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort());
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await mockChat(page);
  await page.goto('/');
    for (const surface of ['shell', 'settings']) {
      if (surface === 'settings') await settings(page);
      const audit = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
      expect(audit.violations).toEqual([]);
      const undersized = await page.locator('button:visible:not(:disabled)').evaluateAll(elements => elements.filter(el => { const r = el.getBoundingClientRect(); return r.width < 24 || r.height < 24; }).map(el => el.textContent));
      expect(undersized).toEqual([]);
    }
  });
}

test('PWA surface: manifest link, icons, and worker registration script', async ({ page }) => {
  await mockChat(page);
  await page.goto('/');
  // The manifest is linked and servable.
  const link = page.locator('link[rel="manifest"]');
  await expect(link).toHaveAttribute('href', '/manifest.webmanifest');
  const manifest = await page.evaluate(async () => {
    const res = await fetch('/manifest.webmanifest');
    return res.ok ? await res.json() : null;
  });
  if (!manifest || manifest.name !== 'Karta' || manifest.display !== 'standalone') {
    throw new Error(`manifest wrong: ${JSON.stringify(manifest)}`);
  }
  // Icons resolve.
  for (const icon of ['/icon-192.png', '/icon-512.png']) {
    const ok = await page.evaluate(async (src) => (await fetch(src)).ok, icon);
    if (!ok) throw new Error(`icon missing: ${icon}`);
  }
});
