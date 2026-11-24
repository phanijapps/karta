import { test, expect, type Page } from '@playwright/test';

// The Wards main-area surface: sidebar row (replacing the old Code Wards
// placeholder), roster cards with lint states, detail panel, and the
// empty state pointing at the vault.

async function mockChat(page: Page) {
  // Catch-all guard FIRST (Playwright consults the latest-registered
  // route first): any UNMOCKED /api request fails the test loudly
  // instead of reaching a local daemon through the Vite proxy. The
  // specific mocks below are registered after, so they win.
  await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort('failed'));
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: [], next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }),
  );
  // The header ModelPicker reads the catalog — mock it so these tests
  // never leak traffic to a real daemon through the Vite proxy.
  await page.route((url) => url.pathname === '/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [], refreshed_at: {} }) }),
  );
}

test('wards page lists the roster with lint states and canonical titles', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        vault_root: '/home/dev/Documents/wards',
        wards: [
          { name: 'amdfinancials', archetype: 'research', lint_status: 'clean', finding_count: 0, canonical_title: 'AMD financials' },
          { name: 'quant-strategy', archetype: 'research', lint_status: 'violations', finding_count: 2 },
          { name: 'scraps', archetype: 'generic', lint_status: 'clean', finding_count: 0, canonical_title: 'Scraps' },
        ],
      }),
    }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  const list = page.getByLabel('Wards');
  await expect(list).toBeVisible();
  const clean = list.locator('li', { hasText: 'amdfinancials' });
  await expect(clean.getByText('clean', { exact: true })).toBeVisible();
  await expect(clean).toContainText('AMD financials');
  const dirty = list.locator('li', { hasText: 'quant-strategy' });
  await expect(dirty.getByText('2 findings')).toBeVisible();
  await expect(list).toContainText('scraps');
  await expect(list).toContainText('generic');
  await expect(page.getByText('Vault: /home/dev/Documents/wards')).toBeVisible();
});

test('navigation is mutually exclusive: the chat surface replaces Wards', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ vault_root: '/v', wards: [{ name: 'a', archetype: 'research', lint_status: 'clean', finding_count: 0 }] }) }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  await expect(page.getByRole('heading', { name: 'Wards' })).toBeVisible();
  await page.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: /Quick Chat/ }).click();
  await expect(page.getByRole('heading', { name: 'Wards' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Send message' })).toBeVisible();
});

test('a slow earlier detail response never overwrites the selected ward', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ vault_root: '/v', wards: [
        { name: 'slow', archetype: 'research', lint_status: 'clean', finding_count: 0 },
        { name: 'fast', archetype: 'generic', lint_status: 'clean', finding_count: 0 },
      ] }),
    }),
  );
  const detail = (name: string) => ({ name, archetype: 'research', lint_status: 'clean', finding_count: 0, findings: [], layout: { kind: 'directory', children: [{ id: `${name}-node`, kind: 'file', match: `${name}.md` }] } });
  await page.route('**/api/wards/slow', async (route) => {
    await new Promise((r) => setTimeout(r, 400));
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(detail('slow')) });
  });
  await page.route('**/api/wards/fast', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(detail('fast')) }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  await page.getByRole('button', { name: /Open ward slow/ }).click();
  await page.getByRole('button', { name: /Open ward fast/ }).click();
  const detailPanel = page.locator('.ward-detail');
  await expect(detailPanel).toContainText('fast-node');
  // Give the slow response every chance to clobber — it must not land.
  await page.waitForTimeout(600);
  await expect(detailPanel).toContainText('fast-node');
  await expect(detailPanel).not.toContainText('slow-node');
});

