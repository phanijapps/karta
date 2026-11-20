import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  workers: 2,
  reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:9245', viewport: { width: 1440, height: 900 }, colorScheme: 'dark' },
  webServer: { command: 'pnpm dev', url: 'http://127.0.0.1:9245', reuseExistingServer: !process.env.CI },
});
