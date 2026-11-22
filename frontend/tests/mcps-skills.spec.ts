import { test, expect, type Page } from '@playwright/test';

// The chat surface loads /api/chat/messages on mount; every test stubs it.
async function mockChat(page: import('@playwright/test').Page, messages: unknown[] = []) {
  await page.route((url) => url.pathname === '/api/chat/messages', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ messages, next_cursor: 0, truncated: false }) });
    }
    return route.fallback();
  });
}

// Route-mocked MCP + skills planes: no live daemon, no processes.

const dualServer = {
  name: 'github',
  file: '/home/u/.zbot/config/mcps/github.yaml',
  transports: [
    { kind: 'stdio', command: 'gh-mcp', args: ['serve'], env: { TOKEN: 't' } },
    { kind: 'http', url: 'https://mcp.example.com/mcp' },
  ],
};
const httpServer = {
  name: 'remote',
  file: '/home/u/.zbot/config/mcps/remote.yaml',
  transports: [{ kind: 'http', url: 'https://tools.example.com/mcp' }],
};

async function mockMcpList(page: Page, servers: unknown[], loadErrors: unknown[] = []) {
  await page.route('**/api/mcps', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers, ...(loadErrors.length ? { load_errors: loadErrors } : {}) }) });
    }
    return route.fallback();
  });
}

async function mockSkillList(page: Page, skills: unknown[], loadErrors: unknown[] = []) {
  await page.route('**/api/skills', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills, ...(loadErrors.length ? { load_errors: loadErrors } : {}) }) });
    }
    return route.fallback();
  });
}

async function openAddDialog(page: Page) {
  const settings = page.getByRole('dialog', { name: 'Settings', exact: true });
  await settings.getByRole('button', { name: 'Add server' }).click();
  return page.getByRole('dialog', { name: 'Add MCP server' });
}

async function openSettings(page: Page, section: 'MCP' | 'Skills') {
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Settings', exact: true });
  await dialog.getByRole('button', { name: section, exact: true }).click();
  return dialog;
}

test('mcp list shows transports and load errors; daemon unreachable is explicit', async ({ page }) => {
  await mockChat(page);
  // First open: daemon unreachable.
  await page.route('**/api/mcps', (route) => route.abort());
  let dialog = await openSettings(page, 'MCP');
  await expect(dialog.getByText('Daemon unreachable.')).toBeVisible();
  await page.keyboard.press('Escape');

  // Second open: healthy list with a broken hand-edited file.
  await mockMcpList(page, [dualServer, httpServer], [
    { path: '/home/u/.zbot/config/mcps/broken.yaml', kind: 'mcp', message: 'at least one of command (stdio) or url (streamable http) is required' },
  ]);
  dialog = await openSettings(page, 'MCP');
  await expect(dialog.getByRole('button', { name: /github/ })).toContainText('stdio · http');
  await dialog.getByRole('button', { name: /github/ }).click();
  await expect(dialog.getByRole('textbox', { name: 'Command' })).toHaveValue('gh-mcp serve');
  await expect(dialog.getByRole('textbox', { name: 'Environment' })).toHaveValue('TOKEN');
  await expect(dialog.getByRole('textbox', { name: 'HTTP URL' })).toHaveValue('https://mcp.example.com/mcp');
  await expect(dialog.getByText('1 file(s) failed to load')).toBeVisible();
  await expect(dialog.getByText('broken.yaml')).toBeVisible();
});

