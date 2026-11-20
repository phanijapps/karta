// Capture is intentionally separate from human/agent visual judgement.
import { chromium } from '@playwright/test';
import { HtmlValidate } from 'html-validate';
import { spawn } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const output = fileURLToPath(new URL('../../docs/architecture/frontend-shell-captures/', import.meta.url));
await mkdir(output, { recursive: true });
const server = spawn('pnpm', ['preview', '--port', '9247', '--strictPort'], { stdio: 'ignore', detached: true });
let browser;
try {
  for (let i = 0; i < 100; i++) {
    try { if ((await fetch('http://127.0.0.1:9247')).ok) break; } catch { /* Wait for the local preview server. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  browser = await chromium.launch();
  const validator = new HtmlValidate({ extends: ['html-validate:recommended'], rules: { 'no-inline-style': 'off', 'prefer-native-element': 'off', 'attribute-empty-style': 'off', 'attribute-boolean-style': 'off', 'no-trailing-whitespace': 'off', 'no-implicit-button-type': 'off', 'no-implicit-input-type': 'off' } });
  const records = [];
  const failures = [];
  for (const theme of ['dark', 'light']) {
    for (const [width, height] of [[375, 600], [375, 900], [768, 600], [768, 900], [1280, 600], [1280, 900], [1440, 900], [375, 812]]) {
      const page = await browser.newPage({ viewport: { width, height }, colorScheme: theme, reducedMotion: 'reduce' });
      await page.goto('http://127.0.0.1:9247/');
      await page.locator(`html[data-theme="${theme}"]`).waitFor();
      for (const surface of ['shell', 'settings']) {
        if (surface === 'settings') {
          if (width < 768) await page.getByRole('button', { name: 'Toggle left sidebar' }).click();
          await page.getByRole('button', { name: 'Settings', exact: true }).click();
          await page.getByRole('dialog', { name: 'Settings', exact: true }).waitFor();
        }
        const name = `${theme}-${width}x${height}-${surface}`;
        const metadata = await page.evaluate(() => ({ route: location.pathname, 'viewport-width': innerWidth, 'viewport-height': innerHeight, 'scroll-position': scrollY, 'page-scrollable': document.documentElement.scrollHeight > innerHeight ? 'yes' : 'no' }));
        await page.screenshot({ path: `${output}${name}.png` });
        records.push({ ...metadata, surface, theme, image: `${name}.png` });
        const result = await validator.validateString(await page.content());
        if (!result.valid) failures.push({ name, messages: result.results.flatMap(r => r.messages.map(m => `${m.ruleId}: ${m.message} (${m.selector})`)) });
        if (metadata['page-scrollable'] === 'yes') {
          await page.evaluate(() => scrollTo(0, 400));
          const scrolled = await page.evaluate(() => scrollY);
          await page.screenshot({ path: `${output}${name}-scrolled.png` });
          records.push({ ...metadata, 'scroll-position': scrolled, surface, theme, image: `${name}-scrolled.png` });
        }
      }
      await page.close();
    }
  }
  await writeFile(`${output}manifest.json`, JSON.stringify({ basis: 'Declared breakpoints 768 and 1280; supported minimum 375 CSS px. Required widths: 375, 768, 1280; short/tall heights 600/900. Additional requested viewports 1440x900 and 375x812.', captures: records }, null, 2) + '\n');
  console.log(`${records.length} captures written to ${output}`);
  const unique = [...new Set(failures.flatMap(f => f.messages))];
  console.log('Rendered HTML validation:', unique.length ? unique : 'PASS');
  if (unique.length) process.exitCode = 1;
} finally {
  await browser?.close();
  if (server.pid) process.kill(-server.pid, 'SIGTERM');
}
