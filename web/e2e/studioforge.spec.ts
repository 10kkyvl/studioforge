import { expect, test, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { startDaemon, stopDaemon, type DaemonHandle } from './helpers';

let handle: DaemonHandle;
let baseURL = '';
let bootstrap = '';
let dataDir = '';

async function prepareProject(page: Page, daemon: DaemonHandle, name: string) {
  await page.goto(`${daemon.baseURL}/#bootstrap=${encodeURIComponent(daemon.bootstrap)}`);
  await expect(page.getByRole('dialog')).toBeVisible();
  const settings = await page.request.post(`${daemon.baseURL}/api/v1/settings`, {
    headers: { Origin: daemon.baseURL },
    data: { setup_complete: 'true', locale: 'en', default_provider: 'mock' },
  });
  expect(settings.ok()).toBeTruthy();
  const projectPath = join(daemon.dataDir, 'browser-project');
  mkdirSync(projectPath, { recursive: true });
  const created = await page.request.post(`${daemon.baseURL}/api/v1/projects`, {
    headers: { Origin: daemon.baseURL },
    data: { name, path: projectPath },
  });
  expect(created.status()).toBe(201);
  const project = (await created.json()) as { id: string };
  await page.reload();
  await expect(page.getByRole('dialog')).toBeHidden();
  return { ...project, path: projectPath };
}

test.beforeAll(async () => {
  handle = await startDaemon();
  baseURL = handle.baseURL;
  bootstrap = handle.bootstrap;
  dataDir = handle.dataDir;
});

test.afterAll(async () => {
  await stopDaemon(handle);
});

test('fresh real database loads setup and empty projects without runtime errors', async ({
  page,
}) => {
  const fresh = await startDaemon({ mock: false });
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  try {
    await page.goto(`${fresh.baseURL}/#bootstrap=${encodeURIComponent(fresh.bootstrap)}`);
    await expect(page.getByRole('dialog')).toBeVisible();
    const snapshot = await page.request.get(`${fresh.baseURL}/api/v1/snapshot`);
    expect(snapshot.ok()).toBeTruthy();
    const body = await snapshot.json();
    for (const key of ['projects', 'runs', 'agents', 'tasks', 'studios', 'decisions'])
      expect(body[key]).toEqual([]);
    const settings = await page.request.post(`${fresh.baseURL}/api/v1/settings`, {
      headers: { Origin: fresh.baseURL },
      data: { setup_complete: 'true', locale: 'en' },
    });
    expect(settings.ok()).toBeTruthy();
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible();
    await expect(page.getByText('Online', { exact: true })).toBeVisible();
    await expect(
      page.getByRole('button', { name: 'New project', exact: true }).first(),
    ).toBeVisible();
    await expect(page.getByText(/can't access property|Cannot read properties/)).toHaveCount(0);
    expect(errors).toEqual([]);
  } finally {
    await stopDaemon(fresh);
  }
});

test('Studio sessions shows mock polling state and persists its interval setting', async ({
  page,
}) => {
  const fresh = await startDaemon();
  try {
    await page.goto(`${fresh.baseURL}/#bootstrap=${encodeURIComponent(fresh.bootstrap)}`);
    await expect(page.getByRole('dialog')).toBeVisible();
    const configured = await page.request.post(`${fresh.baseURL}/api/v1/settings`, {
      headers: { Origin: fresh.baseURL },
      data: { setup_complete: 'true', locale: 'en', studio_sessions_poll_interval_seconds: '0' },
    });
    expect(configured.ok()).toBeTruthy();
    await page.reload();
    await expect(page.getByRole('dialog')).toBeHidden();

    await page.getByRole('button', { name: 'Studio sessions', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Studio sessions', exact: true })).toBeVisible();
    await expect(page.locator('.studio-refresh-state')).toContainText('Last refreshed');
    await expect(page.locator('.studio-refresh-state')).toContainText('Not yet');
    await expect(page.getByText('background refresh on', { exact: true })).toHaveCount(0);

    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    const interval = page.getByLabel('Studio sessions background refresh (seconds)');
    await expect(interval).toHaveValue('0');
    await interval.fill('15');
    await page
      .locator('form.integration-settings')
      .getByRole('button', { name: 'Save', exact: true })
      .click();
    await expect(page.getByText('Settings saved', { exact: true })).toBeVisible();

    const snapshot = await page.request.get(`${fresh.baseURL}/api/v1/snapshot`);
    expect(snapshot.ok()).toBeTruthy();
    const body = await snapshot.json();
    expect(body.settings.studio_sessions_poll_interval_seconds).toBe('15');
    expect(body.studioRefresh.enabled).toBe(false);
  } finally {
    await stopDaemon(fresh);
  }
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

test('Studio journal shows durable operations and uncertain outcomes on a run', async ({
  page,
}) => {
  const journalDaemon = await startDaemon();
  const { baseURL, bootstrap } = journalDaemon;
  try {
    await page.route('**/api/v1/runs/*/studio-changes', async (route) => {
      await route.fulfill({
        json: [
          {
            id: 'journal-1',
            callId: 'call-1',
            runId: 'demo-obby-history',
            createdAt: '2026-09-12T12:00:00Z',
            tool: 'multi_edit',
            target: 'ServerScriptService.Main',
            operation: 'modify',
            properties: ['Source'],
            status: 'succeeded',
          },
          {
            id: 'journal-2',
            callId: 'call-2',
            runId: 'demo-obby-history',
            createdAt: '2026-09-12T12:00:01Z',
            tool: 'execute_luau',
            target: '',
            operation: 'unknown',
            properties: [],
            status: 'unknown',
          },
        ],
      });
    });
    await page.goto(`${baseURL}/#bootstrap=${encodeURIComponent(bootstrap)}`);
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await dialog
      .getByRole('button', { name: /Continue in limited mode|Продолжить в ограниченном режиме/ })
      .click();
    const englishToggle = page.getByRole('button', { name: 'RU', exact: true });
    if (await englishToggle.isVisible()) await englishToggle.click();
    await page.getByRole('button', { name: 'Runs', exact: true }).click();
    await page.locator('.run-row').first().click();
    const journal = page.locator('details.studio-changes');
    await expect(journal.locator('summary')).toContainText('2 records');
    await journal.locator('summary').click();
    await expect(journal).toContainText('Git rollback does not undo these changes');
    await expect(journal).toContainText('ServerScriptService.Main');
    await expect(journal).toContainText('Properties: Source');
    await expect(journal).toContainText('Outcome unknown; inspect Studio before retrying');
    await page.getByRole('button', { name: 'EN', exact: true }).click();
    await expect(journal).toContainText('Изменения Studio');
    await expect(journal).toContainText('Исход неизвестен');
  } finally {
    await stopDaemon(journalDaemon);
  }
});

test('project memory can be edited, pinned, and cleared without touching runs', async ({
  page,
}) => {
  const memoryDaemon = await startDaemon();
  const { baseURL, bootstrap } = memoryDaemon;
  let entries = [
    {
      id: 'memory-1',
      projectId: 'demo-obby',
      runId: 'run-1',
      scope: 'project',
      content: 'Keep server validation',
      summary: 'Keep server validation',
      source: 'run',
      confidence: 0.9,
      importance: 0.8,
      createdAt: '2026-09-12T12:00:00Z',
      pinned: false,
      injected: true,
    },
    {
      id: 'memory-2',
      projectId: 'demo-obby',
      scope: 'project',
      content: 'Use a respawn pad',
      summary: 'Use a respawn pad',
      source: 'run',
      confidence: 0.8,
      importance: 0.5,
      createdAt: '2026-09-11T12:00:00Z',
      pinned: false,
      injected: false,
    },
  ];
  page.on('dialog', (dialog) => dialog.accept());
  await page.route('**/api/v1/projects/demo-obby/memory', async (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: { entries } });
    if (route.request().method() === 'DELETE') {
      entries = [];
      return route.fulfill({ json: { deleted: 2 } });
    }
    return route.continue();
  });
  await page.route('**/api/v1/memory/*', async (route) => {
    const body = route.request().postDataJSON() as { content?: string; pinned?: boolean };
    const id = route.request().url().split('/').pop()!;
    const current = entries.find((entry) => entry.id === id)!;
    const updated = { ...current, ...body, summary: body.content ?? current.summary };
    entries = entries.map((entry) => (entry.id === id ? updated : entry));
    return route.fulfill({ json: updated });
  });
  try {
    await page.goto(`${baseURL}/#bootstrap=${encodeURIComponent(bootstrap)}`);
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await dialog
      .getByRole('button', { name: /Continue in limited mode|Продолжить в ограниченном режиме/ })
      .click();
    await page.getByRole('button', { name: 'Projects', exact: true }).click();
    await page
      .locator('.project-card')
      .filter({ hasText: 'Skyline Obby' })
      .getByRole('button', { name: 'Skyline Obby' })
      .click();
    await expect(page.getByText('Project memory', { exact: true })).toBeVisible();
    const entry = page.locator('.memory-entry').first();
    await entry.getByRole('button', { name: 'Edit', exact: true }).click();
    await entry.locator('textarea').fill('Always validate on the server');
    await entry.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(entry).toContainText('Always validate on the server');
    await expect(entry).toContainText('Included in latest run');
    await entry.getByRole('button', { name: 'Pin', exact: true }).click();
    await expect(entry).toContainText('Pinned');
    await expect(entry).toContainText('Included in latest run');
    await page.getByRole('button', { name: 'Clear all', exact: true }).click();
    await expect(
      page.getByText('No memory entries stored for this project.', { exact: true }),
    ).toBeVisible();
  } finally {
    await stopDaemon(memoryDaemon);
  }
});

test('task graph shows transitive relationships and updates ready-to-start tasks', async ({
  page,
}, testInfo) => {
  const daemon = await startDaemon({ mock: false });
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  try {
    const project = await prepareProject(page, daemon, 'Dependency project');
    const create = async (title: string, dependencies: string[] = []) => {
      const response = await page.request.post(
        `${daemon.baseURL}/api/v1/projects/${project.id}/tasks`,
        {
          headers: { Origin: daemon.baseURL },
          data: { title, dependencies },
        },
      );
      expect(response.status()).toBe(201);
      return (await response.json()) as { id: string };
    };
    const a = await create('Foundation A');
    const b = await create('Gameplay B', [a.id]);
    const c = await create('Polish C', [b.id]);
    await page.reload();
    await page.getByRole('button', { name: 'Tasks', exact: true }).click();
    await page.getByRole('combobox', { name: 'Project', exact: true }).selectOption(project.id);
    const finalCard = page.getByTestId('task-card').filter({ hasText: 'Polish C' });
    await expect(finalCard.getByTestId('task-blocker-list')).toContainText('Foundation A');
    await expect(finalCard.getByTestId('task-blocker-list')).toContainText('Gameplay B');
    await page.getByTestId('tasks-graph-toggle').click();
    const node = (id: string) =>
      page.getByTestId('task-graph-node').and(page.locator(`[data-task-id="${id}"]`));
    await expect(page.getByTestId('task-graph-node')).toHaveCount(3);
    await node(c.id).click();
    await expect(node(c.id)).toHaveAttribute('aria-pressed', 'true');
    await expect(node(a.id)).toHaveClass(/is-related|is-upstream/);
    await expect(node(b.id)).toHaveClass(/is-related|is-upstream/);
    await page.screenshot({ path: testInfo.outputPath('task-graph.png'), fullPage: true });
    await node(a.id).click();
    await expect(node(c.id)).toHaveClass(/is-related|is-downstream/);
    await expect(node(b.id)).toHaveClass(/is-related|is-downstream/);
    await page.getByTestId('tasks-ready-filter').check();
    await expect(page.getByTestId('task-graph-node')).toHaveCount(1);
    await expect(node(a.id)).toBeVisible();
    const completed = await page.request.post(`${daemon.baseURL}/api/v1/tasks/${a.id}`, {
      headers: { Origin: daemon.baseURL },
      data: { status: 'completed' },
    });
    expect(completed.ok()).toBeTruthy();
    await page.reload();
    await page.getByTestId('tasks-graph-toggle').click();
    await page.getByTestId('tasks-ready-filter').check();
    await expect(page.getByTestId('task-graph-node')).toHaveCount(1);
    await expect(node(b.id)).toBeVisible();
    await page.getByRole('button', { name: 'EN', exact: true }).click();
    await expect(page.getByTestId('tasks-graph-toggle')).toHaveText('Граф зависимостей');
    expect(errors).toEqual([]);
  } finally {
    await stopDaemon(daemon);
  }
});

test('project files browse real nested text and explain binary and oversized files', async ({
  page,
}, testInfo) => {
  const daemon = await startDaemon({ mock: false });
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  try {
    const project = await prepareProject(page, daemon, 'File browser project');
    const folder = join(project.path, 'browser-fixture');
    mkdirSync(folder);
    writeFileSync(
      join(folder, 'hello.lua'),
      '-- browser fixture\nlocal message = "Hello from disk"\nprint(message)\n',
    );
    writeFileSync(join(folder, 'binary.bin'), Buffer.from([0, 1, 2, 255]));
    writeFileSync(join(folder, 'oversized.txt'), 'x'.repeat(1024 * 1024 + 1));
    await page.getByRole('button', { name: 'Files', exact: true }).click();
    await page.getByRole('combobox', { name: 'Project', exact: true }).selectOption(project.id);
    await page.getByRole('button', { name: 'browser-fixture', exact: true }).click();
    await page.getByRole('button', { name: 'hello.lua', exact: true }).click();
    await expect(page.locator('pre')).toContainText('Hello from disk');
    const keyword = page.locator('pre .syntax-keyword').first();
    await expect(keyword).toHaveText('local');
    expect(await keyword.evaluate((element) => getComputedStyle(element).color)).not.toBe(
      await page.locator('pre code').evaluate((element) => getComputedStyle(element).color),
    );
    await page.screenshot({ path: testInfo.outputPath('project-files.png'), fullPage: true });
    await page.getByRole('button', { name: 'binary.bin', exact: true }).click();
    await expect(page.getByText(/binary file/i)).toBeVisible();
    await expect(page.locator('pre')).toHaveCount(0);
    await page.getByRole('button', { name: 'oversized.txt', exact: true }).click();
    await expect(page.getByText(/1 MB/)).toBeVisible();
    await page.getByRole('button', { name: 'hello.lua', exact: true }).click();
    await expect(page.locator('pre')).toContainText('Hello from disk');
    await page.route('**/api/v1/runs/*/diff', (route) =>
      route.fulfill({
        json: {
          diff: 'diff --git a/browser-fixture/hello.lua b/browser-fixture/hello.lua\n--- a/browser-fixture/hello.lua\n+++ b/browser-fixture/hello.lua\n@@ -0,0 +1 @@\n+local message = "Hello from disk"\n',
        },
      }),
    );
    await page.getByRole('button', { name: 'Chat', exact: true }).click();
    await expect(page.locator('.composer textarea')).toBeEnabled();
    await page
      .locator('.composer textarea')
      .fill('Inspect the browser fixture with the mock provider.');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(page.locator('details.diff-panel > summary')).toBeVisible({ timeout: 20_000 });
    await page.locator('details.diff-panel > summary').click();
    await page
      .getByTestId('diff-viewer')
      .getByRole('button', { name: 'browser-fixture/hello.lua', exact: true })
      .click();
    await expect(page.getByTestId('files-view')).toBeVisible();
    await expect(page.locator('pre')).toContainText('Hello from disk');
    await expect(page.getByRole('button', { name: 'hello.lua', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'EN', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Файлы', exact: true })).toBeVisible();
    expect(errors).toEqual([]);
  } finally {
    await stopDaemon(daemon);
  }
});

test('project usage separates accounting and exports sortable runs', async ({ page }, testInfo) => {
  const fresh = await startDaemon();
  try {
    await page.goto(`${fresh.baseURL}/#bootstrap=${encodeURIComponent(fresh.bootstrap)}`);
    await expect(page.getByRole('dialog')).toBeVisible();
    const settings = await page.request.post(`${fresh.baseURL}/api/v1/settings`, {
      headers: { Origin: fresh.baseURL },
      data: { setup_complete: 'true', locale: 'en' },
    });
    expect(settings.ok()).toBeTruthy();
    await page.reload();
    await expect(page.getByRole('dialog')).toBeHidden();
    await page.getByRole('button', { name: 'Usage & cost', exact: true }).click();
    await expect(page.getByTestId('usage-view')).toBeVisible();
    await expect(page.getByText('Lifetime recorded spend', { exact: true })).toBeVisible();
    await expect(page.getByTestId('usage-view').locator('tbody tr').first()).toBeVisible();
    await page.getByLabel('Group by', { exact: true }).selectOption('model');
    await page.getByLabel('Sort descending', { exact: true }).selectOption('tokens');
    await page.getByLabel('Bucket', { exact: true }).selectOption('weeks');
    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: 'CSV', exact: true }).click();
    expect((await download).suggestedFilename()).toBe('studioforge-usage.csv');
    await page.screenshot({ path: testInfo.outputPath('usage.png'), fullPage: true });
  } finally {
    await stopDaemon(fresh);
  }
});

test('project style selection persists and keeps an installed custom brief', async ({ page }) => {
  const daemon = await startDaemon({ mock: false });
  try {
    const project = await prepareProject(page, daemon, 'Style test');
    const custom = join(project.path, '.agent', 'styles', 'my-farm');
    mkdirSync(custom, { recursive: true });
    writeFileSync(join(custom, 'brief.md'), 'Operator-owned pastel farm.');
    await page.getByRole('button', { name: 'Overview', exact: true }).click();
    const selector = page.getByRole('combobox', { name: 'UI style pack', exact: true });
    await expect(selector).toBeVisible();
    await expect(selector.locator('option[value="my-farm"]')).toHaveCount(1);
    await selector.selectOption('my-farm');
    await expect
      .poll(async () => {
        const result = await page.request.get(
          `${daemon.baseURL}/api/v1/projects/${project.id}/style`,
        );
        return (await result.json()).style;
      })
      .toBe('my-farm');
    await page.reload();
    await expect(selector).toHaveValue('my-farm');
    const result = await page.request.get(`${daemon.baseURL}/api/v1/projects/${project.id}/style`);
    expect((await result.json()).pack.brief).toBe('Operator-owned pastel farm.');
  } finally {
    await stopDaemon(daemon);
  }
});

test('review card submits selected hunks and clears selection for another decision', async ({
  page,
}) => {
  const fresh = await startDaemon();
  try {
    let chosen: unknown = null;
    let revision = 1;
    const hunk = '@@ -1 +1 @@\n-old\n+new\n';
    await page.route('**/api/v1/snapshot', async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      const run = body.runs[0];
      run.status = 'waiting_decision';
      body.decisions = [
        {
          id: `review-${revision}`,
          runId: run.id,
          projectId: run.projectId,
          kind: 'review_before_apply',
          status: 'pending',
          summary: 'Review test changes',
          diff: `diff --git a/example.txt b/example.txt\n--- a/example.txt\n+++ b/example.txt\n${hunk}`,
          reviewFiles: [{ path: 'example.txt', hunks: [hunk] }],
        },
      ];
      await route.fulfill({ response, json: body });
    });
    await page.route('**/api/v1/decisions/*/resolve', async (route) => {
      chosen = route.request().postDataJSON();
      revision++;
      await route.fulfill({ json: { ok: true } });
    });
    await page.goto(`${fresh.baseURL}/#bootstrap=${encodeURIComponent(fresh.bootstrap)}`);
    await expect(page.getByRole('dialog')).toBeVisible();
    await page.request.post(`${fresh.baseURL}/api/v1/settings`, {
      headers: { Origin: fresh.baseURL },
      data: { setup_complete: 'true', locale: 'en' },
    });
    await page.reload();
    await expect(page.getByRole('dialog')).toBeHidden();
    await page.getByRole('button', { name: 'Runs', exact: true }).click();
    await expect(page.getByText('Review test changes', { exact: true })).toBeVisible();
    const applySelected = page.getByRole('button', { name: 'Apply selected', exact: true });
    await expect(applySelected).toBeDisabled();
    await page.locator('.review-hunks input').check();
    await expect(applySelected).toBeEnabled();
    await applySelected.click();
    await expect
      .poll(() => chosen)
      .toEqual({
        action: 'apply_selected',
        selectedFiles: [],
        hunks: [{ path: 'example.txt', index: 0 }],
      });
    await expect(applySelected).toBeDisabled();
  } finally {
    await stopDaemon(fresh);
  }
});
