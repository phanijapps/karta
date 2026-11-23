import { test, expect, type Page } from '@playwright/test';

// The Schedules main-area surface: sidebar row between Wards and
// Configuration; CRUD with daemon-validated expressions.

async function mockChat(page: Page) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: [], next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }),
  );
}

test('schedules page lists the roster with cadence and next fire', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/schedules', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({
        schedules: [
          { name: 'digest', schedule: '@daily', goal: 'Summarize the day.', agent: 'research-agent', next_fire: new Date(Date.now() + 3600_000).toISOString(), last_outcome: 'fired' },
        ],
      }),
    });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Schedules' }).click();
  const list = page.getByLabel('Schedules');
  await expect(list).toBeVisible();
  await expect(list).toContainText('digest');
  await expect(list).toContainText('Every day');
  await expect(list).toContainText('Summarize the day.');
  await expect(list).toContainText('research-agent');
  await expect(list).toContainText('in 1h');
  await expect(list).toContainText('last run fired');
});

test('create rejects an invalid expression inline (daemon parse error)', async ({ page }) => {
  await mockChat(page);
  let created = false;
  await page.route('**/api/schedules', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schedules: [] }) });
    }
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as { schedule: string };
      if (body.schedule.includes('bad')) {
        return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ code: 'invalid_schedule', message: 'scheduler: invalid expression "bad": expected 5 to 6 fields' }) });
      }
      created = true;
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ name: 'ok' }) });
    }
    return route.fallback();
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Schedules' }).click();
  await page.getByRole('button', { name: 'New schedule' }).click();
  await page.getByLabel('Name').fill('test-sched');
  await page.getByLabel(/Schedule \(cron\)/).fill('bad expr');
  await page.getByLabel('Goal').fill('Do things.');
  await page.getByRole('button', { name: 'Create' }).click();
  await expect(page.getByRole('alert')).toContainText('invalid expression');
  expect(created).toBe(false);
  // Fix the expression → creates.
  await page.getByLabel(/Schedule \(cron\)/).fill('@daily');
  await page.getByRole('button', { name: 'Create' }).click();
  await expect(page.getByRole('dialog', { name: 'New schedule' })).toHaveCount(0, { timeout: 3000 });
  expect(created).toBe(true);
});

test('empty state teaches the concept', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/schedules', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schedules: [] }) }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Schedules' }).click();
  await expect(page.getByText('No schedules')).toBeVisible();
  await expect(page.getByText(/even while you're away/i)).toBeVisible();
});
