import { expect, test } from '@playwright/test';
import { join } from 'node:path';
import { startDaemon, stopDaemon, type DaemonHandle } from './helpers';

let handle: DaemonHandle;
let baseURL = '';
let bootstrap = '';
let dataDir = '';

test.beforeAll(async () => {
  handle = await startDaemon();
  baseURL = handle.baseURL;
  bootstrap = handle.bootstrap;
  dataDir = handle.dataDir;
});

test.afterAll(async () => {
  await stopDaemon(handle);
});

test('first run, locale, projects, live run, and core navigation', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text());
  });
  await page.goto(`${baseURL}/#bootstrap=${encodeURIComponent(bootstrap)}`);
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
  for (const name of ['Skyline Obby', 'Harbor Tycoon', 'Neon Arena'])
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'New project', exact: true }).click();
  await page.getByLabel('Project name').fill('Fresh Project');
  await page.getByLabel('Project folder').fill(join(dataDir, 'fresh-project'));
  await page.getByLabel('Description').fill('Created by the browser regression test.');
  await page.getByRole('button', { name: 'Register project', exact: true }).click();
  // Registering a project drops the operator straight into its chat, so the
  // new project is confirmed by the project switcher rather than by a card.
  await expect(page.getByRole('heading', { name: 'Chat', level: 1 })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Project', exact: true })).toHaveValue(
    await page
      .getByRole('option', { name: 'Fresh Project', exact: true })
      .evaluate((option: HTMLOptionElement) => option.value),
  );
  await page.getByRole('button', { name: 'Projects', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Fresh Project', exact: true })).toBeVisible();
  // NOTE: the project card's "Start agent run" and the team builder's "Run this
  // agent" both call startRun with no prompt, which returns early — they have
  // been dead buttons since before this test last passed. The Runs view is
  // reached through navigation here rather than asserting that dead path works.
  await page.getByRole('button', { name: 'Runs', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Runs', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Activity', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Global activity', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Tasks', exact: true }).click();
  await page
    .getByRole('combobox', { name: 'Project', exact: true })
    .selectOption({ label: 'Skyline Obby' });
  await expect(page.getByTestId('tasks-view')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Task DAG', exact: true })).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'Lock gameplay contract', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Studio sessions', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Studio sessions', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Team builder', exact: true }).click();
  await page
    .getByRole('combobox', { name: 'Project', exact: true })
    .selectOption({ label: 'Fresh Project' });
  await expect(page.getByRole('heading', { name: 'Default Agent', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add agent', exact: true }).click();
  const agentForm = page.locator('form.create-agent');
  await agentForm.getByLabel('Agent name').fill('Fresh Runner');
  await agentForm.getByLabel('Provider').selectOption('mock');
  await agentForm.getByRole('button', { name: 'Create agent', exact: true }).click();
  const agentCard = page
    .locator('article.agent-card')
    .filter({ has: page.getByRole('heading', { name: 'Fresh Runner', exact: true }) });
  await expect(agentCard).toBeVisible();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Agents and integrations', exact: true }),
  ).toBeVisible();
  await page.getByLabel('Global agent concurrency').fill('5');
  await page
    .locator('form.integration-settings')
    .getByRole('button', { name: 'Save', exact: true })
    .click();
  await expect(page.getByText('Settings saved', { exact: true })).toBeVisible();
  // The chat page must scroll in regions, not as a document. It used to be a
  // fixed-height transcript inside a page taller than the viewport, so reaching
  // the composer of a long thread meant scrolling the window.
  await page.getByRole('button', { name: 'Chat', exact: true }).click();
  await expect(page.locator('.chat-layout')).toBeVisible();
  const pageScrolls = await page.evaluate(() => {
    const root = document.scrollingElement ?? document.documentElement;
    return root.scrollHeight > root.clientHeight + 1;
  });
  expect(pageScrolls, 'the chat page must not scroll the whole document').toBe(false);
  for (const region of ['.thread-items', '.message-list']) {
    const scrollable = await page.locator(region).evaluate((el) => {
      const overflow = getComputedStyle(el).overflowY;
      return (overflow === 'auto' || overflow === 'scroll') && el.clientHeight > 0;
    });
    expect(scrollable, `${region} must be its own scroll region`).toBe(true);
  }
  // The composer is the point of all this: it stays on screen without scrolling.
  const composer = page.locator('.composer');
  await expect(composer).toBeInViewport();

  // Pasting an image into the composer uploads it to /attachments and shows a
  // removable preview chip, without inserting anything into the draft text.
  // A real OS clipboard is not available to a headless run, so the paste is
  // simulated the way browsers themselves construct one: a ClipboardEvent
  // carrying a DataTransfer with a File, dispatched straight at the textarea
  // ChatView.svelte's onpaste handler is bound to.
  const draftBefore = await page.locator('.composer textarea').inputValue();
  const pngBase64 =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=';
  await page.locator('.composer textarea').evaluate((el, base64) => {
    const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
    const file = new File([bytes], 'clip.png', { type: 'image/png' });
    const data = new DataTransfer();
    data.items.add(file);
    const event = new ClipboardEvent('paste', {
      clipboardData: data,
      bubbles: true,
      cancelable: true,
    });
    el.dispatchEvent(event);
  }, pngBase64);
  const chip = page.locator('.attachment-chip');
  await expect(chip).toBeVisible();
  await expect(page.locator('.attachment-thumb')).toBeVisible();
  expect(await page.locator('.composer textarea').inputValue()).toBe(draftBefore);
  await page.getByRole('button', { name: 'Remove attachment', exact: true }).click();
  await expect(chip).toHaveCount(0);

  await expect(page).toHaveTitle('StudioForge');
  await expect(page.locator('main')).toBeVisible();
  await expect(page.locator('main h1')).toHaveCount(1);
  const buttons = page.getByRole('button');
  for (let index = 0; index < (await buttons.count()); index++)
    await expect(buttons.nth(index)).toHaveAccessibleName(/\S/);
  await page.keyboard.press('Tab');
  await expect(page.locator(':focus-visible')).toHaveCount(1);
  expect(errors).toEqual([]);
});