test('mcp create success and typed failure recovery', async ({ page }) => {
  await mockChat(page);
  const servers: unknown[] = [];
  await page.route('**/api/mcps', (route) => {
    const method = route.request().method();
    if (method === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers }) });
    }
    if (method === 'POST') {
      const body = route.request().postDataJSON() as { name: string };
      if (body.name === 'github') {
        return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'duplicate_name', message: 'an MCP server named "github" already exists' }) });
      }
      servers.push(dualServer);
      return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(dualServer) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'MCP');
  const addDialog = await openAddDialog(page);
  await addDialog.getByRole('textbox', { name: 'Name' }).fill('github');
  await addDialog.getByRole('textbox', { name: 'Command' }).fill('gh-mcp');
  await addDialog.getByRole('button', { name: 'Add server', exact: true }).click();
  // 409 keeps the dialog with the typed error; retry with a new name succeeds.
  await expect(addDialog.getByText('an MCP server named "github" already exists')).toBeVisible();
  await addDialog.getByRole('textbox', { name: 'Name' }).fill('github-2');
  await addDialog.getByRole('button', { name: 'Add server', exact: true }).click();
  await expect(addDialog).toHaveCount(0); // dialog closed on success
  await expect(dialog.getByRole('button', { name: /github/ })).toBeVisible();
});

test('mcp transport selector shows only relevant fields; form rules enforced', async ({ page }) => {
  await mockChat(page);
  await mockMcpList(page, []);
  await openSettings(page, 'MCP');
  const addDialog = await openAddDialog(page);
  // Local (default): command fields visible, NO URL field at all.
  await expect(addDialog.getByRole('textbox', { name: 'Command' })).toBeVisible();
  await expect(addDialog.getByRole('textbox', { name: 'HTTP URL' })).toHaveCount(0);
  // Invalid managed name caught client-side.
  await addDialog.getByRole('textbox', { name: 'Name' }).fill('Bad_Name');
  await addDialog.getByRole('button', { name: 'Add server', exact: true }).click();
  await expect(addDialog.getByText('lowercase kebab-case')).toBeVisible();
  // Remote: URL appears and is required; command hidden.
  await addDialog.getByLabel('Remote URL (http)').check();
  await expect(addDialog.getByRole('textbox', { name: 'HTTP URL' })).toBeVisible();
  await expect(addDialog.getByRole('textbox', { name: /^Arguments/ })).toHaveCount(0);
  await addDialog.getByRole('textbox', { name: 'Name' }).fill('ok-name');
  await addDialog.getByRole('button', { name: 'Add server', exact: true }).click();
  await expect(addDialog.getByRole('alert')).toContainText('remote server needs its URL');
});

test('mcp edit round-trips whitespace-bearing args and env byte-exactly', async ({ page }) => {
  await mockChat(page);
  const tricky = {
    name: 'tricky',
    file: '/home/u/.zbot/config/mcps/tricky.yaml',
    transports: [{
      kind: 'stdio',
      command: 'runner',
      args: ['--directory', '/home/me/My Project', ''],
      env: { LABEL: 'two  spaces ', PATH_HINT: '/a b/c' },
    }],
  };
  await mockMcpList(page, [tricky]);
  const seen: { patch?: { stdio?: { args?: string[]; env?: Record<string, string> } } } = {};
  await page.route('**/api/mcps/tricky', (route) => {
    if (route.request().method() === 'PATCH') {
      seen.patch = route.request().postDataJSON();
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(tricky) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /tricky/ }).click();
  await dialog.getByRole('button', { name: 'Edit', exact: true }).click();
  const editDialog = page.getByRole('dialog', { name: 'Edit tricky' });
  const args = editDialog.getByRole('textbox', { name: /^Arguments/ });
  const env = editDialog.getByRole('textbox', { name: /^Environment/ });
  // One arg per line (including the empty third argument); values verbatim.
  await expect(args).toHaveValue('--directory\n/home/me/My Project\n');
  await expect(env).toHaveValue('LABEL=two  spaces \nPATH_HINT=/a b/c');
  // A no-op save sends the identical arrays/maps — nothing flattened.
  await editDialog.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => seen.patch !== undefined).toBe(true);
  expect(seen.patch?.stdio?.args).toEqual(['--directory', '/home/me/My Project', '']);
  expect(seen.patch?.stdio?.env).toEqual({ LABEL: 'two  spaces ', PATH_HINT: '/a b/c' });
});

