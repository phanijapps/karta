import { test, expect, type Page } from '@playwright/test';

// The Agents settings section lists the daemon roster (read-only v1).

async function mockChat(page: Page, messages: unknown[] = []) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages, next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
}

test('agents section lists the roster with delegation metadata', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/agents', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        agents: [
          { name: 'researcher', display_name: 'Research Agent', description: 'Gathers evidence.', model: 'zai/glm-5.3', tools: ['clock_now'], max_depth: 0, source: '/home/u/.zbot/agents/research.md' },
          { name: 'planner', description: 'Plans work.', max_depth: 2 },
        ],
      }),
    }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Agents', exact: true }).click();
  const list = page.getByLabel('Configured agents');
  await expect(list).toBeVisible();
  await expect(list).toContainText('Research Agent');
  await expect(list).toContainText('Gathers evidence.');
  await expect(list).toContainText('zai/glm-5.3');
  await expect(list).toContainText('delegates');
  await expect(list).toContainText('depth grant 2');
  await expect(list).toContainText('cannot delegate');
  await expect(list).toContainText('clock_now');
});

test('agents empty state points at the directory', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/agents', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ agents: [] }) }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Agents', exact: true }).click();
  await expect(page.getByText('No agents configured')).toBeVisible();
  await expect(page.getByText('~/.zbot/agents/')).toBeVisible();
});
