import { test, expect, type Page } from '@playwright/test';

// The header model picker is catalog-backed: route-mocked, no daemon.

async function mockCatalog(page: Page, models: unknown[], refreshed: Record<string, string> = {}) {
  await page.route('**/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models, refreshed_at: refreshed }) }),
  );
}

test('picker groups catalog models by provider and selects in memory', async ({ page }) => {
  await mockCatalog(page, [
    { provider: 'ollama', id: 'glm-4.7' },
    { provider: 'ollama', id: 'qwen:7b' },
    { provider: 'zai', id: 'glm-5.3' },
  ]);
  await page.goto('/');
  const header = page.getByRole('combobox', { name: 'Header model' });
  await expect(header.locator('optgroup[label="ollama"] option')).toHaveCount(2);
  await expect(header.locator('optgroup[label="zai"] option')).toHaveCount(1);
  await header.selectOption('zai/glm-5.3');
  await expect(header).toHaveValue('zai/glm-5.3');
  // Honest labeling: replies stay simulated until the engine lands.
  await expect(page.getByText('Simulated', { exact: true }).first()).toBeVisible();
});

test('empty catalog shows an explicit state, never fixture models', async ({ page }) => {
  await mockCatalog(page, []);
  await page.goto('/');
  await expect(page.getByRole('combobox', { name: 'Header model' })).toBeDisabled();
  await expect(page.getByRole('combobox', { name: 'Header model' })).toContainText('No models yet');
});

test('daemon unreachable picker state', async ({ page }) => {
  await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort());
  await page.goto('/');
  await expect(page.getByRole('combobox', { name: 'Header model' })).toBeDisabled();
  await expect(page.getByRole('combobox', { name: 'Header model' })).toContainText('Daemon unreachable');
});

test('settings refresh action updates the catalog the picker reads', async ({ page }) => {
  let models = [{ provider: 'ollama', id: 'old-model' }];
  // Live closure so the catalog read sees the refresh's reassignment.
  await page.route('**/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models, refreshed_at: {} }) }),
  );
  await page.route('**/api/providers/ollama/models/refresh', (route) => {
    models = [{ provider: 'ollama', id: 'fresh-model' }];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ id: 'fresh-model' }] }) });
  });
  await page.route('**/api/providers', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers: [{ name: 'ollama', type: 'openai-compatible', base_url: 'http://localhost:11434/v1', credential: 'none', credential_status: 'unset', origin: 'config', credential_configured: false }] }) }),
  );
  await page.goto('/');
  await expect(page.getByRole('combobox', { name: 'Header model' })).toContainText('old-model');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await dialog.getByRole('button', { name: 'Refresh models' }).click();
  await page.getByRole('heading', { name: /Connection OK — models \(1\)/ }).waitFor();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('combobox', { name: 'Header model' })).toContainText('fresh-model');
});

test('selection preserves provider identity across duplicate ids', async ({ page }) => {
  await mockCatalog(page, [
    { provider: 'ollama', id: 'shared-model' },
    { provider: 'zai', id: 'shared-model' },
  ]);
  await page.goto('/');
  const header = page.getByRole('combobox', { name: 'Header model' });
  await header.selectOption('ollama/shared-model');
  await expect(header).toHaveValue('ollama/shared-model');
  await header.selectOption('zai/shared-model');
  await expect(header).toHaveValue('zai/shared-model');
});

test('stale selection clears when a refresh removes the model', async ({ page }) => {
  let models = [{ provider: 'ollama', id: 'old-model' }];
  await page.route('**/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models, refreshed_at: {} }) }),
  );
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers: [{ name: 'ollama', type: 'openai-compatible', base_url: 'http://localhost:11434/v1', credential: 'none', credential_status: 'unset', origin: 'config', credential_configured: false }] }) });
    }
    return route.fallback();
  });
  await page.route('**/api/providers/ollama/models/refresh', (route) => {
    models = [{ provider: 'ollama', id: 'fresh-model' }];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ id: 'fresh-model' }] }) });
  });
  await page.goto('/');
  const header = page.getByRole('combobox', { name: 'Header model' });
  await header.selectOption('ollama/old-model');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('dialog', { name: 'Settings', exact: true }).getByRole('button', { name: 'Refresh models' }).click();
  await page.keyboard.press('Escape');
  // The removed model's selection clears to the placeholder, not a
  // silent mismatch between displayed option and bound state.
  await expect(header).toHaveValue('');
});

test('refresh typed failure renders guidance and keeps the catalog', async ({ page }) => {
  await mockCatalog(page, [{ provider: 'ollama', id: 'existing' }]);
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers: [{ name: 'ollama', type: 'openai-compatible', base_url: 'http://localhost:11434/v1', credential: 'none', credential_status: 'unset', origin: 'config', credential_configured: false }] }) });
    }
    return route.fallback();
  });
  await page.route('**/api/providers/ollama/models/refresh', (route) =>
    route.fulfill({ status: 502, contentType: 'application/json', body: JSON.stringify({ code: 'provider_failure', message: 'dial refused', reason: 'unreachable' }) }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await dialog.getByRole('button', { name: 'Refresh models' }).click();
  await expect(dialog.getByRole('heading', { name: 'Connection failed' })).toBeVisible();
  await expect(dialog.getByText(/Could not reach the provider/)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('combobox', { name: 'Header model' })).toContainText('existing');
});
