// Live verification of the ADK spine (engine-adk-spine slice 2) against the
// REAL daemon + UI: drives the browser like a user, records every SSE frame
// the page receives, waits for the run to complete, reads the transcript and
// session state back, and screenshots the thread, the Work tab and a
// delegation slideout. Run from frontend/: node tests/_live/karta-live.mjs
import { chromium } from '@playwright/test';
import { writeFileSync } from 'node:fs';

const BASE = process.env.BASE ?? 'http://127.0.0.1:9245';
const GOAL = process.env.GOAL ?? 'Delegate to the builder agent the task: report the current UTC time using the clock tool, in one line. Then answer me with that time in one sentence. Do not call the clock yourself.';
const TIMEOUT_MS = Number(process.env.TIMEOUT_MS ?? 8 * 60 * 1000);
const OUT = process.env.OUT ?? '/tmp/karta-live';

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark' });
const consoleErrors = [];
page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); });
page.on('pageerror', (e) => consoleErrors.push('pageerror: ' + e.message));

// Record every SSE frame + the session's EventSource URL, from before load.
await page.addInitScript(() => {
  const w = window;
  w.__kartaEvents = [];
  w.__kartaStreams = [];
  const OrigES = window.EventSource;
  w.EventSource = function (url, init) {
    w.__kartaStreams.push(String(url));
    return new OrigES(url, init);
  };
  w.EventSource.prototype = OrigES.prototype;
  const origAdd = OrigES.prototype.addEventListener;
  OrigES.prototype.addEventListener = function (type, listener, ...rest) {
    return origAdd.call(this, type, (ev) => {
      try {
        w.__kartaEvents.push({ t: Date.now(), type, data: JSON.parse(ev.data) });
      } catch { w.__kartaEvents.push({ t: Date.now(), type, data: String(ev.data) }); }
      if (typeof listener === 'function') listener.call(this, ev);
      else if (listener && typeof listener.handleEvent === 'function') listener.handleEvent(ev);
    }, ...rest);
  };
});

const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
log('open', BASE);
await page.goto(BASE, { waitUntil: 'networkidle' });
await page.getByRole('button', { name: 'New Research' }).click();
const composer = page.getByRole('textbox', { name: 'Research question' });
await composer.waitFor({ timeout: 20000 });
const streamUrl = await page.evaluate(() => window.__kartaStreams.at(-1) ?? '');
const sessionId = decodeURIComponent((streamUrl.match(/\/api\/sessions\/([^/]+)\/events/) ?? [])[1] ?? '');
log('session', sessionId, 'stream', streamUrl);
await composer.fill(GOAL);
await composer.press('Enter');

// Wait for the run's terminal frame.
const t0 = Date.now();
let done = null;
while (Date.now() - t0 < TIMEOUT_MS) {
  await page.waitForTimeout(4000);
  const evs = await page.evaluate(() => window.__kartaEvents);
  const terminal = evs.filter((e) => e.type === 'session' && /run completed|run cancelled|run stopped|failed/i.test(String(e.data?.text ?? '')));
  if (terminal.length) { done = terminal.at(-1); break; }
  if (Date.now() - t0 > 20000 && !done) log('… running', evs.length, 'frames');
}
const events = await page.evaluate(() => window.__kartaEvents);
log('terminal:', done ? done.data.text : 'NONE within timeout', '| frames:', events.length, '| elapsed s:', Math.round((Date.now() - t0) / 1000));
await page.waitForTimeout(1500); // let the UI settle (pills, Work tab)

// Screenshots: the thread + Work tab; then the first delegation pill's slideout.
await page.screenshot({ path: `${OUT}-thread.png`, fullPage: false });
const pills = page.locator('.delegation-pill');
const pillCount = await pills.count();
if (pillCount > 0) {
  await pills.first().click();
  await page.waitForTimeout(600);
  await page.screenshot({ path: `${OUT}-slideout.png`, fullPage: false });
  await page.keyboard.press('Escape');
}
const workTab = page.getByRole('tabpanel', { name: 'Work' });
const workText = (await workTab.count()) ? await workTab.innerText() : '';
await page.screenshot({ path: `${OUT}-work.png`, fullPage: true });

// Read the transcript + session state back through the page (same origin → the vite proxy).
const api = async (path) => page.evaluate(async (p) => { const r = await fetch(p); return { status: r.status, body: await r.json().catch(() => null) }; }, path);
const messages = sessionId ? await api(`/api/chat/messages?session_id=${encodeURIComponent(sessionId)}&after_seq=0`) : { body: null };
const state = sessionId ? await api(`/api/sessions/${encodeURIComponent(sessionId)}/state`) : { body: null };

const kinds = {};
for (const e of events) kinds[e.type] = (kinds[e.type] ?? 0) + 1;
const rows = messages.body?.messages ?? [];
const summary = {
  sessionId, goal: GOAL, terminal: done?.data?.text ?? null, frames: events.length, kinds,
  attribution: {
    framesWithoutAgent: events.filter((e) => !e.data?.agent).map((e) => e.type),
    framesWithoutExecution: events.filter((e) => e.data?.kind !== 'analysis_started' && !e.data?.execution_id).map((e) => ({ type: e.type, text: e.data?.text })),
  },
  delegationToolsSeen: [...new Set(events.filter((e) => e.type === 'tool_call').map((e) => e.data?.tool_call?.tool).filter((t) => String(t).startsWith('delegate_')))],
  lifecycle: events.filter((e) => e.type === 'delegation_started' || e.type === 'delegation_completed').map((e) => ({ kind: e.type, agent: e.data?.agent, exec: e.data?.execution_id, status: e.data?.delegation?.status, tokens_in: e.data?.delegation?.tokens_in, tokens_out: e.data?.delegation?.tokens_out, task: e.data?.delegation?.task })),
  rows: rows.map((m) => ({ seq: m.seq, role: m.role, agent: m.agent, exec: m.execution_id, tool: m.tool_name ?? null, calls: m.tool_calls ?? null, prompt: m.prompt_tokens, completion: m.completion_tokens, model: m.model, content: (m.content ?? '').slice(0, 90) })),
  executions: state.body?.executions ?? null,
  pillCount, workText: workText.slice(0, 1200), consoleErrors,
};
writeFileSync(`${OUT}-report.json`, JSON.stringify(summary, null, 2));
writeFileSync(`${OUT}-events.json`, JSON.stringify(events, null, 2));
log(JSON.stringify({ kinds, delegationToolsSeen: summary.delegationToolsSeen, lifecycle: summary.lifecycle, pillCount, rows: rows.length, consoleErrors: consoleErrors.length }));
await browser.close();
