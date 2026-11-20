import { test, expect, type Page } from '@playwright/test';

// The Providers section is a live daemon view with CRUD: every test stubs
// the provider endpoints (route mocks) — no live daemon or keyring in the
// test run.

const configProvider = { name: 'ollama', type: 'openai-compatible', base_url: 'http://localhost:11434/v1', credential: 'none', credential_status: 'unset', origin: 'config', credential_configured: false };
const managedProvider = { name: 'zai', type: 'openai-compatible', base_url: 'https://api.z.ai/v1', api_key_env: 'ZAI_API_KEY', credential: 'env', credential_status: 'ready', origin: 'file', credential_configured: true };

async function mockProviderList(page: Page, providers: unknown[]) {
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers }) });
    }
    return route.fallback();
  });
}

async function openSettings(page: Page) {
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  return page.getByRole('dialog', { name: 'Settings', exact: true });
}

test('lists providers with origins, credential status, and no secret values', async ({ page }) => {
  await mockProviderList(page, [configProvider, managedProvider]);
  const dialog = await openSettings(page);
  await expect(dialog.getByRole('button', { name: /ollama/ })).toContainText('config.yaml');
  await expect(dialog.getByRole('button', { name: /zai/ })).toContainText('Managed');
  await expect(dialog.getByRole('button', { name: /zai/ })).toContainText('Credential set');
  await dialog.getByRole('button', { name: /zai/ }).click();
  await expect(dialog.getByRole('textbox', { name: 'Base URL' })).toHaveValue('https://api.z.ai/v1');
  // Edit/delete only for managed providers; config origin stays read-only.
  await expect(dialog.getByRole('button', { name: 'Edit', exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: /ollama/ }).click();
  await expect(dialog.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Delete', exact: true })).toHaveCount(0);
  for (const name of ['Provider name', 'Base URL']) {
    await expect(dialog.getByRole('textbox', { name })).not.toBeEditable();
  }
});

test('test connection lists discovered models', async ({ page }) => {
  await mockProviderList(page, [configProvider]);
  await page.route('**/api/providers/ollama/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ id: 'glm-5.3' }, { id: 'qwen:7b' }] }) }),
  );
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Test connection' }).click();
  await expect(dialog.getByRole('heading', { name: /Connection OK — models \(2\)/ })).toBeVisible();
  await expect(dialog.getByText('glm-5.3')).toBeVisible();
});

test('test connection renders each typed failure with guidance', async ({ page }) => {
  const cases: Array<{ status: number; body: unknown; guidance: RegExp }> = [
    { status: 502, body: { code: 'provider_failure', message: 'dial refused', reason: 'unreachable' }, guidance: /Could not reach the provider/ },
    { status: 502, body: { code: 'provider_failure', message: 'answered 401', reason: 'unauthorized' }, guidance: /rejected the credential/ },
    { status: 502, body: { code: 'provider_failure', message: 'bad body', reason: 'malformed' }, guidance: /model list could not be read/ },
    { status: 502, body: { code: 'provider_failure', message: 'locked', reason: 'credential_unavailable' }, guidance: /OS keyring/ },
    { status: 404, body: { code: 'not_found', message: 'unknown provider' }, guidance: /no longer in the daemon/ },
  ];
  for (const testCase of cases) {
    await mockProviderList(page, [configProvider]);
    await page.route('**/api/providers/ollama/models', (route) =>
      route.fulfill({ status: testCase.status, contentType: 'application/json', body: JSON.stringify(testCase.body) }),
    );
    const dialog = await openSettings(page);
    await dialog.getByRole('button', { name: 'Test connection' }).click();
    await expect(dialog.getByRole('heading', { name: 'Connection failed' })).toBeVisible();
    await expect(dialog.getByText(testCase.guidance)).toBeVisible();
    await page.keyboard.press('Escape');
  }
});

test('add provider succeeds and refreshes the list', async ({ page }) => {
  let providers = [configProvider];
  // One live route: GET reflects the POST's reassignment.
  await page.route('**/api/providers', async (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers }) });
    }
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON();
      providers = [...providers, { ...body, type: 'openai-compatible', credential: body.credential?.kind ?? 'none', credential_status: body.credential?.kind === 'keyring' ? 'ready' : 'unset', origin: 'file', credential_configured: body.credential?.kind === 'keyring' }];
      return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(providers[providers.length - 1]) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Add provider' }).first().click();
  const form = dialog.getByRole('form', { name: 'Add provider' });
  await form.getByRole('textbox', { name: 'Provider name' }).fill('zai-prod');
  await form.getByRole('textbox', { name: 'Base URL' }).fill('https://api.z.ai/v1');
  await form.getByLabel('Store an API key in the OS keyring').check();
  await form.getByRole('textbox', { name: 'API key' }).fill('one-shot-key');
  await dialog.getByRole('button', { name: 'Create provider' }).click();
  await expect(dialog.getByRole('button', { name: /zai-prod/ })).toBeVisible();
  await expect(dialog.getByRole('button', { name: /zai-prod/ })).toContainText('Credential set');
  // The key value is never echoed back into the UI.
  await expect(dialog.getByText('one-shot-key')).toHaveCount(0);
});

