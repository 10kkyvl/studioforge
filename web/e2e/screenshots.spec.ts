import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { startDaemon, stopDaemon, type DaemonHandle } from './helpers';

test.use({ locale: 'en-US' });

let handle: DaemonHandle;
const outDir = resolve(process.cwd(), '..', 'docs', 'screenshots');

test.beforeAll(async () => {
  mkdirSync(outDir, { recursive: true });
});

test.afterAll(async () => {
  if (handle) await stopDaemon(handle);
});

async function settleUI(page: Page) {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.addStyleTag({
    content:
      '*{transition:none!important;animation:none!important;caret-color:transparent!important}',
  });
}

test('capture documentation screenshots', async ({ page, browser }) => {
  const isWindows = process.platform === 'win32';
  const minimalPath = isWindows ? 'C:\\Windows\\System32' : '/usr/bin';
  const wizardEnv: NodeJS.ProcessEnv = { PATH: minimalPath, Path: minimalPath };
  if (isWindows) wizardEnv.LOCALAPPDATA = '';
  const wizardHandle = await startDaemon({ env: wizardEnv });
  try {
    const wizardContext = await browser.newContext({
      locale: 'en-US',
      viewport: { width: 1280, height: 1100 },
    });
    try {
      const wizardPage = await wizardContext.newPage();
      await wizardPage.goto(
        `${wizardHandle.baseURL}/#bootstrap=${encodeURIComponent(wizardHandle.bootstrap)}`,
      );
      await settleUI(wizardPage);
      const wizardDialog = wizardPage.getByRole('dialog');
      await expect(wizardDialog).toBeVisible();
      await expect(wizardPage.getByRole('heading', { name: 'First run setup' })).toBeVisible();
      await expect(
        wizardDialog.getByRole('heading', { name: 'Required', exact: true }),
      ).toBeVisible();
      await expect(
        wizardDialog.getByRole('heading', { name: 'Integrations', exact: true }),
      ).toBeVisible();
      await expect(wizardDialog.locator('[data-check="claude"]')).toBeVisible();
      await expect(wizardDialog.locator('[data-check="studioMcp"]')).toBeVisible();
      await expect(wizardDialog).not.toContainText('C:\\Users');
      const contentHeight = await wizardDialog.evaluate((el) => el.scrollHeight);
      const requiredHeight = Math.max(1100, Math.ceil(contentHeight / 0.9) + 60);
      await wizardPage.setViewportSize({ width: 1280, height: requiredHeight });
      await expect
        .poll(() => wizardPage.evaluate(() => window.innerHeight))
        .toBeGreaterThanOrEqual(requiredHeight);
      const settledHeight = await wizardDialog.evaluate((el) => el.scrollHeight);
      expect(settledHeight, 'wizard dialog must not clip its bottom row').toBeLessThanOrEqual(
        Math.floor(requiredHeight * 0.9),
      );
      await wizardDialog.screenshot({ path: resolve(outDir, 'first-run.png') });
    } finally {
      await wizardContext.close();
    }
  } finally {
    await stopDaemon(wizardHandle);
  }

  handle = await startDaemon();
  await page.goto(`${handle.baseURL}/#bootstrap=${encodeURIComponent(handle.bootstrap)}`);
  await settleUI(page);

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.locator('button.primary').click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible();
  for (const name of ['Skyline Obby', 'Harbor Tycoon', 'Neon Arena'])
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  await settleUI(page);
  await page.screenshot({ path: resolve(outDir, 'dashboard.png'), fullPage: false });

  await page.setViewportSize({ width: 900, height: 600 });
  await expect.poll(() => page.evaluate(() => window.innerWidth)).toBe(900);
  await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible();
  await page.screenshot({ path: resolve(outDir, 'dashboard-900x600.png'), fullPage: false });

  await page.setViewportSize({ width: 1280, height: 800 });
  await expect.poll(() => page.evaluate(() => window.innerWidth)).toBe(1280);

  await page.getByRole('button', { name: 'Chat', exact: true }).click();
  await expect(page.locator('.chat-layout')).toBeVisible();
  await page
    .getByRole('combobox', { name: 'Project', exact: true })
    .selectOption({ label: 'Skyline Obby' });
  await expect(page.getByRole('heading', { name: 'Chat', level: 1 })).toBeVisible();
  const leadSelect = page.getByRole('combobox', { name: 'Lead agent', exact: true });
  await expect(leadSelect).toBeVisible();
  await leadSelect.selectOption({ label: 'Forge Lead' });
  await expect(leadSelect).toHaveValue(
    await page
      .locator('.lead-select option', { hasText: 'Forge Lead' })
      .evaluate((option: HTMLOptionElement) => option.value),
  );

  const composer = page.locator('.composer textarea');
  await composer.fill('Add collectible coins with a pickup sound to the first three checkpoints');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.bubble-user', { hasText: 'Add collectible coins' })).toBeVisible({
    timeout: 30_000,
  });
  await expect(
    page.getByText('Acceptance criteria verified in mock mode.', { exact: false }),
  ).toBeVisible({ timeout: 30_000 });
  await settleUI(page);
  await page.screenshot({ path: resolve(outDir, 'chat-run.png'), fullPage: false });

  const diffOrMuted = page.locator('.diff-panel, .diff-muted').first();
  await expect(diffOrMuted).toBeVisible({ timeout: 30_000 });
  const rollbackButton = page.locator('.rollback-button');
  const hasRollback = (await rollbackButton.count()) > 0;
  if (hasRollback) {
    await rollbackButton.click();
    await expect(page.locator('.rollback-confirm')).toBeVisible();
    await settleUI(page);
    await page.screenshot({ path: resolve(outDir, 'run-diff.png'), fullPage: false });
  } else {
    await page.getByRole('button', { name: 'Runs', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Runs', exact: true })).toBeVisible();
    const skylineRuns = page.locator('.run-row', { hasText: 'Skyline Obby' });
    await expect(skylineRuns).toHaveCount(2, { timeout: 30_000 });
    const skylineRun = skylineRuns.first();
    await skylineRun.click();
    await expect(page.locator('.event-panel header code')).toBeVisible();
    await expect(page.locator('.event-panel .status-completed').first()).toBeVisible();
    await settleUI(page);
    await page.screenshot({ path: resolve(outDir, 'run-diff.png'), fullPage: false });
    await page.getByRole('button', { name: 'Chat', exact: true }).click();
    await expect(page.locator('.chat-layout')).toBeVisible();
  }

  await page.getByRole('button', { name: 'Studio sessions', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Studio sessions', exact: true })).toBeVisible();
  await expect(page.locator('.studio-card .chip').first()).toBeVisible();
  await settleUI(page);
  await page.screenshot({ path: resolve(outDir, 'studio-sessions.png'), fullPage: false });
});