test('opening a ward shows findings and the layout tree', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        vault_root: '/home/dev/Documents/wards',
        wards: [{ name: 'quant-strategy', archetype: 'research', lint_status: 'violations', finding_count: 1 }],
      }),
    }),
  );
  await page.route('**/api/wards/quant-strategy', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        name: 'quant-strategy',
        archetype: 'research',
        lint_status: 'violations',
        finding_count: 1,
        findings: [{ kind: 'missing_required', node_id: 'canonical', path: 'quant-strategy.md', message: 'required file "quant-strategy.md" is missing (match "{ward}.md")' }],
        layout: {
          kind: 'directory',
          children: [
            { id: 'canonical', kind: 'file', match: '{ward}.md', format: 'markdown' },
            { id: 'tasks', kind: 'directory', match: 'tasks', required: false },
          ],
        },
      }),
    }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  await page.getByRole('button', { name: /Open ward quant-strategy/ }).click();
  const detail = page.locator('.ward-detail');
  await expect(detail).toBeVisible();
  await expect(detail).toContainText('missing');
  await expect(detail).toContainText('canonical');
  await expect(detail).toContainText('quant-strategy.md');
  await expect(detail.locator('.layout')).toContainText('file  canonical');
  await expect(detail.locator('.layout')).toContainText('dir  tasks');
});

test('empty state points at the vault path and the override hint', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ vault_root: '/home/dev/Documents/wards', wards: [] }),
    }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  const empty = page.getByLabel('Wards empty state');
  await expect(empty).toBeVisible();
  await expect(empty).toContainText('/home/dev/Documents/wards');
  await expect(empty).toContainText('wards.vault_root');
  await expect(empty).toContainText('ward-conf.yaml');
});

test('sidebar row is Wards, not the retired Code Wards placeholder', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ vault_root: '/v', wards: [] }) }),
  );
  await page.goto('/');
  const row = page.getByRole('button', { name: 'Wards', exact: true });
  await expect(row).toBeVisible();
  await expect(page.getByText('Code Wards')).toHaveCount(0);
  await expect(page.getByText('TODO', { exact: true })).toHaveCount(0); // no placeholder badges remain
});

test('list failure shows an error with retry, not an empty vault', async ({ page }) => {
  await mockChat(page);
  let failing = true;
  await page.route('**/api/wards', (route) => {
    if (failing) return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 'internal', message: 'vault io' }) });
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ vault_root: '/v', wards: [{ name: 'recovered', archetype: 'research', lint_status: 'clean', finding_count: 0 }] }) });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  const alert = page.getByRole('alert');
  await expect(alert).toContainText("Couldn't read the ward vault");
  await expect(page.getByText('No wards yet')).toHaveCount(0);
  failing = false;
  await page.getByRole('button', { name: 'Retry' }).click();
  await expect(page.getByLabel('Wards')).toContainText('recovered');
});

test('detail failure shows an error with retry, not endless loading', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ vault_root: '/v', wards: [{ name: 'quant-strategy', archetype: 'research', lint_status: 'violations', finding_count: 1 }] }) }),
  );
  await page.route('**/api/wards/quant-strategy', (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ code: 'not_found', message: 'no ward named quant-strategy' }) }));
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  await page.getByRole('button', { name: /Open ward quant-strategy/ }).click();
  const detail = page.locator('.ward-detail');
  await expect(detail.getByRole('alert')).toContainText("Couldn't read this ward");
  await expect(detail).not.toContainText('Loading ward');
});

test('an unparseable conf is not described as a freeform ward', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/wards', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ vault_root: '/v', wards: [{ name: 'broken', archetype: 'unknown', lint_status: 'violations', finding_count: 1 }] }) }),
  );
  await page.route('**/api/wards/broken', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      name: 'broken', archetype: 'unknown', lint_status: 'violations', finding_count: 1,
      findings: [{ kind: 'invalid_conf', message: 'ward-conf.yaml: unknown field zzz' }],
    }) }));
  await page.goto('/');
  await page.getByRole('button', { name: 'Wards' }).click();
  await page.getByRole('button', { name: /Open ward broken/ }).click();
  const detail = page.locator('.ward-detail');
  await expect(detail).toContainText('failed to parse');
  await expect(detail).toContainText('invalid conf');
  await expect(detail).not.toContainText('freeform ward');
});
