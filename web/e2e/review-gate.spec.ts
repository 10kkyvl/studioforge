import { expect, test } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { chooseOption, startDaemon, stopDaemon, type DaemonHandle } from './helpers';

const MOCK_EDIT_FILE = 'studioforge-mock-edit.txt';

let handle: DaemonHandle;

test.beforeAll(async () => {
  handle = await startDaemon();
});

test.afterAll(async () => {
  await stopDaemon(handle);
});

function git(cwd: string, ...args: string[]): void {
  execFileSync('git', args, { cwd });
}

test('review-before-apply gate parks a mock run and rejecting it reverts the mock edit on disk', async ({
  page,
}) => {
  const repoPath = join(handle.projectsDir, 'review-gate-project');
  mkdirSync(repoPath, { recursive: true });
  git(repoPath, 'init');
  git(repoPath, 'config', 'user.email', 'e2e@example.invalid');
  git(repoPath, 'config', 'user.name', 'StudioForge E2E');
  writeFileSync(join(repoPath, 'README.md'), 'Review gate e2e fixture.\n');

  await page.goto(`${handle.baseURL}/#bootstrap=${encodeURIComponent(handle.bootstrap)}`);
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.locator('[data-check="studioMcp"]').getByRole('button').click();
  await expect(
    page
      .getByRole('heading', { name: 'Settings', exact: true })
      .or(page.getByRole('heading', { name: 'Настройки', exact: true })),
  ).toBeVisible();
  const resumeSetup = page
    .getByRole('button', { name: 'Finish setup', exact: true })
    .or(page.getByRole('button', { name: 'Завершить настройку', exact: true }));
  await expect(resumeSetup).toBeVisible();
  await resumeSetup.click();
  await expect(dialog).toBeVisible();
  await dialog
    .getByRole('button', { name: 'Recheck', exact: true })
    .or(dialog.getByRole('button', { name: 'Проверить снова', exact: true }))
    .click();
  await expect(dialog).toBeVisible();
  const finishEnglish = dialog.getByRole('button', {
    name: 'Continue in limited mode',
    exact: true,
  });
  const finishRussian = dialog.getByRole('button', {
    name: 'Продолжить в ограниченном режиме',
    exact: true,
  });
  if (await finishEnglish.isVisible()) await finishEnglish.click();
  else await finishRussian.click();
  await expect(dialog).toBeHidden();
  const russianToggle = page.getByRole('button', { name: 'RU', exact: true });
  if (await russianToggle.isVisible()) await russianToggle.click();
  await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'New project', exact: true }).click();
  await page.getByLabel('Project name').fill('Review Gate Project');
  await page.getByLabel('Project folder').fill(repoPath);
  await page.getByLabel('Description').fill('Created by the review gate e2e test.');
  await page.getByRole('button', { name: 'Register project', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Chat', level: 1 })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Project', exact: true })).toContainText(
    'Review Gate Project',
  );

  await page.getByRole('button', { name: 'Team builder', exact: true }).click();
  await chooseOption(page, 'Project', 'Review Gate Project');
  const agentCard = page
    .locator('article.agent-card')
    .filter({ has: page.getByRole('heading', { name: 'Default Agent', exact: true }) });
  await expect(agentCard).toBeVisible();
  await chooseOption(agentCard, 'Provider', 'Demo (no AI)');
  await agentCard.getByLabel('Require review before applying changes', { exact: true }).check();
  await agentCard.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByText('Agent updated', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Chat', exact: true }).click();
  await page.locator('.composer textarea').fill('Build the review gate fixture, please.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();

  const reviewCard = page.locator('.review-card');
  await expect(reviewCard).toBeVisible({ timeout: 15_000 });
  await expect(
    reviewCard.getByText('Changes waiting on your review', { exact: true }),
  ).toBeVisible();
  await expect(reviewCard.getByText(MOCK_EDIT_FILE)).toBeVisible();
  await expect(reviewCard.getByRole('button', { name: 'Apply', exact: true })).toBeVisible();
  await expect(reviewCard.getByRole('button', { name: 'Reject', exact: true })).toBeVisible();
  await expect(
    reviewCard.getByRole('button', { name: 'Apply selected', exact: true }),
  ).toBeVisible();

  expect(existsSync(join(repoPath, MOCK_EDIT_FILE))).toBe(true);

  await reviewCard.getByRole('button', { name: 'Reject', exact: true }).click();
  await reviewCard
    .getByRole('button', { name: 'Reject every change in this run?', exact: true })
    .click();
  await expect(reviewCard).toBeHidden({ timeout: 10_000 });

  await expect
    .poll(() => existsSync(join(repoPath, MOCK_EDIT_FILE)), { timeout: 10_000 })
    .toBe(false);
});
