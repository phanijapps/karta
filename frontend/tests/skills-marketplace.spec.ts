import { test, expect, type Page } from '@playwright/test';

// Route-mocked marketplace plane: zero network, named per task 4.4.

const marketHit = { id: 'anthropics/skills/pdf', source: 'anthropics/skills', skillId: 'pdf', name: 'pdf', installs: 234146 };
const hostileBody = '# pdf\n\n<script>alert(1)</script>\n\n<img src="https://evil.example/pixel.png">\n\n[link](javascript:alert(2))';

async function mockChat(page: Page) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages: [], next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
}

function repoPreview(overrides: Record<string, unknown> = {}) {
  return {
    source: 'anthropics/skills',
    commit: 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2',
    capability: 'cap-token-abcdefghijklmnop',
    skills: [{
      name: 'pdf', description: 'Work with PDF files.',
      body: '# pdf\n\nProcess PDF documents.',
      files: [{ path: 'SKILL.md', size: 40 }, { path: 'scripts/pdftool.sh', size: 220, executable: true }],
      license: 'LICENSE',
    }],
    ...overrides,
  };
}

async function openBrowse(page: Page) {
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await dialog.getByRole('button', { name: 'Skills', exact: true }).click();
  await dialog.getByRole('tab', { name: 'Browse' }).click();
  return dialog;
}

test('search-renders-results', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/marketplace/search?**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [marketHit] }) }));
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Search the marketplace' }).fill('pdf');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(dialog.getByRole('button', { name: /pdf/ })).toContainText('anthropics/skills');
  await expect(dialog.getByText('234K installs')).toBeVisible();
});

test('search results paginate', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  const many = Array.from({ length: 19 }, (_, i) => ({ id: `o/r/s${i}`, source: 'o/r', name: `s${i}`, installs: 10 + i }));
  await page.route('**/api/skills/marketplace/search?**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: many }) }));
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Search the marketplace' }).fill('s');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  // 19 results → 8 per page → 3 pages.
  await expect(dialog.getByText('Page 1 of 3')).toBeVisible();
  await expect(dialog.getByRole('button', { name: /s0/ })).toBeVisible();
  await expect(dialog.getByRole('button', { name: /s8/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Next' }).click();
  await expect(dialog.getByText('Page 2 of 3')).toBeVisible();
  await expect(dialog.getByRole('button', { name: /s8/ })).toBeVisible();
  await expect(dialog.getByRole('button', { name: /s15/ })).toBeVisible();
  await page.getByRole('button', { name: 'Next' }).click();
  await expect(dialog.getByText('Page 3 of 3')).toBeVisible();
  await expect(dialog.getByRole('button', { name: /s17/ })).toBeVisible();
  await expect(dialog.getByRole('button', { name: /s0/ })).toHaveCount(0);
});

test('index-down-typed-state', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/marketplace/search?**', (route) =>
    route.fulfill({ status: 502, contentType: 'application/json', body: JSON.stringify({ code: 'index_unreachable', message: 'skills.sh index unreachable: connection refused' }) }));
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Search the marketplace' }).fill('pdf');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(dialog.getByText('Index unreachable.')).toBeVisible();
  await expect(dialog.getByText('connection refused')).toBeVisible();
  await expect(dialog.getByText('Paste any GitHub owner/repo below')).toBeVisible();
});

test('preview-then-install', async ({ page }) => {
  await mockChat(page);
  const skills: unknown[] = [];
  await page.route('**/api/skills', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills }) });
    }
    return route.fallback();
  });
  await page.route('**/api/skills/marketplace/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(repoPreview()) }));
  await page.route('**/api/skills/marketplace/install', (route) => {
    const body = route.request().postDataJSON() as { capability: string; skills: string[] };
    expect(body.capability).toBe('cap-token-abcdefghijklmnop');
    skills.push({ name: 'pdf', description: 'Work with PDF files.', directory: '/home/u/.zbot/config/skills/pdf', origin: 'marketplace', source: 'anthropics/skills' });
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [{ name: 'pdf', slug: 'pdf', status: 'created', origin: 'marketplace' }] }) });
  });
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Install from GitHub' }).fill('anthropics/skills');
  await page.getByRole('button', { name: 'Preview' }).click();
  const preview = page.getByRole('dialog', { name: 'Preview anthropics/skills' });
  // Mandatory preview: SKILL.md body, file list incl. executable bit, pinned commit, untrusted label.
  await preview.getByText('SKILL.md (2 files').click(); // expand the preview card
  await expect(preview.getByText('# pdf')).toBeVisible();
  await expect(preview.getByText('scripts/pdftool.sh')).toBeVisible();
  await expect(preview.getByText('executable')).toBeVisible();
  await expect(preview.getByText(/Pinned commit a1b2c3d4/)).toBeVisible();
  await expect(preview.getByText('untrusted')).toBeVisible();
  await preview.getByRole('button', { name: /^Install 1 skill/ }).click();
  await expect(preview).toHaveCount(0);
  await dialog.getByRole('tab', { name: 'Installed' }).click();
  await expect(dialog.getByRole('button', { name: /pdf/ })).toBeVisible();
  await expect(dialog.getByRole('button', { name: /pdf/ }).getByText('Marketplace')).toBeVisible();
});

