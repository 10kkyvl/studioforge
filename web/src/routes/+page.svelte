<script lang="ts">
  import { onMount } from 'svelte';
  import '../app.css';
  import {
    Activity,
    FolderKanban,
    Gauge,
    Languages,
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
    APIError,
    bootstrapFromHash,
    connectEvents,
    createTask,
    deleteTask,
    friendlyError,
    getSnapshot,
    post,
    updateTask,
  } from '$lib/api';
  import { loadProject, loadView, saveProject, saveView } from '$lib/session';
  import {
    detectLocale,
    formatDate,
    locale,
    translate,
    type Locale,
    type TranslationKey,
  } from '$lib/i18n';
  import { normalizeFontSize, setThemeColorMeta, themeColorFor } from '$lib/theme';
  import { setPendingLeadAgent } from '$lib/uiIntents';
  import type { Agent, AppSettings, Check, Project, Run, RunEvent, Snapshot } from '$lib/types';
  import FirstRunWizard from '$lib/components/FirstRunWizard.svelte';
  import NewProjectDialog from '$lib/components/NewProjectDialog.svelte';
  import ActivityView from '$lib/components/views/ActivityView.svelte';
  import ChatView from '$lib/components/views/ChatView.svelte';
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
    | 'settings';
  let view: View = 'projects';
  let snapshot: Snapshot | null = null;
  let error = '';
  let errorRetry: (() => void) | null = null;
  let loading = true;
  let busy = '';
  let search = '';
  let showNewProject = false;
  let selectedProjectId = '';
  let selectedRunId = '';
  let events: RunEvent[] = [];
  let streamOnline = false;
  let theme = 'system';
  let fontSize = 'comfortable';
  let notice = '';
  // True until a refresh reports otherwise, so a daemon that has never run a
  // real discovery pass (e.g. --mock, or before the operator's first click)
  // does not read as "Studio MCP not detected" by default.
  let studioSessionsDetected = true;
  let newProject = { name: '', path: '', description: '', create: true, openStudio: false };
  let disconnect = () => {};
  let restored = false;
  // The id of the last event this tab actually received, used to replay any
  // gap (via the SSE `after` param) whenever the stream is reopened, e.g.
  // after being closed while the tab was hidden.
  let lastEventId: number | undefined;
  // True once this tab holds a live subscription registered via connectEvents.
  // connectEvents/openSharedStream (api.ts) already reuse one shared
  // EventSource across calls instead of tearing it down, specifically so that
  // connectStream() below can be invoked repeatedly (on mount, on every
  // visibilitychange-to-visible, and defensively from ChatView on every chat
  // send) without thrashing the connection — but only if connectStream()
  // actually leans on that idempotency instead of working around it. This
  // flag is what lets it skip re-subscribing when the tab is already
  // connected.
  let streaming = false;
  let statusRefreshTimer: ReturnType<typeof setTimeout> | undefined;
  // Coalesces concurrent refresh() callers (the header button, the debounced
  // status-event refresh, and every action() handler all call it) onto one
  // in-flight /snapshot request, so a second click can't queue a second
  // fetch behind a connection pool that's already maxed out by the SSE
  // stream (Chrome allows only 6 concurrent connections per origin).
  let refreshPromise: Promise<void> | null = null;
  // At most one refresh chained behind the in-flight one, shared by every
  // caller that arrives while it runs — see refresh() for why they can't just
  // reuse refreshPromise itself.
  let queuedRefresh: Promise<void> | null = null;

  const nav: { id: View; icon: typeof Activity; key: TranslationKey }[] = [
    { id: 'chat', icon: MessagesSquare, key: 'nav.chat' },
    { id: 'projects', icon: FolderKanban, key: 'nav.projects' },
    { id: 'activity', icon: Activity, key: 'nav.activity' },
    { id: 'overview', icon: Gauge, key: 'nav.overview' },
    { id: 'team', icon: Users, key: 'nav.team' },
    { id: 'tasks', icon: ListChecks, key: 'nav.tasks' },
    { id: 'runs', icon: TerminalSquare, key: 'nav.runs' },
    { id: 'studios', icon: Waypoints, key: 'nav.studios' },
    { id: 'settings', icon: Settings, key: 'nav.settings' },
  ];
  type NavEntry = { id: View; icon: typeof Activity; key: TranslationKey; index: number };
  const navById = new Map<View, NavEntry>(nav.map((item, index) => [item.id, { ...item, index }]));
  const navGroups: { label: TranslationKey | null; items: NavEntry[] }[] = [
    {
      label: 'nav.groupWork',
      items: [navById.get('chat')!, navById.get('tasks')!],
    },
    {
      label: 'nav.groupProject',
      items: [navById.get('projects')!, navById.get('overview')!, navById.get('team')!],
    },
    {
      label: 'nav.groupMonitor',
      items: [navById.get('activity')!, navById.get('runs')!, navById.get('studios')!],
    },
    { label: null, items: [navById.get('settings')!] },
  ];
  const globalViews: readonly View[] = ['activity', 'runs', 'studios', 'settings'];

  $: if (restored) saveView(view);
  $: if (restored) saveProject(selectedProjectId);
  $: document.documentElement.lang = $locale;

  $: projects = snapshot?.projects ?? [];
  $: activeProjects = projects.filter((project) => !project.archived);
  $: if (
    activeProjects.length &&
    !activeProjects.some((project) => project.id === selectedProjectId)
  ) {
    selectedProjectId = activeProjects[0].id;
  }
  $: selectedProject =
    activeProjects.find((project) => project.id === selectedProjectId) ?? activeProjects[0];
  $: selectedRun = snapshot?.runs.find((run) => run.id === selectedRunId);
  $: selectedEvents = events.filter((event) => event.runId === selectedRunId).slice(-250);
  $: showProjectSwitch = !globalViews.includes(view);

  onMount(() => {
    const storedTheme = localStorage.getItem('studioforge-theme') ?? 'system';
    setTheme(storedTheme, false);
    const storedFontSize = localStorage.getItem('studioforge-font-size') ?? 'comfortable';
    setFontSize(storedFontSize);
    const storedView = loadView(nav.map((item) => item.id));
    if (storedView) view = storedView as View;
    // Only now may the reactive writers below run. They are gated on this flag
    // because Svelte runs reactive statements once at init — before onMount —
    // which would persist the default view over the stored one and make the
    // restore a no-op on every load.
    restored = true;
    void initialize();
    const keyHandler = (event: KeyboardEvent) => {
      if (!event.altKey || event.key < '1' || event.key > '9') return;
      // AltGr (common on non-US/RU keyboards) reports as ctrlKey and altKey
      // both true on Windows; require a plain Alt combo so it isn't mistaken
      // for one of these shortcuts.
      if (event.ctrlKey || event.metaKey) return;
      if (event.getModifierState && event.getModifierState('AltGraph')) return;
      // Don't hijack the shortcut while the user is typing into an editable
      // element (input, textarea, select, or contenteditable).
      const active = document.activeElement;
      if (
        active &&
        (active.tagName === 'INPUT' ||
          active.tagName === 'TEXTAREA' ||
          active.tagName === 'SELECT' ||
          (active as HTMLElement).isContentEditable)
      )
        return;
      const target = nav[Number(event.key) - 1];
      if (target) {
        event.preventDefault();
        view = target.id;
      }
    };
    window.addEventListener('keydown', keyHandler);
    // Chrome's 6-connections-per-origin cap (HTTP/1.1, shared across every
    // tab in the profile) is shared with the standing SSE stream each tab
    // holds open, so a background tab's stream is pure waste — it can't
    // update anything the user can see, yet it still occupies a slot needed
    // by whichever tab IS visible. Closing it while hidden and reopening on
    // return limits live streams to visible tabs instead of all open ones.
    // This is purely a resource-conservation optimization, though:
    // connectStream() no longer gates its first attempt on document.hidden,
    // so a tab that is hidden for its entire life still gets — and keeps —
    // a live connection; it just isn't closed-and-reopened by this handler
    // along the way.
    const visibilityHandler = () => {
      if (document.hidden) {
        disconnect();
        disconnect = () => {};
        streaming = false;
        // EventSource.close() fires no error event, so connectEvents' status
        // callback never runs on a deliberate close and the presence dot would
        // keep claiming a live stream that is gone. Say so explicitly.
        streamOnline = false;
      } else {
        connectStream();
      }
    };
    document.addEventListener('visibilitychange', visibilityHandler);
    return () => {
      disconnect();
      if (statusRefreshTimer) clearTimeout(statusRefreshTimer);
      document.removeEventListener('visibilitychange', visibilityHandler);
      window.removeEventListener('keydown', keyHandler);
    };
  });

  // (Re)opens the event stream, replaying from lastEventId so a reconnect
  // never silently drops events. Called unconditionally from initialize() at
  // mount, from every visibilitychange-to-visible, and defensively from
  // ChatView's submitRun() on every send — deliberately NEVER gated on
  // document.hidden. A tab opened in the background (or restored by the
  // browser) can sit at document.hidden === true for its whole session with
  // no visibilitychange ever firing, because there is no hidden -> visible
  // transition to fire it. This function used to bail out early whenever
  // document.hidden was true, which left exactly that tab permanently
  // without a live stream: no error, no console warning, just a chat whose
  // "Working…" never resolved. The visibilitychange handler below still
  // closes the connection reactively once a tab genuinely becomes hidden (so
  // an abandoned background tab doesn't hold one of Chrome's 6
  // connections-per-origin forever) and calls back in here on return — but
  // establishing a connection in the first place must never depend on that
  // event ever firing.
  //
  // Because it is now called from several places, it must first check
  // whether this tab is already connected: connectEvents/openSharedStream
  // (api.ts) reuse a live EventSource instead of recreating it, but that
  // idempotency only helps if THIS function stops disconnecting before every
  // call. This tab is normally the sole subscriber, so an unconditional
  // disconnect() here drops eventSubscribers to zero, which closes the
  // shared EventSource — i.e. it would still tear down and reopen the
  // connection on every redundant call, exactly what the shared-stream
  // design in api.ts was meant to avoid.
  function connectStream() {
    if (streaming) return;
    streamOnline = false;
    streaming = true;
    disconnect = connectEvents(
      (event) => {
        if (event.id > 0) lastEventId = event.id;
        events = [...events.slice(-999), event];
        if (event.type === 'status') scheduleStatusRefresh();
      },
      (online) => (streamOnline = online),
      lastEventId,
    );
  }

  // Debounced: an agent emits a burst of status events in quick succession
  // while working, and firing one /snapshot request per event is exactly the
  // kind of queue that exhausts the connection pool the SSE stream already
  // eats into. Only the last event in a burst actually triggers a refresh,
  // and the timer restarts on every event rather than stacking timeouts.
  function scheduleStatusRefresh() {
    if (statusRefreshTimer) clearTimeout(statusRefreshTimer);
    statusRefreshTimer = setTimeout(() => {
      statusRefreshTimer = undefined;
      void refresh(false);
    }, 120);
  }

  async function initialize() {
    loading = true;
    error = '';
    locale.set(detectLocale());
    try {
      await bootstrapFromHash();
      await refresh();
      // refresh() swallows getSnapshot() failures (see startRefresh) so that a
      // background poll never throws an unhandled rejection; that means the
      // await above always resolves, even when the initial load actually
      // failed. Only open the stream once a snapshot has actually landed —
      // otherwise we'd hold open a live connection behind the "unavailable"
      // error screen until the user manually retries.
      if (snapshot) connectStream();
    } catch (cause) {
      error = friendlyError(cause, $translate);
    } finally {
      loading = false;
    }
  }

  async function refresh(showSpinner = false) {
    // A caller arriving mid-flight must not simply reuse refreshPromise: that
    // request may have been issued before the caller's own mutation landed, so
    // its snapshot would be missing the very thing the caller just created.
    // They instead share ONE follow-up request chained after the current one —
    // enough for every caller to observe its own write, while still never
    // stacking a request per caller onto an already-saturated connection pool.
    if (refreshPromise) {
      queuedRefresh ??= refreshPromise.then(() => {
        queuedRefresh = null;
        return startRefresh(showSpinner);
      });
      return queuedRefresh;
    }
    return startRefresh(showSpinner);
  }

  function startRefresh(showSpinner: boolean): Promise<void> {
    if (showSpinner) busy = 'refresh';
    refreshPromise = (async () => {
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
      } catch (cause) {
        // refresh() is also called directly from click handlers (the header
        // button, FirstRunWizard, SettingsView) with no surrounding
        // try/catch, unlike action() below — without this it was an
        // unhandled rejection and the failure was invisible to the user.
        error = cause instanceof Error ? cause.message : String(cause);
      } finally {
        if (showSpinner) busy = '';
        refreshPromise = null;
      }
    })();
    return refreshPromise;
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
  function setTheme(value: string, animate = true) {
    theme = value;
    localStorage.setItem('studioforge-theme', value);
    if (animate) {
      document.documentElement.classList.add('theme-transition');
      window.setTimeout(() => document.documentElement.classList.remove('theme-transition'), 260);
    }
    document.documentElement.dataset.theme = value;
    setThemeColorMeta(themeColorFor(value));
  }
  function setFontSize(value: string) {
    fontSize = normalizeFontSize(value);
    localStorage.setItem('studioforge-font-size', fontSize);
    document.documentElement.dataset.fontSize = fontSize;
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
  async function addTask(task: { title: string; status: string; dependencies?: string[] }) {
    if (!selectedProject) return;
    await action('task-create', async () => {
      const created = await createTask(selectedProject.id, {
        title: task.title,
        ...(task.dependencies?.length ? { dependencies: task.dependencies } : {}),
      });
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
  async function createAgent(agent: Partial<Agent>) {
    if (!selectedProject) return;
    await action('agent-create', async () => {
      await post<Agent>(`/projects/${selectedProject.id}/agents`, agent);
      notice = $translate('team.createdToast');
      await refresh();
    });
  }
  async function updateAgent(agent: Agent) {
    await action(`agent-${agent.id}`, async () => {
      await post<Agent>(`/projects/${agent.projectId}/agents/${agent.id}`, agent);
      notice = $translate('team.updatedToast');
      await refresh();
    });
  }
  async function saveSettings(settings: AppSettings) {
    await action('settings', async () => {
      await post('/settings', {
        default_provider: settings.default_provider,
        default_model: settings.default_model,
        default_effort: settings.default_effort,
        claude_path: settings.claude_path,
        rojo_path: settings.rojo_path,
        git_path: settings.git_path,
        studio_mcp_path: settings.studio_mcp_path,
        studio_auto_open: settings.studio_auto_open === 'false' ? 'false' : 'true',
        concurrency: String(settings.concurrency),
        playtest_window_seconds: String(settings.playtest_window_seconds),
        openrouter_data_collection: settings.openrouter_data_collection ?? '',
        openrouter_zdr: settings.openrouter_zdr ?? '',
        openrouter_allow_fallbacks: settings.openrouter_allow_fallbacks ?? '',
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
  async function bindStudio(sessionId: string, projectId: string) {
    await action(`studio-${sessionId}`, async () => {
      await post(`/studios/${sessionId}/bind`, { projectId });
      await refresh();
    });
  }
  async function resolveDecision(decisionId: string, approve: boolean) {
    await action(`decision-${decisionId}`, async () => {
      await post(`/decisions/${decisionId}/resolve`, { approve });
      await refresh();
    });
  }
  async function refreshStudioSessions() {
    await action('studio-sessions-refresh', async () => {
      const result = await post<{ detected: boolean }>('/studio/sessions/refresh', {});
      studioSessionsDetected = result.detected;
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
    errorRetry = null;
    notice = '';
    try {
      await work();
    } catch (cause) {
      error = friendlyError(cause, $translate);
      if (cause instanceof APIError && cause.code === 'network') {
        errorRetry = () => void action(id, work);
      }
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
    return $translate(key) || status;
  }
  function validationLabel(validation: string): string {
    const key = `validation.${validation}` as TranslationKey;
    return $translate(key) || validation;
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
    <h1>{$translate('session.title')}</h1>
    <p>{error || $translate('session.body')}</p>
    <button class="primary" onclick={initialize}>{$translate('session.retry')}</button>
  </main>
{:else if snapshot}
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-icon">SF</div>
        <div><strong>{brand.name}</strong><small>{$translate('app.tagline')}</small></div>
      </div>
      <nav aria-label={brand.name}>
        {#each navGroups as group}
          {#if group.label}<p class="nav-group-label">{$translate(group.label)}</p>{/if}
          {#each group.items as item}
            <button
              class:active={view === item.id}
              onclick={() => (view = item.id)}
              title={`Alt+${item.index + 1}`}
              aria-current={view === item.id ? 'page' : undefined}
            >
              <item.icon size={18} /><span>{$translate(item.key)}</span>
            </button>
          {/each}
        {/each}
      </nav>
      <div class="sidebar-footer">
        <span class="presence" class:online={streamOnline} class:reconnecting={!streamOnline}
        ></span><span
          >{streamOnline ? $translate('footer.online') : $translate('footer.reconnecting')}</span
        >
        {#if snapshot.diagnostics.mockMode}<span class="chip">{$translate('footer.mockMode')}</span
          >{/if}
        <code>v{snapshot.diagnostics.version.replace(/^v/, '')}</code>
      </div>
    </aside>

    <div class="workspace">
      <header class="topbar">
        {#if showProjectSwitch}
          <label class="project-switch"
            ><span>{$translate('common.project')}</span><select
              bind:value={selectedProjectId}
              aria-label={$translate('common.project')}
              >{#each activeProjects as project}<option value={project.id}>{project.name}</option
                >{/each}</select
            ></label
          >
        {/if}
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
          <span>{error}</span>
          {#if errorRetry}
            <button class="toast-retry" onclick={errorRetry}>{$translate('common.retry')}</button>
          {/if}
          <button
            aria-label={$translate('common.close')}
            onclick={() => {
              error = '';
              errorRetry = null;
            }}><X size={17} /></button
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
            project={selectedProject}
            liveEvents={events.filter(
              (event) => selectedProject && event.projectId === selectedProject.id,
            )}
            agents={snapshot.agents.filter(
              (a) => selectedProject && a.projectId === selectedProject.id && a.enabled,
            )}
            tasks={snapshot.tasks.filter(
              (t) => selectedProject && t.projectId === selectedProject.id,
            )}
            runs={snapshot.runs}
            {agentName}
            {statusLabel}
            onSent={(id) => {
              selectedRunId = id;
            }}
            onSynced={() => void refresh()}
            onEnsureStream={connectStream}
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
            onRun={(project) => {
              selectedProjectId = project.id;
              view = 'chat';
            }}
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
          <OverviewView
            {snapshot}
            project={selectedProject}
            {busy}
            onRun={() => {
              view = 'chat';
            }}
          />
        {:else if view === 'team'}
          <TeamView
            agents={snapshot.agents}
            project={selectedProject}
            {busy}
            onCreate={createAgent}
            onUpdate={updateAgent}
            onRun={(agent) => {
              if (activeProjects.some((project) => project.id === agent.projectId)) {
                selectedProjectId = agent.projectId;
              }
              setPendingLeadAgent(agent.id);
              view = 'chat';
            }}
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
            {validationLabel}
            {payloadText}
            decisions={snapshot.decisions}
            onResolveDecision={resolveDecision}
            busy={busy.startsWith('run-')}
          />
        {:else if view === 'studios'}
          <StudiosView
            studios={snapshot.studios}
            {projects}
            {projectName}
            onBind={bindStudio}
            detected={studioSessionsDetected}
            onRefresh={refreshStudioSessions}
            busy={busy === 'studio-sessions-refresh'}
          />
        {:else if view === 'settings'}
          <SettingsView
            diagnostics={snapshot.diagnostics}
            settings={snapshot.settings}
            {theme}
            {fontSize}
            {busy}
            onLocale={changeLocale}
            onTheme={setTheme}
            onFontSize={setFontSize}
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