test('mcp test connection shows per-transport results, failures, and truncation', async ({ page }) => {
  await mockChat(page);
  await mockMcpList(page, [dualServer]);
  await page.route('**/api/mcps/github/test', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        results: [
          { transport: 'stdio', status: 'ok', tools: Array.from({ length: 256 }, (_, i) => ({ name: `tool_${i}` })), truncated: true },
          { transport: 'http', status: 'failed', failure: { reason: 'connect_failed', message: 'refused' } },
        ],
      }),
    }),
  );
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /github/ }).click();
  await dialog.getByRole('button', { name: 'Test connection' }).click();
  await expect(dialog.getByText(/list truncated at 256/)).toBeVisible();
  await expect(dialog.getByText('tool_0', { exact: true })).toBeVisible();
  await expect(dialog.getByText('connect_failed')).toBeVisible();
  await expect(dialog.getByText('Check the server is running')).toBeVisible();
});

test('mcp probe request failure (404) stays visible', async ({ page }) => {
  await mockChat(page);
  await mockMcpList(page, [httpServer]);
  await page.route('**/api/mcps/remote/test', (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ code: 'not_found', message: 'no MCP server named remote' }) }),
  );
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /remote/ }).click();
  await dialog.getByRole('button', { name: 'Test connection' }).click();
  await expect(dialog.getByText('Test could not run:')).toBeVisible();
  await expect(dialog.getByText('no MCP server named remote')).toBeVisible();
});

test('mcp delete asks for confirmation', async ({ page }) => {
  await mockChat(page);
  const servers: unknown[] = [httpServer];
  await page.route('**/api/mcps', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers }) });
    }
    return route.fallback();
  });
  await page.route('**/api/mcps/remote', (route) => {
    if (route.request().method() === 'DELETE') {
      servers.length = 0;
      return route.fulfill({ status: 204, body: '' });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /remote/ }).click();
  const popup = page.getByRole('dialog', { name: 'MCP server remote' });
  await popup.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog.getByText('Delete remote?')).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog.getByText('No MCP servers registered.')).toBeVisible();
});

test('mcp delete failure (io_error) stays visible', async ({ page }) => {
  await mockChat(page);
  await mockMcpList(page, [httpServer]);
  await page.route('**/api/mcps/remote', (route) => {
    if (route.request().method() === 'DELETE') {
      return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 'io_error', message: 'removing the manifest failed' }) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /remote/ }).click();
  const popup = page.getByRole('dialog', { name: 'MCP server remote' });
  await popup.getByRole('button', { name: 'Delete', exact: true }).click();
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog.getByText('Operation failed.')).toBeVisible();
  await expect(dialog.getByText('removing the manifest failed')).toBeVisible();
});

test('row toggle disables a server with a full-replace PATCH; popup shows state', async ({ page }) => {
  await mockChat(page);
  const server = {
    name: 'github', file: '/home/u/.zbot/config/mcps/github.yaml', enabled: true,
    transports: [{ kind: 'stdio', command: 'gh-mcp', args: ['serve'] }],
  };
  const servers = [server];
  let patch: Record<string, unknown> | null = null;
  await page.route('**/api/mcps', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers }) });
    }
    return route.fallback();
  });
  await page.route('**/api/mcps/github', (route) => {
    if (route.request().method() === 'PATCH') {
      patch = route.request().postDataJSON();
      servers[0] = { ...server, enabled: false };
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(servers[0]) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'MCP');
  // Row-level switch sends the full manifest with the flipped state.
  // The row switch reflects the enabled state; disabling = unchecking it.
  await dialog.getByRole('checkbox', { name: 'Disable github' }).uncheck();
  await expect.poll(() => patch !== null).toBe(true);
  expect(patch).toMatchObject({ enabled: false, stdio: { command: 'gh-mcp', args: ['serve'] } });
  await expect(dialog.getByText('Disabled')).toBeVisible(); // row marking
  // Selecting opens the popup with the state + actions still offered.
  await dialog.getByRole('button', { name: /github/ }).click();
  const popup = page.getByRole('dialog', { name: 'MCP server github' });
  await expect(popup.getByRole('textbox', { name: 'Command' })).toHaveValue('gh-mcp serve');
  await expect(popup.getByRole('checkbox', { name: 'Enable github' })).toBeVisible();
  await expect(popup.getByRole('button', { name: 'Test connection' })).toBeEnabled();
});

