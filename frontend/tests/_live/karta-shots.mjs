// Final-state screenshots for the engine-adk-spine signoff: the live research
// session (thread, Work tab, Context tab, slideout) and Settings → Agents.
import { chromium } from '@playwright/test';
const BASE = process.env.BASE ?? 'http://127.0.0.1:9245';
const OUT = process.env.OUT ?? '/tmp/karta-signoff';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark' });
const errors = [];
page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
await page.goto(BASE, { waitUntil: 'networkidle' });
// The live run's session: the local-llm-tools ward's latest research session.
const nav = page.getByRole('navigation', { name: 'Workspace navigation' });
await nav.locator('.ward-row', { hasText: 'local-llm-tools' }).click({ force: true });
await page.getByRole('textbox', { name: 'Research question' }).waitFor({ timeout: 20000 });
await page.getByRole('article', { name: 'Final answer' }).first().waitFor({ timeout: 20000 });
const sessionTitle = await page.locator('.ward-group .session.active, li .session[aria-current="true"]').first().innerText().catch(() => '(session)');
await page.getByRole('tab', { name: 'Work' }).click();
await page.screenshot({ path: `${OUT}-1-thread-work.png` });
const pills = page.locator('.delegation-pill');
await pills.first().click();
await page.waitForTimeout(500);
await page.screenshot({ path: `${OUT}-2-slideout.png` });
await page.keyboard.press('Escape');
await page.getByRole('tab', { name: 'Context' }).click();
await page.waitForTimeout(400);
await page.screenshot({ path: `${OUT}-3-context.png` });
const contextText = await page.getByRole('tabpanel', { name: 'Context' }).innerText();
// Settings → Agents (the roster the spine offers delegation tools for).
await page.getByRole('button', { name: 'Settings', exact: true }).click();
const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
await dialog.getByRole('button', { name: 'Agents' }).click();
await page.waitForTimeout(800);
await page.screenshot({ path: `${OUT}-4-settings-agents.png` });
const agentsText = await dialog.innerText();
await page.keyboard.press('Escape');
console.log(JSON.stringify({ sessionTitle, contextAgents: (contextText.match(/Agents[\s\S]{0,300}/) || [''])[0].split('\n').slice(0, 4), roster: agentsText.split('\n').filter((l) => /agent|analyst|reviewer|planner|writer/i.test(l)).slice(0, 12), consoleErrors: errors }));
await browser.close();
