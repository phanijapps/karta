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


test('Karta identity is consistent without changing compatibility paths', async ({ page }) => {
  await mockChat(page);
  await page.goto('/');
  await expect(page).toHaveTitle('Karta — Local workspace');
  await expect(page.locator('meta[name="description"]')).toHaveAttribute('content', 'Karta — your local agent workspace. Shell preview.');
  const wordmark = page.locator('.wordmark');
  await expect(wordmark).toHaveText('Karta');
  await expect(page.getByRole('textbox', { name: 'Message Karta' })).toBeVisible();
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await expect(wordmark.locator('.brand-text')).toBeHidden();
  await expect(wordmark).toBeVisible();
  await expect(wordmark).toHaveText('Karta'); // The hidden suffix preserves a single wordmark component.
  await expect(page.getByRole('navigation', { name: 'Workspace navigation' }).getByText('Karta', { exact: true }).last()).toBeAttached();
  await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort());
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await expect(dialog.getByText('Karta', { exact: true })).toBeVisible();
  // Compatibility-path labeling: config-origin providers name their source file.
  await dialog.getByRole('button', { name: 'Advanced', exact: true }).click();
  await expect(dialog.getByText('~/.zbot/config.yaml', { exact: false }).first()).toBeVisible();
  await expect(page.getByText('z-Bot', { exact: true })).toHaveCount(0);
});
