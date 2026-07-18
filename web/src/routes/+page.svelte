<script lang="ts">
  import { onMount } from 'svelte';
  import '../app.css';
  import {
    Activity,
    FolderKanban,
    Gauge,
    Languages,
    Library,
    ListChecks,
    MessagesSquare,
    RefreshCw,
    Settings,
    ShieldAlert,
    TerminalSquare,
    Users,
    Waypoints,
    X,
  } from '@lucide/svelte';
  import { brand } from '$lib/brand';
  import {
    bootstrapFromHash,
    connectEvents,
    createTask,
    deleteTask,
    getSnapshot,
    post,
    updateTask,
  } from '$lib/api';
  import { loadProject, loadView, saveProject, saveView } from '$lib/session';
  import {
    detectLocale,
    formatDate,
    formatMoney,
    locale,
    translate,
    type Locale,
    type TranslationKey,
  } from '$lib/i18n';
  import type { Agent, AppSettings, Check, Project, Run, RunEvent, Snapshot } from '$lib/types';
  import FirstRunWizard from '$lib/components/FirstRunWizard.svelte';
  import NewProjectDialog from '$lib/components/NewProjectDialog.svelte';
  import ActivityView from '$lib/components/views/ActivityView.svelte';
  import AssetsView from '$lib/components/views/AssetsView.svelte';
  import ChatView from '$lib/components/views/ChatView.svelte';
  import DecisionsView from '$lib/components/views/DecisionsView.svelte';
  import OverviewView from '$lib/components/views/OverviewView.svelte';
  import ProjectsView from '$lib/components/views/ProjectsView.svelte';
  import RunsView from '$lib/components/views/RunsView.svelte';
  import SettingsView from '$lib/components/views/SettingsView.svelte';
  import StudiosView from '$lib/components/views/StudiosView.svelte';
  import TasksView from '$lib/components/views/TasksView.svelte';
  import TeamView from '$lib/components/views/TeamView.svelte';

  type View =
    | 'chat'
    | 'projects'
    | 'activity'
    | 'overview'
    | 'team'
    | 'tasks'
    | 'runs'
    | 'studios'
    | 'decisions'
    | 'assets'
    | 'settings';
  let view: View = 'projects';
  let snapshot: Snapshot | null = null;
  let error = '';
  let loading = true;
  let busy = '';
  let search = '';
  let showNewProject = false;
  let selectedProjectId = '';
  let selectedRunId = '';
  let events: RunEvent[] = [];
  let streamOnline = false;
  let theme = 'dark';
  let notice = '';
  let newProject = { name: '', path: '', description: '', create: true, openStudio: false };
  let disconnect = () => {};
  let restored = false;

  const nav: { id: View; icon: typeof Activity; key: TranslationKey }[] = [
    { id: 'chat', icon: MessagesSquare, key: 'nav.chat' },
    { id: 'projects', icon: FolderKanban, key: 'nav.projects' },
    { id: 'activity', icon: Activity, key: 'nav.activity' },
    { id: 'overview', icon: Gauge, key: 'nav.overview' },
    { id: 'team', icon: Users, key: 'nav.team' },
    { id: 'tasks', icon: ListChecks, key: 'nav.tasks' },
    { id: 'runs', icon: TerminalSquare, key: 'nav.runs' },
    { id: 'studios', icon: Waypoints, key: 'nav.studios' },
    { id: 'decisions', icon: ShieldAlert, key: 'nav.decisions' },
    { id: 'assets', icon: Library, key: 'nav.assets' },
    { id: 'settings', icon: Settings, key: 'nav.settings' },
  ];

  $: if (restored) saveView(view);
  $: if (restored) saveProject(selectedProjectId);

  $: projects = snapshot?.projects ?? [];
  $: selectedProject = projects.find((project) => project.id === selectedProjectId) ?? projects[0];
  $: selectedRun = snapshot?.runs.find((run) => run.id === selectedRunId);
  $: selectedEvents = events.filter((event) => event.runId === selectedRunId).slice(-250);

  onMount(() => {
    const storedTheme = localStorage.getItem('studioforge-theme') ?? 'dark';
    setTheme(storedTheme);
    const storedView = loadView(nav.map((item) => item.id));
    if (storedView) view = storedView as View;
    // Only now may the reactive writers below run. They are gated on this flag
    // because Svelte runs reactive statements once at init — before onMount —
    // which would persist the default view over the stored one and make the
    // restore a no-op on every load.
    restored = true;
    void initialize();
    const keyHandler = (event: KeyboardEvent) => {
      if (event.altKey && event.key >= '1' && event.key <= '9') {
        const target = nav[Number(event.key) - 1];
        if (target) {
          event.preventDefault();
          view = target.id;
        }
      }
    };
    window.addEventListener('keydown', keyHandler);
    return () => {
      disconnect();
      window.removeEventListener('keydown', keyHandler);
    };
  });

  async function initialize() {
    loading = true;
    error = '';
    try {
      await bootstrapFromHash();
      await refresh();
      disconnect = connectEvents(
        (event) => {
          events = [...events.slice(-999), event];
          if (event.type === 'status') window.setTimeout(() => void refresh(false), 120);
        },
        (online) => (streamOnline = online),
      );
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  async function refresh(showSpinner = false) {
    if (showSpinner) busy = 'refresh';
    try {
      const nextSnapshot = await getSnapshot();
      snapshot = nextSnapshot;
      if (!selectedProjectId)
        // The remembered project wins over "first in the list", but only if it
        // still exists — projects can be removed from another window, or the
        // data directory swapped entirely, between sessions.
        selectedProjectId =
          loadProject(nextSnapshot.projects) || nextSnapshot.projects[0]?.id || '';
      const configured = nextSnapshot.settings.locale;
      locale.set(configured === 'ru' || configured === 'en' ? configured : detectLocale());
      if (!selectedRunId && nextSnapshot.runs[0]) selectedRunId = nextSnapshot.runs[0].id;
    } finally {
      if (showSpinner) busy = '';
    }
  }

  async function finishWizard() {
    await action('wizard', async () => {
      await post('/settings', { setup_complete: 'true', locale: $locale });
      await refresh();
    });
  }
  async function changeLocale(value: Locale) {
    locale.set(value);
    await action('locale', () => post('/settings', { locale: value }));
  }
  function setTheme(value: string) {
    theme = value;
    localStorage.setItem('studioforge-theme', value);
    document.documentElement.dataset.theme = value;
  }
  async function createProject() {
    await action('project', async () => {
      const project = await post<Project>('/projects', newProject);
      showNewProject = false;
      newProject = { name: '', path: '', description: '', create: true, openStudio: false };
      await refresh();
      // Onboarding: land in the new project's chat, ready for the first prompt.
      if (project?.id) {
        selectedProjectId = project.id;
        view = 'chat';
      }
    });
  }
  async function addTask(task: { title: string; status: string }) {
    if (!selectedProject) return;
    await action('task-create', async () => {
      const created = await createTask(selectedProject.id, { title: task.title });
      if (task.status && task.status !== created.status) {
        await updateTask(created.id, { status: task.status });
      }
      await refresh();
    });
  }
  async function moveTask(taskId: string, status: string) {
    await action(`task-${taskId}`, async () => {
      await updateTask(taskId, { status });
      await refresh();
    });
  }
  async function removeTask(taskId: string) {
    await action(`task-del-${taskId}`, async () => {
      await deleteTask(taskId);
      await refresh();
    });
  }
  async function openStudio(project: Project) {
    await action(`open-studio-${project.id}`, async () => {
      const result = await post<{ place: string }>(`/projects/${project.id}/open-studio`, {});
      notice = `${$translate('projects.openStudio')}: ${result.place}`;
    });
  }
  async function archiveProject(project: Project) {
    await action(`archive-${project.id}`, async () => {
      await post(`/projects/${project.id}/archive`, { archived: !project.archived });
      await refresh();
    });
  }
  async function startRun(project = selectedProject, agentId = '', prompt = '') {
    if (!project || !prompt.trim()) return;
    await action(`run-${project.id}`, async () => {
      const run = await post<Run>(
        '/runs',
        { projectId: project.id, agentId, prompt },
        { 'Idempotency-Key': crypto.randomUUID() },
      );
      selectedRunId = run.id;
      view = 'runs';
      await refresh();
    });
  }
  async function createAgent(agent: Partial<Agent>) {
    if (!selectedProject) return;
    await action('agent-create', async () => {
      await post<Agent>(`/projects/${selectedProject.id}/agents`, agent);
      notice = $translate('team.create');
      await refresh();
    });
  }
  async function updateAgent(agent: Agent) {
    await action(`agent-${agent.id}`, async () => {
      await post<Agent>(`/projects/${agent.projectId}/agents/${agent.id}`, agent);
      notice = $translate('settings.saved');
      await refresh();
    });
  }
  async function saveSettings(settings: AppSettings) {
    await action('settings', async () => {
      await post('/settings', {
        default_provider: settings.default_provider,
        default_model: settings.default_model,
        default_effort: settings.default_effort,
        codex_path: settings.codex_path,
        claude_path: settings.claude_path,
        rojo_path: settings.rojo_path,
        git_path: settings.git_path,
        studio_mcp_path: settings.studio_mcp_path,
        studio_auto_open: settings.studio_auto_open === 'false' ? 'false' : 'true',
        concurrency: String(settings.concurrency),
      });
      notice = $translate('settings.saved');
      await refresh();
    });
  }
  async function runAction(run: Run, command: string) {
    await action(`${command}-${run.id}`, async () => {
      await post(`/runs/${run.id}/${command}`, {});
      await refresh();
    });
  }
  async function decide(id: string, status: string) {
    await action(`decision-${id}`, async () => {
      await post(`/decisions/${id}`, { status, resolution: status });
      await refresh();
    });
  }
  async function bindStudio(sessionId: string, projectId: string) {
    await action(`studio-${sessionId}`, async () => {
      await post(`/studios/${sessionId}/bind`, { projectId });
      await refresh();
    });
  }
  async function createBackup() {
    await action('backup', async () => {
      const result = await post<{ path: string }>('/backups', {});
      notice = `${$translate('settings.backupCreated')}: ${result.path}`;
    });
  }
  async function action(id: string, work: () => Promise<unknown>) {
    busy = id;
    error = '';
    notice = '';
    try {
      await work();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      busy = '';
    }
  }
  function projectName(id: string): string {
    return projects.find((project) => project.id === id)?.name ?? id;
  }
  function agentName(id: string): string {
    return snapshot?.agents.find((agent) => agent.id === id)?.name ?? id;
  }
  function statusLabel(status: string): string {
    const key = `status.${status}` as TranslationKey;
    return key in ({} as Record<TranslationKey, string>)
      ? $translate(key)
      : $translate(key) || status;
  }
  function checkList(): Check[] {
    return [
      ...Object.values(snapshot?.diagnostics.dependencies ?? {}),
      ...(snapshot?.diagnostics.checks ?? []),
    ];
  }
  function payloadText(payload: unknown): string {
    if (typeof payload === 'string') return payload;
    if (payload && typeof payload === 'object') {
      const value = payload as Record<string, unknown>;
      if (typeof value.text === 'string') return value.text;
      if (typeof value.message === 'string') return value.message;
    }
    return JSON.stringify(payload);
  }
</script>

<svelte:head
  ><title>{brand.name}</title><meta
    name="description"
    content={$translate('app.tagline')}
  /></svelte:head
>

{#if loading}
  <main class="center-state">
    <div class="forge-mark" aria-hidden="true">SF</div>
    <p>{$translate('common.loading')}</p>
  </main>
{:else if error && !snapshot}
  <main class="center-state error-state">
    <ShieldAlert size={34} />
    <h1>{$translate('error.title')}</h1>
    <p>{error}</p>
    <button class="primary" onclick={initialize}>{$translate('common.retry')}</button>
  </main>
{:else if snapshot}
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-icon">SF</div>
        <div><strong>{brand.name}</strong><small>{$translate('app.tagline')}</small></div>
      </div>
      <nav aria-label={brand.name}>
        {#each nav as item, index}
          <button
            class:active={view === item.id}
            onclick={() => (view = item.id)}
            title={`Alt+${index + 1}`}
            aria-current={view === item.id ? 'page' : undefined}
          >
            <item.icon size={18} /><span>{$translate(item.key)}</span>
            {#if item.id === 'decisions' && snapshot.decisions.filter((d) => d.status === 'pending').length > 0}<b
                class="count">{snapshot.decisions.filter((d) => d.status === 'pending').length}</b
              >{/if}
          </button>
        {/each}
      </nav>
      <div class="sidebar-footer">
        <span class:online={streamOnline} class="presence"></span><span
          >{streamOnline ? $translate('common.active') : $translate('status.interrupted')}</span
        ><code>v{snapshot.diagnostics.version}</code>
      </div>
    </aside>

    <div class="workspace">
      <header class="topbar">
        <label class="project-switch"
          ><span>{$translate('common.project')}</span><select
            bind:value={selectedProjectId}
            aria-label={$translate('common.project')}
            >{#each projects as project}<option value={project.id}>{project.name}</option
              >{/each}</select
          ></label
        >
        <div class="top-actions">
          <button
            class="icon-button"
            aria-label={$translate('wizard.recheck')}
            onclick={() => refresh(true)}
            disabled={busy === 'refresh'}
            ><RefreshCw size={17} class={busy === 'refresh' ? 'spin' : ''} /></button
          ><button
            class="locale-button"
            onclick={() => changeLocale($locale === 'en' ? 'ru' : 'en')}
            ><Languages size={16} />{$locale.toUpperCase()}</button
          >
        </div>
      </header>

      {#if error}<div class="toast error-toast" role="alert">
          <span>{error}</span><button
            aria-label={$translate('common.close')}
            onclick={() => (error = '')}><X size={17} /></button
          >
        </div>{/if}
      {#if notice}<div class="toast success-toast" role="status">
          <span>{notice}</span><button
            aria-label={$translate('common.close')}
            onclick={() => (notice = '')}><X size={17} /></button
          >
        </div>{/if}

      <main class="content" class:chat={view === 'chat'}>
        {#if view === 'chat'}
          <ChatView
            projectId={selectedProject?.id}
            liveEvents={events.filter(
              (event) => selectedProject && event.projectId === selectedProject.id,
            )}
            agents={snapshot.agents.filter(
              (a) => selectedProject && a.projectId === selectedProject.id && a.enabled,
            )}
            tasks={snapshot.tasks.filter(
              (t) => selectedProject && t.projectId === selectedProject.id,
            )}
            {agentName}
            {statusLabel}
            onSent={(id) => {
              selectedRunId = id;
            }}
          />
        {:else if view === 'projects'}
          <ProjectsView
            {projects}
            {busy}
            safeMode={snapshot.settings.safeMode}
            bind:search
            onNew={() => (showNewProject = true)}
            onSelect={(project) => {
              selectedProjectId = project.id;
              view = 'overview';
            }}
            onArchive={archiveProject}
            onRun={startRun}
            onOpenStudio={openStudio}
          />
        {:else if view === 'activity'}
          <ActivityView
            runs={snapshot.runs}
            {projectName}
            {agentName}
            {statusLabel}
            onRunAction={runAction}
          />
        {:else if view === 'overview'}
          <OverviewView {snapshot} project={selectedProject} {busy} onRun={() => startRun()} />
        {:else if view === 'team'}
          <TeamView
            agents={snapshot.agents}
            project={selectedProject}
            {busy}
            onCreate={createAgent}
            onUpdate={updateAgent}
            onRun={(agent) => startRun(selectedProject, agent.id)}
          />
        {:else if view === 'tasks'}
          <TasksView
            tasks={snapshot.tasks}
            project={selectedProject}
            onCreateTask={addTask}
            onUpdateStatus={moveTask}
            onDeleteTask={removeTask}
          />
        {:else if view === 'runs'}
          <RunsView
            runs={snapshot.runs}
            bind:selectedRunId
            {selectedRun}
            events={selectedEvents}
            {projectName}
            {agentName}
            {statusLabel}
            {payloadText}
            busy={busy.startsWith('run-')}
            onSend={(prompt) => startRun(selectedProject, '', prompt)}
          />
        {:else if view === 'studios'}
          <StudiosView studios={snapshot.studios} {projects} {projectName} onBind={bindStudio} />
        {:else if view === 'decisions'}
          <DecisionsView decisions={snapshot.decisions} {projectName} onDecide={decide} />
        {:else if view === 'assets'}
          <AssetsView />
        {:else if view === 'settings'}
          <SettingsView
            diagnostics={snapshot.diagnostics}
            settings={snapshot.settings}
            {theme}
            {busy}
            onLocale={changeLocale}
            onTheme={setTheme}
            onRefresh={() => refresh(true)}
            onBackup={createBackup}
            onSave={saveSettings}
          />
        {/if}
      </main>
    </div>
  </div>

  {#if !snapshot.settings.setupComplete}
    <FirstRunWizard
      checks={checkList()}
      safeMode={snapshot.settings.safeMode}
      {busy}
      onRefresh={() => refresh(true)}
      onComplete={finishWizard}
    />
  {/if}
  {#if showNewProject}
    <NewProjectDialog
      bind:value={newProject}
      {busy}
      onClose={() => (showNewProject = false)}
      onSubmit={() => void createProject()}
    />
  {/if}
{/if}
