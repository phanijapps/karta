import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function settings(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  return page.getByRole('dialog', { name: 'Settings', exact: true });
}

test('offline shell, conversation isolation, composer and model preview', async ({ page, context }) => {
  const unexpected: string[] = [];
  await page.route('**/*', route => {
    const url = new URL(route.request().url());
    if (url.origin !== 'http://127.0.0.1:9245' || url.pathname.startsWith('/api/') || url.pathname === '/healthz') { unexpected.push(url.pathname); return route.abort(); }
    return route.continue();
  });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'A workspace that stays yours' })).toBeAttached();
  await page.getByRole('button', { name: 'Plan a small, useful first release' }).click();
  await expect(page.getByText('Pick one useful outcome', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'What’s on your mind?' })).toBeVisible();
  const input = page.getByRole('textbox', { name: 'Message Karta' });
  await input.fill('  ');
  await expect(page.getByRole('button', { name: 'Send message' })).toBeDisabled();
  await input.fill('First line'); await input.press('Shift+Enter'); await input.press('a');
  await expect(input).toHaveValue('First line\na');
  await input.dispatchEvent('keydown', { key: 'Enter', isComposing: true });
  await expect(input).toHaveValue('First line\na');
  await input.fill('<script>window.bad=true</script>'); await input.press('Enter');
  await expect(page.getByRole('article', { name: 'Your message' }).getByText('<script>window.bad=true</script>', { exact: true })).toBeVisible();
  await expect(page.getByText('This is a simulated reply.', { exact: false })).toBeVisible();
  await expect(input).toBeFocused();
  expect(await page.evaluate(() => 'bad' in window)).toBe(false);
  await page.getByRole('combobox', { name: 'Header mock model' }).selectOption('Fast preview');
  await expect(page.getByRole('combobox', { name: 'Mock model', exact: true })).toHaveValue('Fast preview');
  await page.getByRole('button', { name: 'A workspace that stays yours' }).click();
  await expect(page.getByRole('article', { name: 'Your message' }).getByText('<script>window.bad=true</script>', { exact: true })).toHaveCount(0);
  await settings(page);
  await page.getByRole('textbox', { name: 'Provider name' }).fill('Unsaved provider');
  await page.getByRole('button', { name: 'Close Settings' }).click();
  await page.getByRole('button', { name: 'Switch to light theme' }).click();
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0]);
  expect(await page.evaluate(() => indexedDB.databases())).toEqual([]);
  expect(await context.cookies()).toEqual([]);
  expect(unexpected).toEqual([]);
  await page.reload();
  await expect(page.getByRole('button', { name: '<script>window.bad=true</script>' })).toHaveCount(0);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await settings(page);
  await expect(page.getByRole('textbox', { name: 'Provider name' })).toHaveValue('Local provider');
});

test('settings sections, drafts, reset, dismissal and keyboard trap', async ({ page }) => {
  await page.goto('/');
  const dialog = await settings(page);
  await expect(dialog).toBeVisible();
  await page.getByRole('textbox', { name: 'Provider name' }).fill('Draft only');
  for (const section of ['MCP', 'Skills', 'Logging', 'Advanced', 'Providers']) {
    await dialog.getByRole('button', { name: section, exact: true }).click();
    await expect(dialog.getByRole('heading', { name: section, exact: true })).toBeVisible();
  }
  await expect(page.getByRole('textbox', { name: 'Provider name' })).toHaveValue('Draft only');
  await page.getByRole('button', { name: 'Reset preview' }).click();
  await expect(page.getByRole('textbox', { name: 'Provider name' })).toHaveValue('Local provider');
  for (let i = 0; i < 30; i++) { await page.keyboard.press('Tab'); expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true); }
  await page.getByRole('textbox', { name: 'Provider name' }).fill('Discard me');
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Settings', exact: true })).toBeFocused();
  await settings(page);
  await expect(page.getByRole('textbox', { name: 'Provider name' })).toHaveValue('Local provider');
  await page.mouse.click(2, 2);
  await expect(dialog).toHaveCount(0);
});

test('sidebars, placeholders, responsive transitions and keyboard dismissal', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Toggle right sidebar' }).click();
  await expect(page.getByRole('heading', { name: 'A place for context' })).toBeVisible();
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await expect(page.getByRole('button', { name: 'Toggle left sidebar' })).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('heading', { name: 'A place for context' })).toBeVisible();
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await page.getByRole('button', { name: 'Code Wards', exact: false }).click();
  await expect(page.getByRole('heading', { name: 'Code Wards' })).toBeVisible();
  await page.getByRole('button', { name: 'Back to chat' }).click();
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
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
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