test('form edit and rename preserve a disabled server\'s state', async ({ page }) => {
  await mockChat(page);
  const server = {
    name: 'off', file: '/home/u/.zbot/config/mcps/off.yaml', enabled: false,
    transports: [{ kind: 'stdio', command: 'old-cmd' }],
  };
  const servers = [server];
  let patch: Record<string, unknown> | null = null;
  await page.route('**/api/mcps', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers }) });
    }
    return route.fallback();
  });
  await page.route('**/api/mcps/off', (route) => {
    if (route.request().method() === 'PATCH') {
      patch = route.request().postDataJSON();
      servers[0] = { ...server, name: 'renamed', transports: [{ kind: 'stdio', command: 'new-cmd' }], enabled: false };
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(servers[0]) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'MCP');
  await expect(dialog.getByText('Disabled')).toBeVisible(); // row marked
  await dialog.getByRole('button', { name: /off/ }).click();
  const popup = page.getByRole('dialog', { name: 'MCP server off' });
  await popup.getByRole('button', { name: 'Edit', exact: true }).click();
  const editDialog = page.getByRole('dialog', { name: 'Edit off' });
  await editDialog.getByRole('textbox', { name: 'Command' }).fill('new-cmd');
  await editDialog.getByRole('textbox', { name: 'Name' }).fill('renamed');
  await editDialog.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => patch !== null).toBe(true);
  // The full-replace payload carries the CURRENT enablement — no silent
  // re-enable from an ordinary edit+rename.
  expect(patch).toMatchObject({ enabled: false, stdio: { command: 'new-cmd' } });
  await expect(dialog.getByRole('button', { name: /renamed/ })).toBeVisible();
  await expect(dialog.getByText('Disabled')).toBeVisible();
});

test('popup escape closes only the popup; toggle failure stays visible', async ({ page }) => {
  await mockChat(page);
  const server = {
    name: 'esc', file: '/e.yaml', enabled: true,
    transports: [{ kind: 'stdio', command: 'x' }],
  };
  let patchAttempts = 0;
  await page.route('**/api/mcps', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers: [server] }) });
    }
    return route.fallback();
  });
  await page.route('**/api/mcps/esc', (route) => {
    if (route.request().method() === 'PATCH') {
      patchAttempts++;
      return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 'io_error', message: 'write refused' }) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /esc/ }).click();
  const popup = page.getByRole('dialog', { name: 'MCP server esc' });
  // Escape inside the popup closes ONLY the popup (Settings stays).
  await page.keyboard.press('Escape');
  await expect(popup).toHaveCount(0);
  await expect(dialog).toBeVisible();
  // A failed row toggle reconciles and leaves a visible explanation.
  await dialog.getByRole('checkbox', { name: 'Disable esc' }).uncheck();
  await expect(dialog.getByText('Operation failed.')).toBeVisible();
  await expect(dialog.getByText('write refused')).toBeVisible();
  expect(patchAttempts).toBe(1);
});

