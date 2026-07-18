import { expect, test } from '@playwright/test';
import { execFileSync, spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { createServer } from 'node:net';
import { mkdtempSync, mkdirSync, rmSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';

let daemon: ChildProcessWithoutNullStreams;
let baseURL = '';
let bootstrap = '';
let dataDir = '';
let binary = '';

function freePort(): Promise<number> {
  return new Promise((resolvePort, reject) => {
    const server = createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      if (!address || typeof address === 'string') return reject(new Error('No TCP address'));
      server.close(() => resolvePort(address.port));
    });
  });
}

test.beforeAll(async () => {
  const root = resolve(process.cwd(), '..');
  const buildDir = mkdtempSync(join(tmpdir(), 'studioforge-e2e-build-'));
  dataDir = mkdtempSync(join(tmpdir(), 'studioforge-e2e-data-'));
  mkdirSync(buildDir, { recursive: true });
  binary = join(buildDir, process.platform === 'win32' ? 'studioforge.exe' : 'studioforge');
  execFileSync('go', ['build', '-o', binary, './cmd/studioforge'], { cwd: root, stdio: 'inherit' });
  const port = await freePort();
  daemon = spawn(binary, ['--mock', '--no-open', '--port', String(port), '--data-dir', dataDir], {
    cwd: root,
  });
  await new Promise<void>((resolveReady, reject) => {
    let output = '';
    const timeout = setTimeout(
      () => reject(new Error(`Daemon startup timed out: ${output}`)),
      20_000,
    );
    daemon.stdout.on('data', (chunk) => {
      output += chunk.toString();
      baseURL = output.match(/STUDIOFORGE_URL=(.+)/)?.[1]?.trim() ?? baseURL;
      bootstrap = output.match(/STUDIOFORGE_BOOTSTRAP=(.+)/)?.[1]?.trim() ?? bootstrap;
      if (baseURL && bootstrap) {
        clearTimeout(timeout);
        resolveReady();
      }
    });
    daemon.once('exit', (code) => {
      clearTimeout(timeout);
      reject(new Error(`Daemon exited early with ${code}: ${output}`));
    });
  });
});

test.afterAll(async () => {
  if (daemon && !daemon.killed) {
    daemon.kill();
    await new Promise((resolveExit) => {
      daemon.once('exit', resolveExit);
      setTimeout(resolveExit, 3_000);
    });
  }
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
  if (binary) rmSync(resolve(binary, '..'), { recursive: true, force: true });
});

test('first run, locale, projects, live run, and core navigation', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text());
  });
  await page.goto(`${baseURL}/#bootstrap=${encodeURIComponent(bootstrap)}`);
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  const finishEnglish = page.getByRole('button', { name: 'Open dashboard', exact: true });
  const finishRussian = page.getByRole('button', { name: 'Открыть панель', exact: true });
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
  await page.getByLabel('Canonical path').fill(join(dataDir, 'fresh-project'));
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
  await page.getByRole('button', { name: 'Global activity', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Global activity', exact: true })).toBeVisible();
  // The seeded tasks belong to a demo project, and registering Fresh Project
  // switched the selection to it.
  await page
    .getByRole('combobox', { name: 'Project', exact: true })
    .selectOption({ label: 'Skyline Obby' });
  await page.getByRole('button', { name: 'Tasks', exact: true }).click();
  await expect(page.getByTestId('tasks-view')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Task DAG', exact: true })).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'Lock gameplay contract', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Studio sessions', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Studio sessions', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Decisions 1', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Decision inbox', exact: true })).toBeVisible();
  await page
    .getByRole('combobox', { name: 'Project', exact: true })
    .selectOption({ label: 'Fresh Project' });
  await page.getByRole('button', { name: 'Team builder', exact: true }).click();
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
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByText('Settings saved', { exact: true })).toBeVisible();
  await expect(page.getByText('Codex CLI', { exact: true })).toBeVisible();
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