test('duplicate create keeps the draft editable with guidance', async ({ page }) => {
  await mockProviderList(page, [configProvider]);
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'duplicate_name', message: 'name already exists' }) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Add provider' }).first().click();
  const form = dialog.getByRole('form', { name: 'Add provider' });
  await form.getByRole('textbox', { name: 'Provider name' }).fill('ollama');
  await form.getByRole('textbox', { name: 'Base URL' }).fill('http://localhost:11434/v1');
  await dialog.getByRole('button', { name: 'Create provider' }).click();
  await expect(dialog.getByText(/already exists/)).toBeVisible();
  await expect(form.getByRole('textbox', { name: 'Provider name' })).toHaveValue('ollama');
});

test('keyring-unavailable create keeps the draft with guidance', async ({ page }) => {
  await mockProviderList(page, []);
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ code: 'keyring_unavailable', message: 'keyring down' }) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Add provider' }).click();
  const form = dialog.getByRole('form', { name: 'Add provider' });
  await form.getByRole('textbox', { name: 'Provider name' }).fill('zai');
  await form.getByRole('textbox', { name: 'Base URL' }).fill('https://api.z.ai/v1');
  await dialog.getByRole('button', { name: 'Create provider' }).click();
  await expect(dialog.getByText(/keyring is unavailable/i)).toBeVisible();
  await expect(form.getByRole('textbox', { name: 'Provider name' })).toHaveValue('zai');
});

test('edit sends PATCH with explicit credential semantics and refreshes', async ({ page }) => {
  let providers = [managedProvider];
  let patchBody: unknown;
  // Live closure for the list so the edit's reassignment is visible.
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers }) });
    }
    return route.fallback();
  });
  await page.route('**/api/providers/zai', async (route) => {
    if (route.request().method() === 'PATCH') {
      patchBody = route.request().postDataJSON();
      providers = [{ ...managedProvider, base_url: 'https://api.z.ai/v2' }];
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(providers[0]) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Edit', exact: true }).click();
  const form = dialog.getByRole('form', { name: 'Edit provider' });
  await form.getByRole('textbox', { name: 'Base URL' }).fill('https://api.z.ai/v2');
  // Credential radio stays on env with the existing name: the edit sends
  // the explicit credential object (same reference) plus the new URL.
  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect(form).toHaveCount(0); // form closes on success
  await expect(dialog.getByRole('textbox', { name: 'Base URL' })).toHaveValue('https://api.z.ai/v2');
  expect(patchBody).toEqual({ base_url: 'https://api.z.ai/v2', credential: { kind: 'env', env_name: 'ZAI_API_KEY' } });
});

test('delete requires confirmation naming the credential, then refreshes', async ({ page }) => {
  let providers = [managedProvider];
  // Live closure: the list route must see the delete's reassignment.
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers }) });
    }
    return route.fallback();
  });
  await page.route('**/api/providers/zai', async (route) => {
    if (route.request().method() === 'DELETE') {
      providers = [];
      return route.fulfill({ status: 204 });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog.getByRole('alertdialog', { name: 'Delete provider' })).toContainText('zai');
  await dialog.getByRole('button', { name: 'Delete', exact: true }).last().click();
  await expect(dialog.getByText('No providers configured.')).toBeVisible();
});