test('multi-skill-subset-outcomes', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  const preview = repoPreview({
    skills: [
      { name: 'pdf', description: 'd', body: '# pdf', files: [{ path: 'SKILL.md', size: 10 }] },
      { name: 'docx', description: 'd', body: '# docx', files: [{ path: 'SKILL.md', size: 10 }] },
    ],
  });
  await page.route('**/api/skills/marketplace/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(preview) }));
  const seen: { skills: string[] }[] = [];
  await page.route('**/api/skills/marketplace/install', (route) => {
    seen.push(route.request().postDataJSON());
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [
      { name: 'pdf', status: 'created', origin: 'marketplace' },
      { name: 'docx', status: 'skipped', reason: 'duplicate' },
    ] }) });
  });
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Install from GitHub' }).fill('anthropics/skills');
  await page.getByRole('button', { name: 'Preview' }).click();
  const card = page.getByRole('dialog', { name: 'Preview anthropics/skills' });
  // Deselect one skill; install carries exactly the chosen subset.
  await card.getByRole('checkbox', { name: /pdf/ }).uncheck();
  await card.getByRole('button', { name: /^Install 1 skill/ }).click();
  await expect.poll(() => seen.length).toBe(1);
  expect(seen[0]?.skills).toEqual(['docx']);
  await expect(dialog.getByText('skipped · duplicate')).toBeVisible();
  await expect(dialog.getByText('created', { exact: true })).toBeVisible();
});

test('duplicate-refusal', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [{ name: 'pdf', description: 'd', directory: '/s/pdf', origin: 'marketplace', source: 'anthropics/skills' }] }) }));
  await page.route('**/api/skills/marketplace/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(repoPreview()) }));
  await page.route('**/api/skills/marketplace/install', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [{ name: 'pdf', status: 'skipped', reason: 'duplicate', message: 'a skill named "pdf" already exists' }] }) }));
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Install from GitHub' }).fill('anthropics/skills');
  await page.getByRole('button', { name: 'Preview' }).click();
  await page.getByRole('dialog', { name: 'Preview anthropics/skills' }).getByRole('button', { name: /^Install 1 skill/ }).click();
  await expect(dialog.getByText('skipped · duplicate')).toBeVisible();
});

test('changed-upstream-on-expired-capability', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/marketplace/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(repoPreview()) }));
  await page.route('**/api/skills/marketplace/install', (route) =>
    route.fulfill({ status: 502, contentType: 'application/json', body: JSON.stringify({ code: 'changed_upstream', message: 'content changed upstream; re-preview: SKILL.md digest mismatch' }) }));
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Install from GitHub' }).fill('anthropics/skills');
  await page.getByRole('button', { name: 'Preview' }).click();
  await page.getByRole('dialog', { name: 'Preview anthropics/skills' }).getByRole('button', { name: /^Install 1 skill/ }).click();
  await expect(dialog.getByText('Install failed.')).toBeVisible();
  await expect(dialog.getByText('digest mismatch')).toBeVisible();
});

test('import-preview-commit', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/import/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ stagingId: 'staging-token-0123456789abcdef', skill: {
      name: 'My Skill', description: 'mine', body: '# mine\n\nhand-made', files: [{ path: 'SKILL.md', size: 22 }],
    } }) }));
  const seenCommit: { stagingId: string }[] = [];
  await page.route('**/api/skills/import/commit', (route) => {
    seenCommit.push(route.request().postDataJSON());
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ name: 'My Skill', slug: 'my-skill', status: 'created', origin: 'import' }) });
  });
  const dialog = await openBrowse(page);
  const chooser = page.waitForEvent('filechooser');
  await page.getByRole('button', { name: 'Import .skill / .zip' }).click();
  const fc = await chooser;
  await fc.setFiles({ name: 'my.skill', mimeType: 'application/zip', buffer: Buffer.from('zip-bytes') });
  const preview = page.getByRole('dialog', { name: 'Preview imported skill' });
  await expect(preview.getByText('# mine')).toBeVisible();
  await expect(preview.getByText('untrusted')).toBeVisible();
  await preview.getByRole('button', { name: 'Install skill' }).click();
  await expect.poll(() => seenCommit.length).toBe(1);
  expect(seenCommit[0]?.stagingId).toBe('staging-token-0123456789abcdef');
  await expect(dialog.getByText('created', { exact: true })).toBeVisible();
});

