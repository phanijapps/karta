import { test, expect } from '@playwright/test';

test('integration details and logging/advanced fields are honest previews', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await dialog.getByRole('button', { name: 'Custom endpoint' }).click();
  await expect(dialog.getByRole('textbox', { name: 'Base URL' })).toHaveValue('https://provider.example/v1');
  await expect(dialog.getByRole('button', { name: 'Test connection unavailable' })).toBeDisabled();
  await dialog.getByRole('button', { name: 'MCP', exact: true }).click();
  await dialog.getByRole('button', { name: 'Remote tools' }).click();
  await expect(dialog.getByRole('textbox', { name: 'URL preview' })).toHaveValue('https://mcp.example/mcp');
  await expect(dialog.getByRole('button', { name: 'Connect unavailable' })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Skills', exact: true }).click();
  await dialog.getByRole('button', { name: 'Code review', exact: false }).click();
  await expect(dialog.getByText('~/.zbot/config/skills/code-review/SKILL.md')).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Install skill unavailable' })).toBeDisabled();
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
  await page.setViewportSize({ width: 375, height: 600 });
  await page.goto('/');
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(1);
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

test('collapsed search expands the rail and filters fixture history', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
  await page.getByRole('button', { name: 'Search chats' }).click();
  await page.getByRole('textbox', { name: 'Filter sample chats' }).fill('not in samples');
  await expect(page.getByText('No matching sample chats.')).toBeVisible();
  await page.getByRole('textbox', { name: 'Filter sample chats' }).fill('first release');
  await expect(page.getByRole('button', { name: 'Plan a small, useful first release' })).toBeVisible();
});