test('paste JSON imports the official block with per-server outcomes', async ({ page }) => {
  await mockChat(page);
  const servers: unknown[] = [];
  await page.route('**/api/mcps', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers }) });
    }
    return route.fallback();
  });
  await page.route('**/api/mcps/import', (route) => {
    const body = route.request().postDataJSON() as { document: string; name?: string };
    // Daemon-side parsing is mocked to mirror the contract: outcomes in
    // paste order, one per server.
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [
      { source_key: 'filesystem', name: 'filesystem', status: 'created' },
      { source_key: 'existing', status: 'skipped', reason: 'duplicate' },
      { source_key: 'broken', status: 'failed', reason: 'field', message: 'at least one transport is required (command and/or url)' },
    ] }) });
  });
  const dialog = await openSettings(page, 'MCP');
  const addDialog = await openAddDialog(page);
  await addDialog.getByRole('tab', { name: 'Paste JSON' }).click();
  await addDialog.getByRole('textbox', { name: 'Paste standard mcpServers JSON' }).fill(`{
    "mcpServers": {
      "filesystem": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"] },
      "existing": { "command": "x" },
      "broken": { "args": ["nope"] }
    }
  }`);
  // Multi-server paste: the name field stays disabled (names come from keys).
  await expect(addDialog.getByRole('textbox', { name: /Server name/ })).toBeDisabled();
  await addDialog.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(addDialog.getByText('filesystem')).toBeVisible();
  await expect(addDialog.getByText('existing', { exact: true })).toBeVisible();
  await expect(addDialog.getByText('skipped · duplicate')).toBeVisible();
  await expect(addDialog.getByText('failed · field')).toBeVisible();
  await expect(addDialog.getByText('at least one transport is required')).toBeVisible();
});

test('paste JSON single-object enables the name field; malformed shows the error', async ({ page }) => {
  await mockChat(page);
  await mockMcpList(page, []);
  await page.route('**/api/mcps/import', (route) => {
    const body = route.request().postDataJSON() as { document: string; name?: string };
    if (body.name !== 'solo') {
      return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ code: 'invalid_import', message: 'a single-server paste needs a name (fill the name field)' }) });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [{ source_key: 'solo', name: 'solo', status: 'created' }] }) });
  });
  await openSettings(page, 'MCP');
  const addDialog = await openAddDialog(page);
  await addDialog.getByRole('tab', { name: 'Paste JSON' }).click();
  const nameField = addDialog.getByRole('textbox', { name: /Server name/ });
  await expect(nameField).toBeDisabled();
  await addDialog.getByRole('textbox', { name: 'Paste standard mcpServers JSON' }).fill('{"command": "solo-runner"}');
  await expect(nameField).toBeEnabled();
  // Importing without the name shows the daemon's typed error.
  await addDialog.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(addDialog.getByText('a single-server paste needs a name')).toBeVisible();
  await nameField.fill('solo');
  await addDialog.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(addDialog.getByText('solo', { exact: false })).toBeVisible();
});

test('edit as JSON round-trips through replace mode', async ({ page }) => {
  await mockChat(page);
  const server = {
    name: 'fs', file: '/home/u/.zbot/config/mcps/fs.yaml',
    transports: [{ kind: 'stdio', command: 'npx', args: ['-y', 'fs-server', '/a dir', ''], env: { MULTILINE: 'line1\nline2' } }],
  };
  await mockMcpList(page, [server]);
  const seenImport: { body?: { document: string; replaceName?: string } } = {};
  await page.route('**/api/mcps/import', (route) => {
    seenImport.body = route.request().postDataJSON();
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [{ source_key: 'fs', name: 'fs', status: 'created' }] }) });
  });
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /fs/ }).click();
  await dialog.getByRole('button', { name: 'Edit', exact: true }).click();
  const editDialog = page.getByRole('dialog', { name: 'Edit fs' });
  await editDialog.getByRole('tab', { name: 'Paste JSON' }).click();
  // Prefilled with the server's standard block (args byte-exact).
  const area = editDialog.getByRole('textbox', { name: 'Paste standard mcpServers JSON' });
  const prefilled = await area.inputValue();
  const parsed = JSON.parse(prefilled).mcpServers.fs;
  // Newline-bearing args, an empty argument, and multiline env values all
  // round-trip byte-exactly through the JSON editor.
  expect(parsed.args).toEqual(['-y', 'fs-server', '/a dir', '']);
  expect(parsed.env.MULTILINE).toBe('line1\nline2');
  await editDialog.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => seenImport.body !== undefined).toBe(true);
  expect(seenImport.body?.replaceName).toBe('fs');
  const doc = JSON.parse(seenImport.body!.document);
  expect(doc.mcpServers.fs.args).toEqual(['-y', 'fs-server', '/a dir', '']);
  expect(doc.mcpServers.fs.env.MULTILINE).toBe('line1\nline2');
  await expect(editDialog).toHaveCount(0); // clean replace closes the dialog
});