test('daemon unreachable shows an explicit state, never fixture providers', async ({ page }) => {
  await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort());
  const dialog = await openSettings(page);
  await expect(dialog.getByText('Daemon unreachable.')).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Retry' })).toBeVisible();
  await expect(dialog.getByText('Local provider')).toHaveCount(0);
});

test('empty provider list explains how to start', async ({ page }) => {
  await mockProviderList(page, []);
  const dialog = await openSettings(page);
  await expect(dialog.getByText('No providers configured.')).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Add provider' })).toBeVisible();
});

test('reserved device names are blocked client-side with the rule named', async ({ page }) => {
  await mockProviderList(page, []);
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Add provider' }).click();
  const form = dialog.getByRole('form', { name: 'Add provider' });
  await form.getByRole('textbox', { name: 'Provider name' }).fill('con');
  await form.getByRole('textbox', { name: 'Base URL' }).fill('http://localhost:11434/v1');
  await form.getByRole('button', { name: 'Create provider' }).click();
  await expect(form.getByText(/reserved device name/i).first()).toBeVisible();
  await expect(form.getByRole('textbox', { name: 'Provider name' })).toHaveValue('con');
});

test('blank keyring key on create is blocked with guidance', async ({ page }) => {
  await mockProviderList(page, []);
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Add provider' }).click();
  const form = dialog.getByRole('form', { name: 'Add provider' });
  await form.getByRole('textbox', { name: 'Provider name' }).fill('zai');
  await form.getByRole('textbox', { name: 'Base URL' }).fill('https://api.z.ai/v1');
  await form.getByLabel('Store an API key in the OS keyring').check();
  await form.getByRole('button', { name: 'Create provider' }).click();
  await expect(form.getByText(/Enter the API key to store/i)).toBeVisible();
});

test('keyring-unavailable create with a key entered keeps the draft', async ({ page }) => {
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers: [] }) });
    }
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ code: 'keyring_unavailable', message: 'keyring down' }) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Add provider' }).click();
  const form = dialog.getByRole('form', { name: 'Add provider' });
  await form.getByRole('textbox', { name: 'Provider name' }).fill('zai');
  await form.getByRole('textbox', { name: 'Base URL' }).fill('https://api.z.ai/v1');
  await form.getByLabel('Store an API key in the OS keyring').check();
  await form.getByRole('textbox', { name: 'API key' }).fill('a-real-key');
  await form.getByRole('button', { name: 'Create provider' }).click();
  await expect(form.getByText(/keyring is unavailable/i)).toBeVisible();
  await expect(form.getByRole('textbox', { name: 'API key' })).toHaveValue('a-real-key');
});

test('create at capacity conflicts with typed guidance', async ({ page }) => {
  await page.route('**/api/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ providers: [] }) });
    }
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'provider_limit', message: 'list is full' }) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page);
  await dialog.getByRole('button', { name: 'Add provider' }).click();
  const form = dialog.getByRole('form', { name: 'Add provider' });
  await form.getByRole('textbox', { name: 'Provider name' }).fill('one-more');
  await form.getByRole('textbox', { name: 'Base URL' }).fill('http://x:1/v1');
  await form.getByRole('button', { name: 'Create provider' }).click();
  await expect(form.getByText(/provider list is full/i)).toBeVisible();
});
