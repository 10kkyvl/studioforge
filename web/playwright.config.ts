import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  timeout: 45_000,
  fullyParallel: false,
  workers: 1,
  use: {
    headless: true,
    viewport: { width: 1280, height: 800 },
    trace: 'retain-on-failure',
  },
  reporter: [['list']],
  projects: [
    { name: 'e2e', testMatch: /studioforge\.spec\.ts/ },
    { name: 'screenshots', testMatch: /screenshots\.spec\.ts/ },
    { name: 'social', testMatch: /social-preview\.spec\.ts/ },
  ],
});