test('edit as JSON keeps the report visible when the replace has warnings', async ({ page }) => {
  await mockChat(page);
  const server = { name: 'hdr', file: '/h.yaml', transports: [{ kind: 'http', url: 'https://x/mcp' }] };
  await mockMcpList(page, [server]);
  await page.route('**/api/mcps/import', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [
      { source_key: 'hdr', name: 'hdr', status: 'created', warnings: ['ignored headers (HTTP headers are not supported on managed manifests)'] },
    ] }) }),
  );
  const dialog = await openSettings(page, 'MCP');
  await dialog.getByRole('button', { name: /hdr/ }).click();
  await dialog.getByRole('button', { name: 'Edit', exact: true }).click();
  const editDialog = page.getByRole('dialog', { name: 'Edit hdr' });
  await editDialog.getByRole('tab', { name: 'Paste JSON' }).click();
  await editDialog.getByRole('textbox', { name: 'Paste standard mcpServers JSON' }).fill('{"mcpServers": {"hdr": {"url": "https://x/mcp", "headers": {"X": "y"}}}}');
  await editDialog.getByRole('button', { name: 'Save changes' }).click();
  // Warnings keep the dialog open with the report rendered.
  await expect(editDialog.getByText('ignored headers')).toBeVisible();
  await expect(editDialog).toHaveCount(1);
});

test('closing mid-import still reconciles the list', async ({ page }) => {
  await mockChat(page);
  const servers: unknown[] = [];
  await page.route('**/api/mcps', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ servers }) });
    }
    return route.fallback();
  });
  await page.route('**/api/mcps/import', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 600)); // in-flight while the user closes
    servers.push({ name: 'slow', file: '/s.yaml', transports: [{ kind: 'stdio', command: 'x' }] });
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ results: [{ source_key: 'slow', name: 'slow', status: 'created' }] }) });
  });
  const dialog = await openSettings(page, 'MCP');
  const addDialog = await openAddDialog(page);
  await addDialog.getByRole('tab', { name: 'Paste JSON' }).click();
  await addDialog.getByRole('textbox', { name: 'Paste standard mcpServers JSON' }).fill('{"mcpServers": {"slow": {"command": "x"}}}');
  await addDialog.getByRole('button', { name: 'Import', exact: true }).click();
  await addDialog.getByRole('button', { name: 'Close dialog' }).click();
  // The import still lands and the list reconciles behind the closed dialog.
  await expect(dialog.getByRole('button', { name: /slow/ })).toBeVisible({ timeout: 5000 });
});

test('paste JSON shows document-level rejections from the daemon', async ({ page }) => {
  await mockChat(page);
  await mockMcpList(page, []);
  await page.route('**/api/mcps/import', (route) => {
    const body = route.request().postDataJSON() as { document: string };
    if (body.document.includes('too-many')) {
      return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ code: 'invalid_import', message: 'the document holds 33 servers; the import cap is 32' }) });
    }
    return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ code: 'invalid_import', message: 'trailing content after the document' }) });
  });
  await openSettings(page, 'MCP');
  const addDialog = await openAddDialog(page);
  await addDialog.getByRole('tab', { name: 'Paste JSON' }).click();
  const area = addDialog.getByRole('textbox', { name: 'Paste standard mcpServers JSON' });
  await area.fill('{"mcpServers": {"a": {"command": "x"}}} trailing');
  await addDialog.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(addDialog.getByText('trailing content after the document')).toBeVisible();
  await area.fill('{"mcpServers": {"too-many": {"command": "x"}}}');
  await addDialog.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(addDialog.getByText('the import cap is 32')).toBeVisible();
});