test('archive-violation-surfaces-reason', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/import/preview', (route) =>
    route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ code: 'invalid_import', message: 'archive violation: non-regular entry "evil" (symlink) rejected' }) }));
  await openBrowse(page);
  const chooser = page.waitForEvent('filechooser');
  await page.getByRole('button', { name: 'Import .skill / .zip' }).click();
  const fc = await chooser;
  await fc.setFiles({ name: 'bad.skill', mimeType: 'application/zip', buffer: Buffer.from('x') });
  const preview = page.getByRole('dialog', { name: 'Preview imported skill' });
  await expect(preview.getByText('symlink')).toBeVisible();
});

test('staging-expired-commit', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/import/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ stagingId: 'staging-token-0123456789abcdef', skill: { name: 'X', description: 'd', body: 'b', files: [] } }) }));
  await page.route('**/api/skills/import/commit', (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ code: 'capability_expired', message: 'staging expired, unknown, or already used — preview the archive again' }) }));
  const dialog = await openBrowse(page);
  const chooser = page.waitForEvent('filechooser');
  await page.getByRole('button', { name: 'Import .skill / .zip' }).click();
  const fc = await chooser;
  await fc.setFiles({ name: 'x.skill', mimeType: 'application/zip', buffer: Buffer.from('x') });
  await page.getByRole('dialog', { name: 'Preview imported skill' }).getByRole('button', { name: 'Install skill' }).click();
  await expect(dialog.getByText('staging expired', { exact: false })).toBeVisible();
});

test('hostile-markdown-inert', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/marketplace/preview', (route) => {
    const p = repoPreview();
    p.skills[0].body = hostileBody;
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(p) });
  });
  await openBrowse(page);
  await page.getByRole('textbox', { name: 'Install from GitHub' }).fill('anthropics/skills');
  await page.getByRole('button', { name: 'Preview' }).click();
  const preview = page.getByRole('dialog', { name: 'Preview anthropics/skills' });
  await preview.getByText('SKILL.md (2 files').click(); // expand the preview card
  // Rendered as plain text: the script tag is TEXT, no script/img elements load.
  await expect(preview.getByText('<script>alert(1)</script>')).toBeVisible();
  const imgs = await preview.locator('img').count();
  expect(imgs).toBe(0);
  const scripts = await preview.locator('script').count();
  expect(scripts).toBe(0);
});

test('dialog-escape-closes-only-preview', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills: [] }) }));
  await page.route('**/api/skills/marketplace/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(repoPreview()) }));
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Install from GitHub' }).fill('anthropics/skills');
  await page.getByRole('button', { name: 'Preview' }).click();
  const preview = page.getByRole('dialog', { name: 'Preview anthropics/skills' });
  await expect(preview).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(preview).toHaveCount(0);
  await expect(dialog).toBeVisible(); // Settings stays open
});

test('delayed-install-reconciles-after-dismissal', async ({ page }) => {
  await mockChat(page);
  const skills: unknown[] = [];
  await page.route('**/api/skills', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills }) });
    }
    return route.fallback();
  });
  await page.route('**/api/skills/marketplace/preview', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(repoPreview()) }));
  await page.route('**/api/skills/marketplace/install', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 600)); // in-flight while dismissed
    skills.push({ name: 'pdf', description: 'd', directory: '/s/pdf', origin: 'marketplace' });
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [{ name: 'pdf', status: 'created' }] }) });
  });
  const dialog = await openBrowse(page);
  await page.getByRole('textbox', { name: 'Install from GitHub' }).fill('anthropics/skills');
  await page.getByRole('button', { name: 'Preview' }).click();
  await page.getByRole('dialog', { name: 'Preview anthropics/skills' }).getByRole('button', { name: /^Install 1 skill/ }).click();
  await page.keyboard.press('Escape'); // dismiss mid-install
  await dialog.getByRole('tab', { name: 'Installed' }).click();
  await expect(dialog.getByRole('button', { name: /pdf/ })).toBeVisible({ timeout: 5000 });
});
