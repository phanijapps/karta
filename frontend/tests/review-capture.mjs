// Review capture: screenshots of the key surfaces with realistic
// daemon data (route mocks, same pattern as the specs). Run: node tests/review-capture.mjs
import { chromium } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const output = fileURLToPath(new URL('../../docs/architecture/review-captures/', import.meta.url));
await mkdir(output, { recursive: true });

const roster = {
  vault_root: '/home/dev/Documents/wards',
  wards: [
    { name: 'stock-analysis', archetype: 'research', lint_status: 'clean', finding_count: 0, canonical_title: 'Equity research method' },
    { name: 'india-analysis', archetype: 'research', lint_status: 'violations', finding_count: 2 },
    { name: 'scraps', archetype: 'generic', lint_status: 'clean', finding_count: 0, canonical_title: 'Scraps' },
  ],
};
const stockDetail = {
  name: 'stock-analysis', archetype: 'research', lint_status: 'clean', finding_count: 0,
  canonical_title: 'Equity research method', findings: [],
  layout: {
    kind: 'directory',
    children: [
      { id: 'canonical', kind: 'file', match: '{ward}.md', format: 'markdown' },
      { id: 'agent-instructions', kind: 'file', match: 'AGENTS.md', format: 'markdown' },
      { id: 'log', kind: 'file', match: 'log.md', format: 'markdown' },
      { id: 'tasks', kind: 'directory', match: 'tasks', required: false },
      { id: 'sources', kind: 'directory', match: 'sources', required: false },
    ],
  },
};
const indiaDetail = {
  name: 'india-analysis', archetype: 'research', lint_status: 'violations', finding_count: 2,
  findings: [
    { kind: 'missing_required', node_id: 'canonical', path: 'india-analysis.md', message: 'required file "india-analysis.md" is missing (match "{ward}.md")' },
    { kind: 'unexpected_entry', path: 'notes.txt', message: 'no layout rule matches "notes.txt"' },
  ],
};
const skills = {
  skills: ['book-reader', 'clean-code', 'duckduckgo-search', 'eagle-eye', 'explain-like-socrates', 'humanizer', 'light-panda-browser', 'pdf', 'plan-composer', 'ppt-agent', 'spec-builder', 'yfinance-market-analysis'].map((n) => ({
    name: n, description: `${n.replace(/-/g, ' ')} — seeded operator skill`, origin: 'local',
  })),
};

async function mockAll(page, extra = {}) {
  await page.route((url) => url.pathname.startsWith('/api/'), (route) => route.abort('failed'));
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: [
      { seq: 1, role: 'user', content: 'What do you know about AMD earnings?', created_at: new Date(Date.now() - 60000).toISOString() },
      { seq: 2, role: 'assistant', content: 'AMD reports Q3 on Oct 29. Last quarter beat on datacenter revenue (+115% YoY). I have prior coverage in the stock-analysis ward — want me to pull it?', created_at: new Date(Date.now() - 30000).toISOString() },
    ], next_cursor: 2, truncated: false }) });
  });
  await page.route((url) => url.pathname === '/api/chat/default-model', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ model: 'zai/glm-5.3' }) }));
  await page.route((url) => url.pathname === '/api/models', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: [], refreshed_at: {} }) }));
  await page.route((url) => url.pathname === '/api/wards', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(roster) });
  });
  await page.route((url) => url.pathname === '/api/wards/stock-analysis', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(stockDetail) }));
  await page.route((url) => url.pathname === '/api/wards/india-analysis', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(indiaDetail) }));
  await page.route((url) => url.pathname === '/api/skills', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(skills) });
  });
  for (const [re, body] of Object.entries(extra)) {
    await page.route(re, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) }));
  }
}

const browser = await chromium.launch();
const records = [];
async function shot(page, name) {
  await page.screenshot({ path: `${output}${name}.png` });
  records.push(name);
  console.log('captured', name);
}

try {
  for (const theme of ['dark', 'light']) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: theme });
    await mockAll(page);
    await page.goto('http://127.0.0.1:9245/');
    await page.locator(`html[data-theme="${theme}"]`).waitFor();

    // 1. Chat (the simple-run surface)
    await page.waitForTimeout(600);
    await shot(page, `${theme}-01-chat`);

    // 2. Wards roster
    await page.getByRole('button', { name: 'Wards' }).click();
    await page.getByLabel('Wards').waitFor();
    await shot(page, `${theme}-02-wards-roster`);

    // 3. Ward detail expanded (clean ward)
    await page.getByRole('button', { name: /Open ward stock-analysis/ }).click();
    await page.locator('.ward-detail').waitFor();
    await shot(page, `${theme}-03-ward-detail-clean`);

    // 4. Ward with lint violations
    await page.getByRole('button', { name: /Open ward india-analysis/ }).click();
    await page.locator('.ward-detail').getByText('Findings').waitFor();
    await shot(page, `${theme}-04-ward-detail-violations`);

    // 5. Settings → Skills (the relocated operator set)
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await page.getByRole('dialog', { name: 'Settings', exact: true }).waitFor();
    const skillsTab = page.getByRole('button', { name: /Skills/ });
    if (await skillsTab.count()) await skillsTab.first().click();
    await page.waitForTimeout(400);
    await shot(page, `${theme}-05-settings-skills`);
    await page.keyboard.press('Escape');

    await page.close();
  }
  await writeFile(`${output}manifest.json`, JSON.stringify({ captured: records, date: new Date().toISOString() }, null, 2));
  console.log(`\n${records.length} captures in ${output}`);
} finally {
  await browser.close();
}