test('skills list, body round-trip, and load errors', async ({ page }) => {
  await mockChat(page);
  await mockSkillList(page, [
    { name: 'Code review', description: 'Review a change.', directory: '/home/u/.zbot/config/skills/code-review' },
  ], [
    { path: '/home/u/.zbot/config/skills/broken/SKILL.md', kind: 'skill', message: 'frontmatter requires name and description' },
  ]);
  await page.route('**/api/skills/Code%20review', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ name: 'Code review', description: 'Review a change.', directory: '/home/u/.zbot/config/skills/code-review', body: '# Code review\n\n- [ ] tests' }) }),
  );
  const dialog = await openSettings(page, 'Skills');
  await expect(dialog.getByRole('button', { name: /Code review/ })).toContainText('Review a change.');
  await dialog.getByRole('button', { name: /Code review/ }).click();
  await expect(dialog.getByRole('textbox', { name: 'SKILL.md body' })).toHaveValue('# Code review\n\n- [ ] tests');
  await expect(dialog.getByText('broken/SKILL.md')).toBeVisible();
});

test('skill edit and rename via PATCH (round-trips the body)', async ({ page }) => {
  await mockChat(page);
  const skill = { name: 'Project planning', description: 'Break it down.', directory: '/home/u/.zbot/config/skills/project-planning', origin: 'local' };
  const skills = [skill];
  await page.route('**/api/skills', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills }) });
    }
    return route.fallback();
  });
  await page.route('**/api/skills/Project%20planning', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...skill, body: '# Plan', origin: 'local' }) });
    }
    if (route.request().method() === 'PATCH') {
      const body = route.request().postDataJSON() as { name?: string; description: string; body: string };
      const renamed = body.name ?? skill.name;
      skills[0] = { ...skill, name: renamed, description: body.description, directory: `/home/u/.zbot/config/skills/${renamed.toLowerCase().replace(/\s+/g, '-')}` };
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...skills[0], body: body.body }) });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'Skills');
  // Manual creation is gone: the Installed view offers Import skill only.
  await expect(dialog.getByRole('button', { name: 'Import skill' })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Add skill', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Browse marketplace' })).toHaveCount(0);
  await dialog.getByRole('button', { name: /Project planning/ }).click();
  await dialog.getByRole('button', { name: 'Edit', exact: true }).click();
  const editForm = page.getByRole('form', { name: 'Edit Project planning' });
  await expect(editForm.getByRole('textbox', { name: 'Body (markdown)' })).toHaveValue('# Plan');
  await editForm.getByRole('textbox', { name: 'Description' }).fill('Scope it down.');
  await editForm.getByRole('textbox', { name: 'Name' }).fill('Project scoping');
  await editForm.getByRole('button', { name: 'Save changes' }).click();
  await expect(editForm).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: /Project scoping/ })).toBeVisible();
});

test('skill delete confirmation names the directory and bundle', async ({ page }) => {
  await mockChat(page);
  const skills: unknown[] = [{ name: 'pdf tools', description: 'PDF helpers.', directory: '/home/u/.zbot/config/skills/pdf-tools' }];
  await page.route('**/api/skills', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ skills }) });
    }
    return route.fallback();
  });
  await page.route('**/api/skills/pdf%20tools', (route) => {
    if (route.request().method() === 'DELETE') {
      skills.length = 0;
      return route.fulfill({ status: 204, body: '' });
    }
    return route.fallback();
  });
  const dialog = await openSettings(page, 'Skills');
  await dialog.getByRole('button', { name: /pdf tools/ }).click();
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog.getByText('including any bundled files')).toBeVisible();
  await expect(dialog.getByText('/home/u/.zbot/config/skills/pdf-tools')).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete skill and bundle' }).click();
  await expect(dialog.getByText('No skills installed.')).toBeVisible();
});

test('skills daemon unreachable is explicit', async ({ page }) => {
  await mockChat(page);
  await page.route('**/api/skills', (route) => route.abort());
  const dialog = await openSettings(page, 'Skills');
  await expect(dialog.getByText('Daemon unreachable.')).toBeVisible();
});
