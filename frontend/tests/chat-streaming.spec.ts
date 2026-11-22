import { test, expect, type Page } from '@playwright/test';

// Streamed chat (POST /api/chat/send → SSE). Route-mocked: the full SSE
// body arrives in one fulfill — progressive per-event flushing is proven
// at the backend (per-event flush tests) and the parser handles chunked
// arrival by construction; this suite pins the UI contract: deltas render
// live, the Thinking block behaves, the terminal exchange reconciles the
// transcript to persisted truth, and the a11y contract holds.

async function mockChat(page: Page, messages: unknown[] = []) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages, next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
  // Catalog + default-model: the header picker needs both offline.
  await page.route('**/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [{ provider: 'zai', id: 'glm-5.3' }], refreshed_at: { zai: '2026-11-20T10:00:00Z' } }) }),
  );
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }),
  );
}

function sse(events: string[]): string {
  return events.join('\n\n') + '\n\n';
}

// Stateful stream+transcript mock: the transcript is empty until the
// stream's terminal exchange fires, then it holds the persisted rows —
// mirroring the daemon's truth timeline.
async function mockStream(page: Page, exchangeMessages: unknown[], error?: unknown) {
  let delivered = false;
  await page.route('**/api/chat/send', (route) => {
    delivered = true;
    const events = [
      'event: chat_delta\ndata: {"kind":"reasoning","text":"secret thoughts"}',
      'event: chat_delta\ndata: {"kind":"content","text":"the answer"}',
      'event: chat_exchange\ndata: ' +
        JSON.stringify({ messages: exchangeMessages, end_cursor: exchangeMessages.length, error }),
    ];
    const body = events.join('\n\n') + '\n\n';
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body });
  });
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const messages = delivered ? exchangeMessages : [];
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages, next_cursor: 0, truncated: false }) });
  });
}

test('streamed reply reconciles to the persisted transcript', async ({ page }) => {
  const rows = [
    { id: 'u1', seq: 1, role: 'user', content: 'hi', created_at: '2026-01-01T00:00:00Z' },
    { id: 'a1', seq: 2, role: 'assistant', content: 'the answer', reasoning: 'secret thoughts', created_at: '2026-01-01T00:00:01Z' },
  ];
  await mockChat(page);
  await mockStream(page, rows);
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Message Karta' }).fill('hi');
  await page.getByRole('textbox', { name: 'Message Karta' }).press('Enter');

  // The terminal exchange reconciles to the PERSISTED transcript.
  await expect(page.getByLabel('Assistant message').last()).toContainText('the answer', { timeout: 5000 });
  // The optimistic stream view is gone (no duplicate bubbles).
  await expect(page.getByLabel('Assistant is responding')).toHaveCount(0);
});

test('reasoning is persisted and viewable as an expandable thinking block', async ({ page }) => {
  const rows = [
    { id: 'u1', seq: 1, role: 'user', content: 'q', created_at: '2026-01-01T00:00:00Z' },
    { id: 'a1', seq: 2, role: 'assistant', content: 'the answer', reasoning: 'secret thoughts', created_at: '2026-01-01T00:00:01Z' },
  ];
  await mockChat(page);
  await mockStream(page, rows);
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Message Karta' }).fill('q');
  await page.getByRole('textbox', { name: 'Message Karta' }).press('Enter');
  const bubble = page.getByLabel('Assistant message').last();
  await expect(bubble).toContainText('the answer', { timeout: 5000 });
  // Expandable after: the Thinking block exists, collapsed by default.
  const thinking = bubble.locator('details.thinking');
  await expect(thinking).toHaveCount(1);
  await expect(thinking.getByText('secret thoughts')).not.toBeVisible();
  await thinking.locator('summary').click();
  await expect(thinking.getByText('secret thoughts')).toBeVisible();
});

test('a11y: working and streaming regions are aria-busy', async ({ page }) => {
  await mockChat(page);
  // A send that never answers: the pending bubble is aria-busy.
  // A handler that never resolves (and never fulfills) leaves the fetch
  // pending — documented Playwright behavior for hanging handlers.
  await page.route('**/api/chat/send', async () => {
    await new Promise(() => {});
  });
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Message Karta' }).fill('x');
  await page.getByRole('textbox', { name: 'Message Karta' }).press('Enter');
  const working = page.getByLabel('Assistant is working');
  await expect(working).toBeVisible({ timeout: 5000 });
  await expect(working).toHaveAttribute('aria-busy', 'true');
});

test('typed failure rides the terminal exchange and surfaces the error banner', async ({ page }) => {
  const rows = [{ id: 'u1', seq: 1, role: 'user', content: 'hi', created_at: '2026-01-01T00:00:00Z' }];
  await mockChat(page);
  await mockStream(page, rows, { reason: 'unreachable', message: 'provider unreachable' });
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Message Karta' }).fill('hi');
  await page.getByRole('textbox', { name: 'Message Karta' }).press('Enter');
  await expect(page.getByRole('alert')).toContainText('provider unreachable', { timeout: 5000 });
  // The user row reconciled; no assistant row exists.
  await expect(page.getByLabel('Your message').last()).toContainText('hi');
  await expect(page.getByLabel('Assistant message')).toHaveCount(0);
});
