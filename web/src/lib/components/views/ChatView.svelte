<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { Cpu, ImagePlus, Lock, MessagesSquare, Plus, TerminalSquare } from '@lucide/svelte';
  import {
    APIError,
    attachmentUrl,
    createTask,
    createThread,
    friendlyError,
    getLead,
    getPace,
    getProjectCheckpoints,
    getProjectDiff,
    getRunDiff,
    getRunDiffStructured,
    getStudioStatus,
    getThreadMessages,
    getThreads,
    post,
    rollbackRun,
    rollbackRunSelective,
    setLead,
    startSync,
    stopSync,
    taskDependencyBlockers,
    uploadAttachment,
  } from '$lib/api';
  import { isTaskBlocked, taskReadiness } from '$lib/tasksReadiness';
  import { getOpenRouterCapabilities } from '$lib/openrouter';
  import { getNVIDIACapabilities } from '$lib/nvidia';
  import { aggregateOpenRouterMessages } from '$lib/openrouterStream';
  import { parseAttachments } from '$lib/attachments';
  import Markdown from '$lib/components/Markdown.svelte';
  import StructuredDiff from '$lib/components/StructuredDiff.svelte';
  import Select from '$lib/components/ui/Select.svelte';
  import { foregroundRun, liveThreadRuns, queuedBehindForeground } from '$lib/runQueue';
  import { endsRun, mcpWithheldMessage } from '$lib/runStatus';
  import { LiveDiffDebounce, fileEditEventCount } from '$lib/liveDiff';
  import {
    extractQuestionFence,
    isStuckEscalation,
    normalizeQuestionPayload,
    shouldAnswerQuestion,
  } from '$lib/questionCard';
  import { isStaleGeneration } from '$lib/staleness';
  import {
    emptySelection,
    selectionCount,
    toApiSelection,
    type RollbackSelectionState,
  } from '$lib/rollbackSelection';
  import {
    cacheTokens,
    formatDate,
    formatTokens,
    locale,
    spendTokens,
    translate,
    type TranslationKey,
  } from '$lib/i18n';
  import { clearPendingLeadAgent, pendingLeadAgent } from '$lib/uiIntents';
  import type {
    Agent,
    ChatMessage,
    ChatThread,
    Checkpoint,
    Project,
    ProjectDiff,
    Run,
    RunDiffStructured,
    RunEvent,
    SelectiveRollbackResult,
    StudioStatus,
    Task,
    TokenUsage,
  } from '$lib/types';

  export let projectId: string | undefined;
  // Only used for its sync field: the studio badge above already tracks Studio
  // connectivity by polling, but sync status rides on the project payload
  // itself (it only changes in response to the calls below, or the session
  // dying on its own), so the caller's already-loaded project is enough.
  export let project: Project | undefined = undefined;
  export let agentName: (id: string) => string;
  export let statusLabel: (status: string) => string;
  export let liveEvents: RunEvent[] = [];
  export let onSent: (runId: string) => void = () => {};
  // Sync has no push notification of its own, so the caller must be nudged to
  // reload the project payload after a start/stop rather than this view
  // guessing at the new state.
  export let onSynced: () => void = () => {};
  // Second line of defense against a tab that never opened a live SSE
  // connection in the first place (see +page.svelte's connectStream): actually
  // sending a message is unambiguous proof this tab is in real use regardless
  // of what the Page Visibility API reports, or whether a visibilitychange
  // event has ever fired, so submitRun calls this unconditionally before
  // posting the run. It is idempotent — a no-op once already connected — so
  // calling it on every send costs nothing.
  export let onEnsureStream: () => void = () => {};
  export let agents: Agent[] = [];
  export let tasks: Task[] = [];
  export let runs: Run[] = [];

  let threads: ChatThread[] = [];
  let selectedThreadId = '';
  let messages: ChatMessage[] = [];
  // Runs returned by chat sends are immediately authoritative, while the
  // parent snapshot may update a moment later. Keeping them locally makes
  // the real model badge available from the first streamed message.
  let submittedRuns: Run[] = [];
  let loadingThreads = false;
  let loadingMessages = false;
  let sending = false;
  let draft = '';
  let messageListEl: HTMLDivElement;
  let atBottom = true;
  let mode: 'do' | 'plan' = 'do';
  let error = '';
  let errorRetry: (() => void) | null = null;
  let sentRunId: string | null = null;
  // The image currently opened full-size. Chat thumbnails are deliberately
  // small so a screenshot does not shove the conversation off the screen, which
  // leaves them too small to actually read — a Studio capture is a whole
  // viewport scaled into 220px. Clicking one opens it at full size.
  let lightboxSrc: string | null = null;
  let runDiff: RunDiffStructured | null = null;
  let runDiffRunId: string | null = null;
  let loadingDiff = false;
  let confirmingRollback = false;
  let rollingBack = false;
  let rollbackError = '';
  let rollbackResult: { branch: string; commitHash: string } | null = null;
  let selectiveSelection: RollbackSelectionState = emptySelection();
  let confirmingSelectiveRollback = false;
  let selectiveRollingBack = false;
  let selectiveRollbackError = '';
  let selectiveRollbackResult: SelectiveRollbackResult | null = null;
  let liveDiff: RunDiffStructured | null = null;
  let seenFileEditCount = 0;
  let liveDiffDebounce = new LiveDiffDebounce();
  let liveDiffTimer: ReturnType<typeof setTimeout> | undefined;
  let checkpointsLoading = false;
  let checkpointsLoaded = false;
  let checkpointsError = '';
  let projectCheckpoints: Checkpoint[] = [];
  let threadDiff: ProjectDiff | null = null;
  let threadDiffLoading = false;
  let rangeFrom = '';
  let rangeTo = 'HEAD';
  let rangeDiff: ProjectDiff | null = null;
  let rangeDiffLoading = false;
  let rangeDiffError = '';
  let loadedProjectId: string | undefined;
  // Bumped once per project switch. Every async load below captures it before
  // its await and only applies the result if it is still current afterward —
  // guards against a slow/reordered response from project A landing in the UI
  // after project B is already selected.
  let projectGeneration = 0;
  let leadAgentId = '';
  let applyingPendingLead = false;
  let attachedTaskId = '';
  let creatingThread = false;
  // Pasted images, uploaded but not yet sent. previewUrl is a local object URL
  // (never the server's own /attachments URL) so the chip renders instantly,
  // before the upload that produces `path` even resolves.
  let pendingAttachments: { path: string; previewUrl: string }[] = [];
  let uploadingCount = 0;
  let sendStartMs = 0;
  let nowMs = Date.now();
  let typicalSeconds = 0;
  let paceSamples = 0;
  const STUDIO_OFFLINE: StudioStatus = { open: 0, matched: 0, state: 'none' };
  const STUDIO_POLL_MS = 10_000;

  let studioStatus: StudioStatus = STUDIO_OFFLINE;
  let openingStudio = false;
  let syncBusy = false;
  let stopping = false;
  let cancellingQueuedRunIds = new Set<string>();
  let commandInfo = '';
  let progressInterval: ReturnType<typeof setInterval> | undefined;
  let studioInterval: ReturnType<typeof setInterval> | undefined;

  function providerName(provider: string) {
    switch (provider.toLowerCase()) {
      case 'nvidia':
        return 'NVIDIA NIM';
      case 'openrouter':
        return 'OpenRouter';
      case 'claude':
        return 'Claude Code';
      case 'mock':
        return $translate('common.mock');
      default:
        return provider;
    }
  }

  function taskStatusLabel(status: string): string {
    const key = `tasks.status.${status}` as TranslationKey;
    return $translate(key) || status;
  }

  function displayThreadTitle(title: string): string {
    if (title === 'New chat') return $translate('chat.defaultThreadTitle');
    if (title === 'Chat') return $translate('chat.threadTitleChat');
    return title;
  }

  function runIdentity(runId: string, provider = '', modelAlias = '') {
    const run =
      submittedRuns.find((item) => item.id === runId) ?? runs.find((item) => item.id === runId);
    const resolvedProvider = provider || run?.provider || '';
    const resolvedModel = modelAlias || run?.modelAlias || '';
    return resolvedProvider && resolvedModel
      ? { provider: providerName(resolvedProvider), model: resolvedModel }
      : null;
  }

  $: if (projectId && projectId !== loadedProjectId) {
    loadedProjectId = projectId;
    projectGeneration += 1;
    void loadThreads(projectId);
    void loadLead(projectId);
    void loadPace(projectId);
    void loadStudioStatus();
    attachedTaskId = '';
    clearPendingAttachments();
    resetCheckpointsState();
  } else if (!projectId && loadedProjectId) {
    loadedProjectId = undefined;
    projectGeneration += 1;
    threads = [];
    selectedThreadId = '';
    messages = [];
    submittedRuns = [];
    restoreActiveRun('');
    commandInfo = '';
    leadAgentId = '';
    typicalSeconds = 0;
    paceSamples = 0;
    studioStatus = STUDIO_OFFLINE;
    attachedTaskId = '';
    clearPendingAttachments();
    resetCheckpointsState();
  }

  onMount(() => {
    progressInterval = setInterval(() => {
      nowMs = Date.now();
    }, 1000);
    // A Studio opened after this view mounted must still be noticed, so the
    // badge polls instead of reading the status once.
    studioInterval = setInterval(() => void loadStudioStatus(), STUDIO_POLL_MS);
    void loadStudioStatus();
  });

  onDestroy(() => {
    if (progressInterval) clearInterval(progressInterval);
    if (studioInterval) clearInterval(studioInterval);
    if (liveDiffTimer) clearTimeout(liveDiffTimer);
    clearPendingAttachments();
  });

  async function loadStudioStatus() {
    try {
      studioStatus = await getStudioStatus(projectId);
    } catch {
      studioStatus = STUDIO_OFFLINE;
    }
  }

  function setError(message: string, retry: (() => void) | null = null) {
    error = message;
    errorRetry = retry;
  }

  // Stop the run this chat is waiting on. The strip stays up until a terminal
  // status arrives over SSE rather than being cleared here, because the run is
  // only really over once the scheduler says so — cancelling is not instant, and
  // hiding the strip early would claim an outcome that has not happened yet.
  async function stopRun() {
    if (!sentRunId || stopping) return;
    stopping = true;
    const runId = sentRunId;
    try {
      await post(`/runs/${runId}/cancel`, {});
    } catch (cause) {
      // A run that reached a terminal state between the click and the request is
      // the common race, and the scheduler rejects it as "not active". Nothing
      // went wrong — the run is stopped, which is what was asked for — so this
      // must not raise a red banner. Anything else is worth saying out loud.
      if (cause instanceof APIError && cause.code === 'run_action_failed') stopping = false;
      else if (cause instanceof APIError && cause.code === 'network') {
        setError($translate('error.network'), () => void stopRun());
        stopping = false;
      } else {
        setError(cause instanceof Error ? cause.message : String(cause));
        stopping = false;
      }
    }
  }

  async function cancelQueuedRun(runId: string) {
    if (!runId || cancellingQueuedRunIds.has(runId)) return;
    cancellingQueuedRunIds = new Set(cancellingQueuedRunIds).add(runId);
    try {
      await post(`/runs/${runId}/cancel`, {});
    } catch (cause) {
      if (!(cause instanceof APIError && cause.code === 'run_action_failed')) {
        setError(
          cause instanceof APIError && cause.code === 'network'
            ? $translate('error.network')
            : cause instanceof Error
              ? cause.message
              : String(cause),
        );
      }
    } finally {
      const next = new Set(cancellingQueuedRunIds);
      next.delete(runId);
      cancellingQueuedRunIds = next;
    }
  }

  // Stops for a stuck-escalation question card. sentRunId is normally already
  // cleared by the time this card renders — waiting_decision is terminal for
  // "is this run still live" purposes (see runStatus.ts's endsRun), so
  // stopRun's own sentRunId-only path cannot reach it — so each card carries
  // and cancels its own run id directly, independent of whichever run is
  // currently "active" in the composer.
  let stoppingWaitingRunIds = new Set<string>();

  async function stopWaitingRun(runId: string) {
    if (!runId || stoppingWaitingRunIds.has(runId)) return;
    stoppingWaitingRunIds = new Set(stoppingWaitingRunIds).add(runId);
    try {
      await post(`/runs/${runId}/cancel`, {});
      if (selectedThreadId) await loadMessages(selectedThreadId);
    } catch (cause) {
      // A run that reached a terminal state between the click and the request is
      // the common race, and the scheduler rejects it as "not active" — nothing
      // went wrong, so this must not raise a red banner. Anything else is worth
      // saying out loud.
      if (cause instanceof APIError && cause.code === 'run_action_failed') {
        // no-op
      } else if (cause instanceof APIError && cause.code === 'network') {
        setError($translate('error.network'), () => void stopWaitingRun(runId));
      } else {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      const next = new Set(stoppingWaitingRunIds);
      next.delete(runId);
      stoppingWaitingRunIds = next;
    }
  }

  // The single path to Studio, shared by the badge button and /open.
  async function openStudio() {
    if (!projectId || openingStudio) return;
    openingStudio = true;
    try {
      await post(`/projects/${projectId}/open-studio`, {});
    } finally {
      openingStudio = false;
      void loadStudioStatus();
    }
  }

  async function openStudioFromBadge() {
    setError('');
    try {
      await openStudio();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  // Start and stop share one handler and one badge: the same POST/DELETE pair
  // the server exposes on /projects/{id}/sync. Toggling reads project.sync
  // rather than a local boolean, so the badge reflects what the last project
  // reload actually reported, not an optimistic guess.
  async function toggleSync() {
    if (!projectId || syncBusy) return;
    syncBusy = true;
    setError('');
    try {
      if (project?.sync.active) {
        await stopSync(projectId);
      } else {
        await startSync(projectId);
      }
      onSynced();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      syncBusy = false;
    }
  }

  // Claude Code has no image content-block channel — StudioForge shells out
  // to the CLI with the prompt as a single positional argument
  // (claudecode/claude.go:213) — so a pasted screenshot only ever reaches the
  // agent as a file path Claude Code's own Read tool opens. This handler is
  // the one place that path gets minted: upload on paste, chip in the
  // composer, path folded into the prompt on send (see submitRun).
  async function handlePaste(e: ClipboardEvent) {
    const items = e.clipboardData?.items;
    if (!items) return;
    const images = Array.from(items).filter(
      (item) => item.kind === 'file' && item.type.startsWith('image/'),
    );
    if (images.length === 0) return;
    // Letting the default paste through here would, at best, insert nothing
    // and, in some browsers, drop a broken image placeholder into the
    // textarea — the image belongs in the chip row, not the draft text.
    e.preventDefault();
    for (const item of images) {
      const file = item.getAsFile();
      if (file) await attachImage(file);
    }
  }

  let fileInputEl: HTMLInputElement;

  async function pickImages(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    for (const file of Array.from(input.files ?? [])) await attachImage(file);
    input.value = '';
  }

  function openCommands() {
    draft = '/';
    slashMenuDismissed = false;
    void tick().then(() => composerEl?.focus());
  }

  async function attachImage(file: File) {
    if (!projectId) return;
    // Captured up front: if the project changes mid-upload, pendingAttachments
    // was already cleared synchronously by the reset block above, and this
    // upload's path lives under the OLD project's attachments directory —
    // pushing it into the new project's chip row after the fact would be
    // silently wrong, not just late.
    const generation = projectGeneration;
    setError('');
    uploadingCount += 1;
    const previewUrl = URL.createObjectURL(file);
    try {
      const { path } = await uploadAttachment(projectId, file);
      if (isStaleGeneration(generation, projectGeneration)) return;
      pendingAttachments = [...pendingAttachments, { path, previewUrl }];
    } catch (cause) {
      URL.revokeObjectURL(previewUrl);
      if (!isStaleGeneration(generation, projectGeneration)) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      uploadingCount -= 1;
    }
  }

  function removeAttachment(path: string) {
    const found = pendingAttachments.find((a) => a.path === path);
    if (found) URL.revokeObjectURL(found.previewUrl);
    pendingAttachments = pendingAttachments.filter((a) => a.path !== path);
  }

  // Object URLs are local to this browser tab and never sent anywhere — they
  // must be revoked explicitly or they outlive the File they were built from
  // for the life of the page, on every project/thread switch and unmount.
  function clearPendingAttachments() {
    for (const attachment of pendingAttachments) URL.revokeObjectURL(attachment.previewUrl);
    pendingAttachments = [];
  }

  const SLASH_COMMANDS: { name: string; usage: string; key: TranslationKey }[] = [
    { name: 'task', usage: '/task <title>', key: 'cmd.task' },
    { name: 'build', usage: '/build <desc>', key: 'cmd.build' },
    { name: 'playtest', usage: '/playtest <desc>', key: 'cmd.playtest' },
    { name: 'plan', usage: '/plan', key: 'cmd.plan' },
    { name: 'do', usage: '/do', key: 'cmd.do' },
    { name: 'open', usage: '/open', key: 'cmd.open' },
  ];
  let composerEl: HTMLTextAreaElement;
  let slashMenuDismissed = false;
  $: slashPrefix =
    draft.startsWith('/') && !draft.slice(1).includes(' ') ? draft.slice(1).toLowerCase() : null;
  $: if (slashPrefix === null) slashMenuDismissed = false;
  $: filteredSlashCommands =
    slashPrefix === null ? [] : SLASH_COMMANDS.filter((cmd) => cmd.name.startsWith(slashPrefix));
  $: showSlashMenu = filteredSlashCommands.length > 0 && !slashMenuDismissed;

  function selectSlashCommand(usage: string) {
    draft = `${usage.split(' ')[0]} `;
    slashMenuDismissed = true;
    void tick().then(() => composerEl?.focus());
  }

  // Slash commands let the operator act without leaving the composer.
  async function runCommand(text: string): Promise<boolean> {
    const [name, ...rest] = text.slice(1).split(' ');
    const arg = rest.join(' ').trim();
    draft = '';
    setError('');
    try {
      if (name === 'task' && arg && projectId) {
        await createTask(projectId, { title: arg });
        commandInfo = `${$translate('chat.cmdTaskDone')}: ${arg}`;
      } else if (name === 'plan') {
        mode = 'plan';
        commandInfo = $translate('chat.cmdModePlan');
      } else if (name === 'do') {
        mode = 'do';
        commandInfo = $translate('chat.cmdModeDo');
      } else if (name === 'open' && projectId) {
        await openStudio();
        commandInfo = $translate('chat.cmdOpeningStudio');
      } else if (name === 'build' && arg) {
        await submitRun(
          `As the project orchestrator, plan and build this, delegating to the engineer and QA sub-agents where useful, then hand off a short summary of what changed and what was verified: ${arg}`,
        );
      } else if (name === 'playtest' && arg) {
        await submitRun(
          `Build the change, then in Roblox Studio start Play mode, read the console with get_console_output, fix any errors, stop Play, and report the result: ${arg}`,
        );
      } else {
        commandInfo = $translate('chat.cmdHelp');
      }
    } catch (cause) {
      commandInfo = '';
      setError(cause instanceof Error ? cause.message : String(cause));
    }
    return true;
  }

  $: elapsedSeconds = sentRunId && sendStartMs ? Math.max(0, (nowMs - sendStartMs) / 1000) : 0;
  $: progressCapped = typicalSeconds <= 0 || elapsedSeconds >= typicalSeconds;
  $: progressPercent =
    typicalSeconds > 0 ? Math.min((elapsedSeconds / typicalSeconds) * 100, 100) : 0;
  $: stepCount = activeRunEvents.length;
  // Usage events carry the run's totals so far rather than a delta, so the
  // newest one wins; adding them up would count the same tokens repeatedly.
  // Spend and cache are then read off that one surviving payload, so the
  // "replace, don't sum" rule applies to both instead of just a single total.
  $: liveUsage = activeRunEvents.reduce<Partial<TokenUsage> | null>(
    (usage, event) => (event.type === 'usage' ? (event.payload as Partial<TokenUsage>) : usage),
    null,
  );
  $: liveSpendTokens = spendTokens(liveUsage);
  $: liveCacheTokens = cacheTokens(liveUsage);
  $: studioLabel = openingStudio
    ? $translate('chat.studioOpening')
    : studioStatus.state === 'matched'
      ? $translate('chat.studioMatched')
      : studioStatus.state === 'other'
        ? $translate('chat.studioOther')
        : studioStatus.state === 'blocked'
          ? $translate('chat.studioBlocked')
          : $translate('chat.studioNone');
  // Blocked is the one unreachable state opening Studio cannot fix — Studio is
  // already open, and the connection belongs to another client — so the badge
  // explains the fix instead of offering a button that would change nothing.
  $: studioHint =
    studioStatus.state === 'blocked'
      ? $translate('chat.studioBlockedHint')
      : $translate('chat.studioStatus');
  // Only a state Studio can be nudged out of, and only with a project to open.
  $: studioActionable =
    studioStatus.state !== 'matched' && studioStatus.state !== 'blocked' && !!projectId;
  $: syncActive = !!project?.sync.active;
  $: syncLabel = syncBusy
    ? syncActive
      ? $translate('chat.syncStopping')
      : $translate('chat.syncStarting')
    : syncActive
      ? $translate('chat.syncOn')
      : $translate('chat.syncOff');
  // The hint about pressing Connect only matters once a session exists to
  // connect to; before that the badge's own label already says everything.
  $: syncHint = syncActive ? $translate('chat.syncHint') : $translate('chat.syncStatus');

  function formatElapsed(seconds: number): string {
    const total = Math.max(0, Math.floor(seconds));
    const minutes = Math.floor(total / 60);
    const secs = total % 60;
    return `${minutes}:${secs.toString().padStart(2, '0')}`;
  }

  // Pull the agent's plain text out of an event payload; returns '' for events
  // that carry no readable message (system/tool/status noise, or Claude events
  // whose content is not text), so they never reach the chat as raw JSON.
  function messageText(payload: unknown): string {
    if (typeof payload === 'string') return payload;
    if (!payload || typeof payload !== 'object') return '';
    const value = payload as Record<string, unknown>;
    if (typeof value.text === 'string') return value.text;
    if (typeof value.message === 'string') return value.message;
    const message = value.message;
    if (message && typeof message === 'object') {
      const content = (message as Record<string, unknown>).content;
      if (Array.isArray(content)) {
        const parts = content
          .map((c) =>
            c && typeof c === 'object' && typeof (c as Record<string, unknown>).text === 'string'
              ? ((c as Record<string, unknown>).text as string)
              : '',
          )
          .filter(Boolean);
        if (parts.length) return parts.join('');
      }
    }
    const delta = value.delta;
    if (
      delta &&
      typeof delta === 'object' &&
      typeof (delta as Record<string, unknown>).text === 'string'
    ) {
      return (delta as Record<string, unknown>).text as string;
    }
    return '';
  }

  $: selectedThread = threads.find((thread) => thread.id === selectedThreadId);
  // The thread's lifetime totals, as opposed to liveSpendTokens/liveCacheTokens
  // below, which are only the run currently in flight.
  $: threadSpendTokens = spendTokens(selectedThread);
  $: threadCacheTokens = cacheTokens(selectedThread);
  $: leadKnown = !!leadAgentId && agents.some((agent) => agent.id === leadAgentId);
  // The lead agent is who actually answers a send (see submitRun: agentId is
  // always '', letting the scheduler pick the lead), so its model is the one
  // an image-vision warning below has to check — not any other enabled agent.
  $: leadAgent = leadKnown ? agents.find((agent) => agent.id === leadAgentId) : undefined;
  $: attachedTask = attachedTaskId ? tasks.find((task) => task.id === attachedTaskId) : undefined;
  // Display-only, mirroring the backend's authoritative internal/tasks.TaskReadiness:
  // the server still rejects the send with task_dependencies_incomplete if this
  // ever falls out of sync with it (e.g. a dependency finished between loading
  // the snapshot and sending).
  $: attachedTaskBlockers = attachedTaskId ? taskReadiness(tasks, attachedTaskId).blockers : [];
  $: attachedTaskBlocked = attachedTaskBlockers.length > 0;
  $: attachedTaskBlockersSummary = attachedTaskBlockers
    .map((blocker) => `${blocker.title || blocker.taskId} (${taskStatusLabel(blocker.status)})`)
    .join(', ');
  $: uploadingImage = uploadingCount > 0;
  // Keyed by model id so switching back to a model already checked this
  // session doesn't re-fire the request. `null` marks "known unknown" (still
  // loading, or the lookup failed) so it is never mistaken for a confirmed
  // non-vision model.
  let visionCapabilityCache: Record<string, boolean | null> = {};
  async function ensureVisionCapability(provider: string, modelId: string) {
    const cacheKey = `${provider}:${modelId}`;
    if (!modelId || cacheKey in visionCapabilityCache) return;
    visionCapabilityCache = { ...visionCapabilityCache, [cacheKey]: null };
    try {
      const caps =
        provider === 'nvidia'
          ? await getNVIDIACapabilities(modelId)
          : await getOpenRouterCapabilities(modelId);
      visionCapabilityCache = {
        ...visionCapabilityCache,
        [cacheKey]: caps.known ? caps.vision : null,
      };
    } catch {
      visionCapabilityCache = { ...visionCapabilityCache, [cacheKey]: null };
    }
  }
  $: if (
    (leadAgent?.provider === 'openrouter' || leadAgent?.provider === 'nvidia') &&
    leadAgent.modelAlias &&
    pendingAttachments.length > 0
  ) {
    void ensureVisionCapability(leadAgent.provider, leadAgent.modelAlias);
  }
  // Non-blocking: this never disables Send, it only surfaces a heads-up next
  // to the attachment chips (see the composer markup below).
  $: showVisionWarning =
    !!leadAgent &&
    (leadAgent.provider === 'openrouter' || leadAgent.provider === 'nvidia') &&
    pendingAttachments.length > 0 &&
    visionCapabilityCache[`${leadAgent.provider}:${leadAgent.modelAlias}`] === false;
  $: endedRunIds = new Set(
    liveEvents.filter((event) => endsRun(event, event.runId)).map((event) => event.runId),
  );
  $: currentThreadRuns = liveThreadRuns(runs, submittedRuns, selectedThreadId, endedRunIds);
  $: currentForegroundRun = foregroundRun(currentThreadRuns);
  $: queuedRuns = queuedBehindForeground(currentThreadRuns, currentForegroundRun);
  $: latestThreadRun = (() => {
    const byId = new Map<string, Run>();
    for (const run of submittedRuns) byId.set(run.id, run);
    for (const run of runs) byId.set(run.id, run);
    return [...byId.values()]
      .filter((run) => run.threadId === selectedThreadId)
      .sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt) || a.id.localeCompare(b.id))
      .pop();
  })();
  $: failedRunError =
    latestThreadRun?.status === 'failed' && latestThreadRun.error ? latestThreadRun.error : '';
  // A follow-up must not steal the live transcript or Stop button from the
  // run that is actually executing. It becomes foreground only after the
  // predecessor reaches a terminal state (from snapshot or SSE).
  $: if ((currentForegroundRun?.id ?? null) !== sentRunId) {
    sentRunId = currentForegroundRun?.id ?? null;
    sendStartMs = currentForegroundRun ? Date.parse(currentForegroundRun.createdAt) : 0;
    stopping = false;
    resetLiveDiff();
  }
  $: activeRunEvents = sentRunId ? liveEvents.filter((event) => event.runId === sentRunId) : [];
  $: liveFileEditCount = sentRunId ? fileEditEventCount(activeRunEvents, sentRunId) : 0;
  $: if (sentRunId && liveFileEditCount > seenFileEditCount) {
    seenFileEditCount = liveFileEditCount;
    scheduleLiveDiffFetch(sentRunId);
  }
  $: liveRunStatus = activeRunEvents.reduce<string>((status, event) => {
    if (event.type !== 'status' || event.rawType !== 'scheduler.state') return status;
    const payload = event.payload;
    if (!payload || typeof payload !== 'object') return status;
    const next = (payload as Record<string, unknown>).status;
    return typeof next === 'string' ? next : status;
  }, '');
  // Only the agent's own text belongs in the chat — drop system/tool/status/
  // stderr events so the conversation is not buried in machine chatter. A
  // "question" event is the one other kind that belongs here: it is how the
  // agent's turn ends when it wants the operator to pick between options
  // instead of just talking.
  $: activeLiveEvents = aggregateOpenRouterMessages(
    activeRunEvents.filter(
      (event) =>
        (event.type === 'message' && messageText(event.payload).trim() !== '') ||
        (event.type === 'question' && normalizeQuestionPayload(event.payload) !== null) ||
        mcpWithheldMessage(event) !== null,
    ),
  );

  // A card only offers live buttons while it is the very last thing in the
  // transcript — once anything newer exists (a later message, or a later live
  // event), clicking it would send an out-of-context answer, so it renders as
  // a static summary instead. A send already in flight counts as "newer" too.
  function isLatestQuestionCard(isLast: boolean): boolean {
    return isLast && !sending;
  }

  let answeredQuestionKeys = new Set<string>();

  // Shared by both the live-event card and the historical-message card: submit
  // the chosen option's label exactly as if the operator had typed it, and
  // disable this specific card immediately so a double-click (or a click after
  // the send already started) cannot fire twice.
  function answerQuestion(key: string, label: string) {
    if (!shouldAnswerQuestion(key, sending, answeredQuestionKeys)) return;
    answeredQuestionKeys = new Set(answeredQuestionKeys).add(key);
    void submitRun(label);
  }

  // Anything within this many pixels of the end counts as "at the bottom".
  const BOTTOM_SLACK = 80;

  function trackScroll() {
    if (!messageListEl) return;
    atBottom =
      messageListEl.scrollHeight - messageListEl.scrollTop - messageListEl.clientHeight <=
      BOTTOM_SLACK;
  }

  // Always an instant jump, never smooth: a freshly opened thread must be at
  // its newest message before the operator sees it.
  async function scrollToBottom() {
    atBottom = true;
    await tick();
    if (messageListEl) messageListEl.scrollTop = messageListEl.scrollHeight;
  }

  // Read through a function so `atBottom` is not itself a dependency — flipping
  // back to true by scrolling must not trigger a jump of its own.
  function followBottom() {
    if (atBottom) void scrollToBottom();
  }

  // A streamed reply must never yank the operator out of a message they are
  // reading, so new events only follow the bottom when it is already in view.
  $: if (messages.length || activeLiveEvents.length) followBottom();

  let handledRunIds = new Set<string>();
  $: {
    const newlyEnded = [...endedRunIds].filter((id) => !handledRunIds.has(id));
    if (newlyEnded.length > 0) {
      handledRunIds = new Set([...handledRunIds, ...newlyEnded]);
      const knownRuns = [...submittedRuns, ...runs];
      const selectedEnded = newlyEnded.filter(
        (id) => knownRuns.find((run) => run.id === id)?.threadId === selectedThreadId,
      );
      if (selectedEnded.length > 0) {
        // Reload history before streamed bubbles disappear; queued messages
        // are already persisted as run prompts and remain visible throughout.
        const threadAtSend = selectedThreadId;
        void loadMessages(threadAtSend);
        void loadRunDiff(selectedEnded[selectedEnded.length - 1]);
      }
    }
  }

  function restoreActiveRun(threadId: string) {
    runDiff = null;
    runDiffRunId = null;
    loadingDiff = false;
    resetRollbackState();
    resetLiveDiff();
    const active = foregroundRun(liveThreadRuns(runs, submittedRuns, threadId, endedRunIds));
    sentRunId = active?.id ?? null;
    sendStartMs = active ? Date.parse(active.createdAt) : 0;
  }

  async function loadThreads(id: string) {
    const generation = projectGeneration;
    loadingThreads = true;
    setError('');
    try {
      const result = await getThreads(id);
      if (isStaleGeneration(generation, projectGeneration)) return;
      threads = result;
      if (threads[0]) {
        await selectThread(threads[0].id);
      } else {
        selectedThreadId = '';
        messages = [];
        atBottom = true;
        restoreActiveRun('');
      }
    } catch (cause) {
      if (!isStaleGeneration(generation, projectGeneration)) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      if (!isStaleGeneration(generation, projectGeneration)) loadingThreads = false;
    }
  }

  async function selectThread(id: string) {
    selectedThreadId = id;
    commandInfo = '';
    restoreActiveRun(id);
    threadDiff = null;
    if (checkpointsLoaded) void loadThreadDiff();
    await loadMessages(id);
    // Opening a thread lands on its newest message regardless of where the
    // previous thread was scrolled to.
    await scrollToBottom();
  }

  async function loadMessages(id: string) {
    if (!id) return;
    const generation = projectGeneration;
    loadingMessages = true;
    setError('');
    try {
      const result = await getThreadMessages(id);
      if (isStaleGeneration(generation, projectGeneration) || selectedThreadId !== id) return;
      messages = result;
    } catch (cause) {
      if (!isStaleGeneration(generation, projectGeneration) && selectedThreadId === id) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      if (!isStaleGeneration(generation, projectGeneration) && selectedThreadId === id) {
        loadingMessages = false;
      }
    }
  }

  async function loadLead(id: string) {
    const generation = projectGeneration;
    try {
      const result = await getLead(id);
      if (isStaleGeneration(generation, projectGeneration)) return;
      leadAgentId = result;
    } catch (cause) {
      if (!isStaleGeneration(generation, projectGeneration)) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    }
  }

  async function loadPace(id: string) {
    const generation = projectGeneration;
    try {
      const pace = await getPace(id);
      if (isStaleGeneration(generation, projectGeneration)) return;
      typicalSeconds = pace.typicalSeconds;
      paceSamples = pace.samples;
    } catch {
      if (!isStaleGeneration(generation, projectGeneration)) {
        typicalSeconds = 0;
        paceSamples = 0;
      }
    }
  }

  function resetRollbackState() {
    confirmingRollback = false;
    rollingBack = false;
    rollbackError = '';
    rollbackResult = null;
    confirmingSelectiveRollback = false;
    selectiveRollingBack = false;
    selectiveRollbackError = '';
    selectiveRollbackResult = null;
    selectiveSelection = emptySelection();
  }

  async function loadRunDiff(runId: string) {
    loadingDiff = true;
    resetRollbackState();
    try {
      runDiff = await getRunDiffStructured(runId);
      runDiffRunId = runId;
    } catch (cause) {
      if (cause instanceof APIError) {
        runDiff = null;
        runDiffRunId = null;
      }
    } finally {
      loadingDiff = false;
    }
  }

  function fetchRunDiffPatch(runId: string): Promise<string> {
    return getRunDiff(runId).then((result) => result.diff);
  }

  function resetLiveDiff() {
    liveDiff = null;
    seenFileEditCount = 0;
    liveDiffDebounce.reset();
    if (liveDiffTimer) clearTimeout(liveDiffTimer);
    liveDiffTimer = undefined;
  }

  async function fetchLiveDiff(runId: string) {
    try {
      const result = await getRunDiffStructured(runId);
      if (sentRunId === runId) liveDiff = result;
    } catch {}
  }

  function scheduleLiveDiffFetch(runId: string) {
    const decision = liveDiffDebounce.onFileEdit(Date.now());
    if (decision.kind === 'fetch-now') {
      void fetchLiveDiff(runId);
      return;
    }
    if (liveDiffTimer) clearTimeout(liveDiffTimer);
    const fireAt = decision.fireAt;
    liveDiffTimer = setTimeout(
      () => {
        if (liveDiffDebounce.consume(fireAt)) void fetchLiveDiff(runId);
      },
      Math.max(0, fireAt - Date.now()),
    );
  }

  function resetCheckpointsState() {
    checkpointsLoading = false;
    checkpointsLoaded = false;
    checkpointsError = '';
    projectCheckpoints = [];
    threadDiff = null;
    threadDiffLoading = false;
    rangeFrom = '';
    rangeTo = 'HEAD';
    rangeDiff = null;
    rangeDiffLoading = false;
    rangeDiffError = '';
  }

  function threadRunIdSet(threadId: string): Set<string> {
    const byId = new Map<string, Run>();
    for (const run of submittedRuns) byId.set(run.id, run);
    for (const run of runs) byId.set(run.id, run);
    const ids = new Set<string>();
    for (const run of byId.values()) if (run.threadId === threadId) ids.add(run.id);
    return ids;
  }

  function earliestThreadCheckpoint(threadId: string): Checkpoint | undefined {
    const ids = threadRunIdSet(threadId);
    return [...projectCheckpoints]
      .filter((checkpoint) => ids.has(checkpoint.runId))
      .sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt))[0];
  }

  async function loadThreadDiff() {
    if (!projectId || !selectedThreadId) return;
    const threadAtLoad = selectedThreadId;
    const earliest = earliestThreadCheckpoint(threadAtLoad);
    if (!earliest) {
      threadDiff = null;
      return;
    }
    threadDiffLoading = true;
    try {
      const result = await getProjectDiff(projectId, earliest.commitHash, 'HEAD');
      if (selectedThreadId === threadAtLoad) threadDiff = result;
    } catch (cause) {
      if (selectedThreadId === threadAtLoad) {
        threadDiff = {
          stats: { filesChanged: 0, additions: 0, deletions: 0 },
          files: [],
          note: friendlyError(cause, $translate),
        };
      }
    } finally {
      if (selectedThreadId === threadAtLoad) threadDiffLoading = false;
    }
  }

  async function loadProjectCheckpoints() {
    if (!projectId || checkpointsLoaded || checkpointsLoading) return;
    const projectAtLoad = projectId;
    checkpointsLoading = true;
    checkpointsError = '';
    try {
      const result = await getProjectCheckpoints(projectAtLoad);
      if (projectId !== projectAtLoad) return;
      projectCheckpoints = result;
      checkpointsLoaded = true;
      const oldest = [...result].sort(
        (a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt),
      )[0];
      rangeFrom = oldest?.commitHash ?? '';
      rangeTo = 'HEAD';
      await loadThreadDiff();
    } catch (cause) {
      if (projectId === projectAtLoad) {
        checkpointsError =
          cause instanceof APIError
            ? friendlyError(cause, $translate)
            : $translate('chat.diffCheckpointsLoadError');
      }
    } finally {
      if (projectId === projectAtLoad) checkpointsLoading = false;
    }
  }

  function handleRangeDetailsToggle(event: Event) {
    if ((event.currentTarget as HTMLDetailsElement).open) void loadProjectCheckpoints();
  }

  function checkpointOptionLabel(checkpoint: Checkpoint): string {
    return `${checkpoint.commitHash.slice(0, 7)} · ${formatDate(checkpoint.createdAt, $locale)} · ${checkpoint.runId.slice(0, 8)}`;
  }

  async function applyRangeDiff() {
    if (!projectId || !rangeFrom || rangeDiffLoading) return;
    rangeDiffLoading = true;
    rangeDiffError = '';
    rangeDiff = null;
    try {
      rangeDiff = await getProjectDiff(projectId, rangeFrom, rangeTo);
    } catch (cause) {
      if (cause instanceof APIError && cause.code === 'unknown_checkpoint') {
        rangeDiffError = $translate('chat.diffRangeErrorUnknown');
      } else if (cause instanceof APIError && cause.code === 'invalid_range') {
        rangeDiffError = $translate('chat.diffRangeErrorInvalid');
      } else {
        rangeDiffError = friendlyError(cause, $translate);
      }
    } finally {
      rangeDiffLoading = false;
    }
  }

  async function doRollback() {
    if (!sentRunId || rollingBack) return;
    rollingBack = true;
    rollbackError = '';
    try {
      rollbackResult = await rollbackRun(sentRunId);
    } catch (cause) {
      rollbackError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      rollingBack = false;
    }
  }

  function selectiveRollbackErrorMessage(cause: unknown): string {
    if (cause instanceof APIError) {
      switch (cause.code) {
        case 'invalid_selection':
          return $translate('error.rollbackInvalidSelection');
        case 'dirty_worktree':
          return $translate('error.rollbackDirty');
        case 'later_change_conflict':
          return $translate('error.rollbackLaterChange');
        case 'patch_check_failed':
          return $translate('error.rollbackPatchCheck');
        case 'not_git_repo':
          return $translate('error.rollbackNotGitRepo');
        default:
          return cause.message;
      }
    }
    return cause instanceof Error ? cause.message : String(cause);
  }

  function selectionSummaryLabel(count: { files: number; hunks: number }): string {
    const parts: string[] = [];
    if (count.files > 0) parts.push(`${count.files} ${$translate('chat.revertSelectedFiles')}`);
    if (count.hunks > 0) parts.push(`${count.hunks} ${$translate('chat.revertSelectedHunks')}`);
    return parts.join(', ');
  }

  async function doSelectiveRollback() {
    if (!runDiffRunId || selectiveRollingBack) return;
    const targetRunId = runDiffRunId;
    selectiveRollingBack = true;
    selectiveRollbackError = '';
    try {
      const result = await rollbackRunSelective(targetRunId, toApiSelection(selectiveSelection));
      await loadRunDiff(targetRunId);
      selectiveRollbackResult = result;
    } catch (cause) {
      selectiveRollbackError = selectiveRollbackErrorMessage(cause);
    } finally {
      selectiveRollingBack = false;
    }
  }

  async function changeLead(agentId: string) {
    if (!projectId) return;
    const previous = leadAgentId;
    leadAgentId = agentId;
    try {
      await setLead(projectId, agentId);
    } catch (cause) {
      leadAgentId = previous;
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  async function applyPendingLead(agentId: string) {
    applyingPendingLead = true;
    await changeLead(agentId);
    if (leadAgentId === agentId) {
      commandInfo = `✓ ${$translate('chat.lead')}: ${agentName(agentId)}`;
    }
    clearPendingLeadAgent();
    applyingPendingLead = false;
  }

  $: if ($pendingLeadAgent && projectId && selectedThreadId && !applyingPendingLead) {
    void applyPendingLead($pendingLeadAgent);
  }

  async function newThread() {
    if (!projectId || creatingThread) return;
    creatingThread = true;
    setError('');
    try {
      const thread = await createThread(projectId, '');
      threads = [thread, ...threads];
      selectedThreadId = thread.id;
      messages = [];
      sentRunId = null;
      runDiff = null;
      runDiffRunId = null;
      loadingDiff = false;
      resetRollbackState();
      resetLiveDiff();
      threadDiff = null;
      atBottom = true;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      creatingThread = false;
    }
  }

  async function submitRun(prompt: string) {
    if (!projectId || !selectedThreadId || sending) return;
    onEnsureStream();
    const queueing = currentThreadRuns.length > 0;
    sending = true;
    setError('');
    commandInfo = '';
    draft = '';
    // Captured before clearing: on failure both need to reappear exactly as
    // the operator left them, and the chips must not flash empty while the
    // request is in flight.
    const attachments = pendingAttachments;
    pendingAttachments = [];
    if (!queueing) {
      sendStartMs = Date.now();
      nowMs = sendStartMs;
    }
    const optimistic: ChatMessage = {
      role: 'user',
      text: prompt,
      at: new Date().toISOString(),
      runId: '',
    };
    messages = [...messages, optimistic];
    try {
      const run = await post<Run>(
        '/runs',
        {
          projectId,
          agentId: '',
          prompt,
          threadId: selectedThreadId,
          mode,
          ...(attachedTaskId ? { taskId: attachedTaskId } : {}),
          ...(attachments.length ? { attachments: attachments.map((a) => a.path) } : {}),
        },
        { 'Idempotency-Key': crypto.randomUUID() },
      );
      // The server folded the paths into prompt_snapshot; the local object
      // URLs served their purpose as instant previews and are no longer
      // needed once the real message reloads with server-backed thumbnails.
      for (const attachment of attachments) URL.revokeObjectURL(attachment.previewUrl);
      submittedRuns = [...submittedRuns.filter((item) => item.id !== run.id), run];
      if (queueing) commandInfo = $translate('chat.queuedAdded');
      runDiff = null;
      runDiffRunId = null;
      resetRollbackState();
      onSent(run.id);
    } catch (cause) {
      // The send failed: drop the optimistic bubble and give the text and
      // attachments back.
      messages = messages.filter((m) => m !== optimistic);
      draft = prompt;
      pendingAttachments = attachments;
      if (cause instanceof APIError && cause.code === 'task_dependencies_incomplete') {
        setError(taskDependenciesIncompleteMessage(cause));
      } else {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      sending = false;
    }
  }

  // The backend is authoritative on task_dependencies_incomplete; this only
  // localizes its blocker list for display, the same shape the inline
  // composer hint below already renders from the local snapshot.
  function taskDependenciesIncompleteMessage(err: APIError): string {
    const blockers = taskDependencyBlockers(err);
    const summary = blockers
      .map((blocker) => `${blocker.title || blocker.taskId} (${taskStatusLabel(blocker.status)})`)
      .join(', ');
    return summary
      ? `${$translate('chat.taskBlockedHint')} ${summary}`
      : $translate('chat.taskBlockedHint');
  }

  async function send() {
    const text = draft.trim();
    if (!text || sending) return;
    if (text.startsWith('/')) {
      await runCommand(text);
      return;
    }
    await submitRun(text);
  }
</script>

<section class="page-heading">
  <div>
    <h1>
      {selectedThread ? displayThreadTitle(selectedThread.title) : $translate('chat.threadsTitle')}
    </h1>
    {#if threadSpendTokens > 0 || threadCacheTokens > 0}
      <p class="thread-tokens">
        {$translate('common.spend')}
        {formatTokens(threadSpendTokens, $locale)}{#if threadCacheTokens > 0}
          <span class="token-cache"
            >· {$translate('common.cache')} {formatTokens(threadCacheTokens, $locale)}</span
          >{/if}
      </p>
    {/if}
  </div>
  <div class="chat-toolbar">
    {#if studioActionable}
      <button
        type="button"
        class="studio-badge"
        class:warn={studioStatus.state === 'other'}
        disabled={openingStudio}
        title={`${studioLabel} — ${$translate('chat.studioOpen')}`}
        aria-label={$translate('chat.studioOpen')}
        onclick={openStudioFromBadge}
      >
        <span class="dot"></span>{studioLabel}
      </button>
    {:else}
      <span
        class="studio-badge"
        class:online={studioStatus.state === 'matched'}
        class:warn={studioStatus.state === 'blocked'}
        title={`${studioLabel} — ${studioHint}`}
      >
        <span class="dot"></span>{studioLabel}
      </span>
    {/if}
    {#if project}
      <button
        type="button"
        class="studio-badge sync-badge"
        class:online={syncActive}
        disabled={syncBusy}
        title={`${syncLabel} — ${syncHint}`}
        aria-label={syncActive ? $translate('chat.syncStop') : $translate('chat.syncStart')}
        onclick={toggleSync}
      >
        <span class="dot"></span>{syncLabel}
      </button>
    {/if}
    {#if agents.length > 0}
      <div class="lead-select">
        <span>{$translate('chat.lead')}</span>
        <Select
          value={leadKnown ? leadAgentId : ''}
          label={$translate('chat.lead')}
          placeholder={$translate('common.none')}
          disabled={!projectId}
          options={agents.map((agent) => ({ value: agent.id, label: agent.name }))}
          onchange={changeLead}
        />
      </div>
    {/if}
  </div>
</section>
<section class="chat-layout">
  <div class="thread-list">
    <div class="thread-list-header">
      <h2>{$translate('chat.threadsTitle')}</h2>
      <button class="new-thread" onclick={newThread} disabled={!projectId || creatingThread}>
        <Plus size={14} />{$translate('chat.newThread')}
      </button>
    </div>
    <div class="thread-items">
      {#if loadingThreads}
        <div class="empty"><p>{$translate('common.loading')}</p></div>
      {:else}
        {#each threads as thread (thread.id)}
          <button
            class:active={thread.id === selectedThreadId}
            onclick={() => selectThread(thread.id)}
          >
            <strong>{displayThreadTitle(thread.title)}</strong>
            <time>{formatDate(thread.updatedAt, $locale)}</time>
          </button>
        {:else}
          <div class="empty"><p>{$translate('chat.threadsEmpty')}</p></div>
        {/each}
      {/if}
    </div>
  </div>
  <article class="chat-panel">
    {#if error}
      <div class="chat-error">
        <span>{error}</span>
        {#if errorRetry}
          <button type="button" class="retry-button" onclick={errorRetry}
            >{$translate('common.retry')}</button
          >
        {/if}
      </div>
    {/if}
    <div class="message-list" aria-live="polite" bind:this={messageListEl} onscroll={trackScroll}>
      {#if loadingMessages}
        <div class="empty"><p>{$translate('common.loading')}</p></div>
      {:else if messages.length === 0 && activeLiveEvents.length === 0}
        <div class="chat-empty">
          <div class="chat-empty-mark" aria-hidden="true"><MessagesSquare size={24} /></div>
          <p class="chat-empty-lead">{$translate('chat.empty')}</p>
          <div class="chat-empty-commands">
            <p class="instrument">{$translate('chat.commandsTitle')}</p>
            <div class="chat-empty-grid">
              {#each SLASH_COMMANDS as cmd (cmd.name)}
                <button
                  type="button"
                  class="chat-empty-command"
                  disabled={!projectId || !selectedThreadId}
                  onclick={() => selectSlashCommand(cmd.usage)}
                >
                  <code>{cmd.usage}</code>
                  <span>{$translate(cmd.key)}</span>
                </button>
              {/each}
            </div>
          </div>
        </div>
      {:else}
        {#each messages as message, index (message.runId + '-' + message.role + '-' + index)}
          {@const identity = runIdentity(message.runId, message.provider, message.modelAlias)}
          {@const question = message.role === 'agent' ? extractQuestionFence(message.text) : null}
          {@const parsed = parseAttachments(question ? question.remainder : message.text)}
          {@const questionKey = `msg-${message.runId}-${index}`}
          {@const isLatestQuestion = isLatestQuestionCard(
            index === messages.length - 1 && activeLiveEvents.length === 0,
          )}
          <div
            class={`bubble bubble-${message.role}`}
            class:bubble-completed={message.role === 'agent' && message.status === 'completed'}
            class:bubble-failed={message.role === 'agent' &&
              (message.status === 'failed' || message.status === 'cancelled')}
          >
            <div class="bubble-meta">
              <span class="bubble-label"
                >{message.role === 'user' ? $translate('chat.you') : $translate('chat.agent')}</span
              >
              {#if message.role === 'agent' && message.status && message.status !== 'completed'}
                <span class={`status status-${message.status}`}>{statusLabel(message.status)}</span>
              {/if}
              {#if message.role === 'agent' && identity}
                <span
                  class="model-identity"
                  title={`${$translate('chat.actualModel')}: ${identity.provider} · ${identity.model}`}
                >
                  <Cpu size={11} aria-hidden="true" />
                  <span>{identity.provider}</span>
                  <code>{identity.model}</code>
                </span>
              {/if}
              <time>{formatDate(message.at, $locale)}</time>
            </div>
            {#if parsed.text}
              {#if message.role === 'agent'}
                <Markdown source={parsed.text} />
              {:else}
                <p>{parsed.text}</p>
              {/if}
            {/if}
            {#if parsed.images.length > 0 && projectId}
              <div class="message-images">
                {#each parsed.images as image (image)}
                  <button
                    type="button"
                    class="message-image-button"
                    title={$translate('chat.openImage')}
                    aria-label={$translate('chat.openImage')}
                    onclick={() => (lightboxSrc = attachmentUrl(projectId, image))}
                  >
                    <img
                      class="message-image"
                      src={attachmentUrl(projectId, image)}
                      alt=""
                      loading="lazy"
                    />
                  </button>
                {/each}
              </div>
            {/if}
            {#if question}
              {@const stuck = isStuckEscalation(message.rawType)}
              <div
                class="question-card"
                class:question-card-static={!isLatestQuestion}
                class:stuck-question-card={stuck}
              >
                <p class="question-text">{question.card.question}</p>
                <div class="question-options">
                  {#each question.card.options as option, optionIndex (optionIndex)}
                    <button
                      type="button"
                      class="question-option"
                      disabled={!isLatestQuestion || answeredQuestionKeys.has(questionKey)}
                      onclick={() => answerQuestion(questionKey, option.label)}
                    >
                      <span class="question-option-label">{option.label}</span>
                      {#if option.description}
                        <span class="question-option-desc">{option.description}</span>
                      {/if}
                    </button>
                  {/each}
                  {#if stuck}
                    <button
                      type="button"
                      class="question-option stuck-stop-option"
                      disabled={!isLatestQuestion || stoppingWaitingRunIds.has(message.runId)}
                      title={$translate('chat.stuckStopTitle')}
                      onclick={() => stopWaitingRun(message.runId)}
                    >
                      <span class="question-option-label"
                        >{stoppingWaitingRunIds.has(message.runId)
                          ? $translate('chat.stopping')
                          : $translate('chat.stuckStop')}</span
                      >
                    </button>
                  {/if}
                </div>
              </div>
            {/if}
          </div>
        {/each}
        {#each activeLiveEvents as event, eventIndex (event.id)}
          {@const liveIdentity = runIdentity(event.runId)}
          {@const withheldMessage = mcpWithheldMessage(event)}
          {#if withheldMessage !== null}
            <div class="mcp-withheld-banner" role="status">
              <strong>{$translate('chat.studioWithheld')}</strong>
              {withheldMessage}
            </div>
          {:else}
            {@const stuckQuestion = isStuckEscalation(event.rawType)
              ? extractQuestionFence(messageText(event.payload))
              : null}
            {@const liveQuestion =
              !stuckQuestion && event.type === 'question'
                ? normalizeQuestionPayload(event.payload)
                : null}
            {@const isLatestQuestion = isLatestQuestionCard(
              eventIndex === activeLiveEvents.length - 1,
            )}
            <div class="bubble bubble-agent bubble-live">
              <div class="bubble-meta">
                <span class="bubble-label"
                  >{event.agentId ? agentName(event.agentId) : $translate('chat.agent')}</span
                >
                {#if liveIdentity}
                  <span
                    class="model-identity"
                    title={`${$translate('chat.actualModel')}: ${liveIdentity.provider} · ${liveIdentity.model}`}
                  >
                    <Cpu size={11} aria-hidden="true" />
                    <span>{liveIdentity.provider}</span>
                    <code>{liveIdentity.model}</code>
                  </span>
                {/if}
                <time
                  >{new Intl.DateTimeFormat($locale, { timeStyle: 'medium' }).format(
                    new Date(event.createdAt),
                  )}</time
                >
              </div>
              {#if stuckQuestion}
                {#if stuckQuestion.remainder}<Markdown source={stuckQuestion.remainder} />{/if}
                <div
                  class="question-card stuck-question-card"
                  class:question-card-static={!isLatestQuestion}
                >
                  <p class="question-text">{stuckQuestion.card.question}</p>
                  <div class="question-options">
                    {#each stuckQuestion.card.options as option, optionIndex (optionIndex)}
                      <button
                        type="button"
                        class="question-option"
                        disabled={!isLatestQuestion || answeredQuestionKeys.has(String(event.id))}
                        onclick={() => answerQuestion(String(event.id), option.label)}
                      >
                        <span class="question-option-label">{option.label}</span>
                        {#if option.description}
                          <span class="question-option-desc">{option.description}</span>
                        {/if}
                      </button>
                    {/each}
                    <button
                      type="button"
                      class="question-option stuck-stop-option"
                      disabled={!isLatestQuestion || stoppingWaitingRunIds.has(event.runId)}
                      title={$translate('chat.stuckStopTitle')}
                      onclick={() => stopWaitingRun(event.runId)}
                    >
                      <span class="question-option-label"
                        >{stoppingWaitingRunIds.has(event.runId)
                          ? $translate('chat.stopping')
                          : $translate('chat.stuckStop')}</span
                      >
                    </button>
                  </div>
                </div>
              {:else if liveQuestion}
                <div class="question-card" class:question-card-static={!isLatestQuestion}>
                  <p class="question-text">{liveQuestion.question}</p>
                  <div class="question-options">
                    {#each liveQuestion.options as option, optionIndex (optionIndex)}
                      <button
                        type="button"
                        class="question-option"
                        disabled={!isLatestQuestion || answeredQuestionKeys.has(String(event.id))}
                        onclick={() => answerQuestion(String(event.id), option.label)}
                      >
                        <span class="question-option-label">{option.label}</span>
                        {#if option.description}
                          <span class="question-option-desc">{option.description}</span>
                        {/if}
                      </button>
                    {/each}
                  </div>
                </div>
              {:else}
                {@const liveParsed = parseAttachments(messageText(event.payload))}
                {#if liveParsed.text}
                  <Markdown source={liveParsed.text} />
                {/if}
                <!-- A screenshot the agent captured mid-run arrives as an
                     ordinary message carrying the attachments block, the same
                     shape a pasted image has. Rendering it here as well as in
                     the history below is what makes it appear while the run is
                     still going, rather than only once the turn is persisted. -->
                {#if liveParsed.images.length > 0 && projectId}
                  <div class="message-images">
                    {#each liveParsed.images as image (image)}
                      <button
                        type="button"
                        class="message-image-button"
                        title={$translate('chat.openImage')}
                        aria-label={$translate('chat.openImage')}
                        onclick={() => (lightboxSrc = attachmentUrl(projectId, image))}
                      >
                        <img
                          class="message-image"
                          src={attachmentUrl(projectId, image)}
                          alt=""
                          loading="lazy"
                        />
                      </button>
                    {/each}
                  </div>
                {/if}
              {/if}
            </div>
          {/if}
        {/each}
        {#if sentRunId && liveDiff}
          <div class="live-diff-card">
            <p class="live-diff-heading">{$translate('chat.liveDiffHeading')}</p>
            {#if liveDiff.studioDirectEdits}
              <p class="diff-studio-notice">{$translate('chat.diffStudioDirect')}</p>
            {/if}
            <StructuredDiff
              stats={liveDiff.stats}
              files={liveDiff.files}
              note={liveDiff.note ?? ''}
              compact
            />
          </div>
        {/if}
      {/if}
    </div>
    {#if !atBottom}
      <button
        class="scroll-down"
        type="button"
        title={$translate('chat.scrollDown')}
        aria-label={$translate('chat.scrollDown')}
        onclick={scrollToBottom}>↓</button
      >
    {/if}
    {#if sentRunId}
      <div class="progress-strip" aria-live="polite">
        <div class="progress-track">
          <div
            class="progress-fill"
            class:indeterminate={progressCapped}
            style={progressCapped ? undefined : `width: ${progressPercent}%`}
          ></div>
        </div>
        <div class="progress-row">
          <div class="progress-text-block">
            <p class="progress-text">
              {(liveRunStatus || currentForegroundRun?.status) === 'running'
                ? $translate('chat.working')
                : liveRunStatus || currentForegroundRun
                  ? statusLabel(liveRunStatus || currentForegroundRun?.status || '')
                  : $translate('chat.working')}
              {formatElapsed(elapsedSeconds)} · {stepCount}
              {$translate('chat.steps')}{#if liveSpendTokens > 0}
                · {formatTokens(liveSpendTokens, $locale)}
                {$translate('chat.tokens')}{/if}{#if paceSamples > 0}
                · ~{formatElapsed(typicalSeconds)} {$translate('chat.typical')}{/if}
            </p>
            {#if liveCacheTokens > 0}
              <p class="progress-text progress-cache">
                {$translate('common.cache')}
                {formatTokens(liveCacheTokens, $locale)}
              </p>
            {/if}
          </div>
          <button
            type="button"
            class="stop-button"
            disabled={stopping}
            title={$translate('chat.stopTitle')}
            aria-label={$translate('chat.stopTitle')}
            onclick={stopRun}
          >
            {stopping ? $translate('chat.stopping') : $translate('chat.stop')}
          </button>
        </div>
      </div>
    {/if}
    {#if queuedRuns.length > 0}
      <div class="queue-panel" aria-live="polite">
        <div class="queue-heading">
          <div>
            <strong>{queuedRuns.length} {$translate('chat.queueCount')}</strong>
            <span>{$translate('chat.queueHint')}</span>
          </div>
        </div>
        <div class="queue-list">
          {#each queuedRuns as queued, index (queued.id)}
            <div class="queue-item">
              <span class="queue-position" aria-hidden="true">{index + 1}</span>
              <span class="queue-prompt" title={queued.promptSnapshot ?? ''}
                >{queued.promptSnapshot || $translate('chat.queuedMessage')}</span
              >
              <button
                type="button"
                class="queue-cancel"
                disabled={cancellingQueuedRunIds.has(queued.id)}
                aria-label={$translate('chat.cancelQueued')}
                title={$translate('chat.cancelQueued')}
                onclick={() => cancelQueuedRun(queued.id)}
              >
                {cancellingQueuedRunIds.has(queued.id) ? '…' : '×'}
              </button>
            </div>
          {/each}
        </div>
      </div>
    {/if}
    {#if failedRunError}
      <div class="chat-error">
        <span>{$translate('error.runFailed')}: {failedRunError}</span>
      </div>
    {/if}
    {#if loadingDiff}
      <p class="diff-muted">{$translate('common.loading')}</p>
    {:else if runDiff}
      <!-- Above whatever git had to say, including a note explaining why it had
           nothing: a run that edited Studio directly is exactly the run whose
           empty or unavailable diff is most likely to be read as "nothing
           happened". -->
      {#if runDiff.studioDirectEdits}
        <p class="diff-studio-notice">{$translate('chat.diffStudioDirect')}</p>
      {/if}
      {#if !(runDiff.studioDirectEdits && runDiff.files.length === 0 && !runDiff.note)}
        <StructuredDiff
          stats={runDiff.stats}
          files={runDiff.files}
          note={runDiff.note ?? ''}
          getRawPatch={runDiffRunId ? () => fetchRunDiffPatch(runDiffRunId as string) : null}
          patchFileName={runDiffRunId ? `run-${runDiffRunId}.patch` : 'changes.patch'}
          selectable
          bind:selection={selectiveSelection}
        />
      {/if}
      {#if runDiff.checkpoint}
        <div class="rollback-row">
          {#if rollbackResult}
            <p class="rollback-success">
              {$translate('chat.rollbackDonePrefix')} <code>{rollbackResult.branch}</code>
            </p>
          {:else if confirmingRollback}
            <div class="rollback-confirm">
              <p class="rollback-confirm-text">
                {$translate('chat.rollbackConfirmTitle')}
                <code>{runDiff.checkpoint.commitHash.slice(0, 7)}</code>
                ({runDiff.checkpoint.label})
              </p>
              <p class="rollback-explain">{$translate('chat.rollbackExplain')}</p>
              {#if runDiff.studioDirectEdits}
                <p class="rollback-studio-notice">{$translate('chat.rollbackStudioDirect')}</p>
              {/if}
              {#if rollbackError}
                <p class="rollback-error">{rollbackError}</p>
              {/if}
              <div class="rollback-actions">
                <button
                  type="button"
                  onclick={() => (confirmingRollback = false)}
                  disabled={rollingBack}
                >
                  {$translate('common.cancel')}
                </button>
                <button
                  type="button"
                  class="rollback-confirm-button"
                  disabled={rollingBack}
                  onclick={doRollback}
                >
                  {rollingBack
                    ? $translate('chat.rollbackWorking')
                    : $translate('chat.rollbackConfirmButton')}
                </button>
              </div>
            </div>
          {:else}
            <button
              type="button"
              class="rollback-button"
              onclick={() => (confirmingRollback = true)}
            >
              {$translate('chat.rollbackButton')}
            </button>
          {/if}
        </div>
        {@const selCount = selectionCount(selectiveSelection)}
        {#if selectiveRollbackResult || selCount.files > 0 || selCount.hunks > 0}
          <div class="rollback-row selective-rollback-row">
            {#if selectiveRollbackResult}
              <p class="rollback-success">
                {$translate('chat.revertDone')}: {selectionSummaryLabel({
                  files: selectiveRollbackResult.revertedFiles,
                  hunks: selectiveRollbackResult.revertedHunks,
                })}
                {#if selectiveRollbackResult.safetyCommit}
                  <code>{selectiveRollbackResult.safetyCommit.slice(0, 7)}</code>
                {/if}
              </p>
            {:else if confirmingSelectiveRollback}
              <div class="rollback-confirm">
                <p class="rollback-confirm-text">{$translate('chat.revertConfirm')}</p>
                {#if selectiveRollbackError}
                  <p class="rollback-error">{selectiveRollbackError}</p>
                {/if}
                <div class="rollback-actions">
                  <button
                    type="button"
                    onclick={() => (confirmingSelectiveRollback = false)}
                    disabled={selectiveRollingBack}
                  >
                    {$translate('common.cancel')}
                  </button>
                  <button
                    type="button"
                    class="rollback-confirm-button"
                    disabled={selectiveRollingBack}
                    onclick={doSelectiveRollback}
                  >
                    {selectiveRollingBack
                      ? $translate('chat.rollbackWorking')
                      : $translate('chat.revertSelected')}
                  </button>
                </div>
              </div>
            {:else}
              <button
                type="button"
                class="rollback-button"
                onclick={() => (confirmingSelectiveRollback = true)}
              >
                {$translate('chat.revertSelected')}
                {selectionSummaryLabel(selCount)}
              </button>
            {/if}
          </div>
        {/if}
      {/if}
    {/if}
    {#if projectId && selectedThreadId}
      <details class="diff-panel diff-range-panel" ontoggle={handleRangeDetailsToggle}>
        <summary>{$translate('chat.diffRangeSection')}</summary>
        <div class="diff-range-body">
          <div class="diff-range-section">
            <p class="diff-range-subtitle">{$translate('chat.diffThreadTitle')}</p>
            {#if checkpointsLoading || threadDiffLoading}
              <p class="diff-muted">{$translate('common.loading')}</p>
            {:else if checkpointsError}
              <p class="diff-muted">{checkpointsError}</p>
            {:else if threadDiff}
              <StructuredDiff
                stats={threadDiff.stats}
                files={threadDiff.files}
                note={threadDiff.note ?? ''}
                emptyMessage={$translate('chat.diffRangeEmpty')}
              />
            {:else if checkpointsLoaded}
              <p class="diff-muted">{$translate('chat.diffRangeEmpty')}</p>
            {/if}
          </div>
          <div class="diff-range-section">
            <p class="diff-range-subtitle">{$translate('chat.diffRangeTitle')}</p>
            {#if checkpointsLoading}
              <p class="diff-muted">{$translate('common.loading')}</p>
            {:else if checkpointsError}
              <p class="diff-muted">{checkpointsError}</p>
            {:else if checkpointsLoaded && projectCheckpoints.length === 0}
              <p class="diff-muted">{$translate('chat.diffRangeEmpty')}</p>
            {:else if checkpointsLoaded}
              <div class="diff-range-picker">
                <label class="diff-range-field">
                  <span>{$translate('chat.diffRangeFrom')}</span>
                  <Select
                    bind:value={rangeFrom}
                    options={projectCheckpoints.map((checkpoint) => ({
                      value: checkpoint.commitHash,
                      label: checkpointOptionLabel(checkpoint),
                    }))}
                  />
                </label>
                <label class="diff-range-field">
                  <span>{$translate('chat.diffRangeTo')}</span>
                  <Select
                    bind:value={rangeTo}
                    options={[
                      { value: 'HEAD', label: $translate('chat.diffRangeHead') },
                      ...projectCheckpoints.map((checkpoint) => ({
                        value: checkpoint.commitHash,
                        label: checkpointOptionLabel(checkpoint),
                      })),
                    ]}
                  />
                </label>
                <button
                  type="button"
                  class="diff-range-apply"
                  disabled={rangeDiffLoading || !rangeFrom || !rangeTo}
                  onclick={applyRangeDiff}
                >
                  {rangeDiffLoading
                    ? $translate('common.loading')
                    : $translate('chat.diffRangeApply')}
                </button>
              </div>
              {#if rangeDiffError}
                <p class="diff-muted">{rangeDiffError}</p>
              {:else if rangeDiff}
                <StructuredDiff
                  stats={rangeDiff.stats}
                  files={rangeDiff.files}
                  note={rangeDiff.note ?? ''}
                  emptyMessage={$translate('chat.diffRangeEmpty')}
                />
              {/if}
            {/if}
          </div>
        </div>
      </details>
    {/if}
    {#if attachedTask}
      <div class="attached-task-chip" class:blocked={attachedTaskBlocked}>
        {#if attachedTaskBlocked}<Lock size={12} />{/if}
        <span>{attachedTask.title}</span>
        <button
          type="button"
          class="chip-clear"
          onclick={() => (attachedTaskId = '')}
          aria-label={$translate('chat.noTask')}>×</button
        >
      </div>
    {/if}
    {#if attachedTaskBlocked}
      <p class="task-blocked-hint">
        <Lock size={13} />
        {$translate('chat.taskBlockedHint')}
        {attachedTaskBlockersSummary}
      </p>
    {/if}
    {#if pendingAttachments.length > 0 || uploadingImage}
      <div class="attachment-row">
        {#each pendingAttachments as attachment (attachment.path)}
          <div class="attachment-chip">
            <img class="attachment-thumb" src={attachment.previewUrl} alt="" />
            <button
              type="button"
              class="chip-clear"
              onclick={() => removeAttachment(attachment.path)}
              aria-label={$translate('chat.removeAttachment')}>×</button
            >
          </div>
        {/each}
        {#if uploadingImage}
          <span class="attachment-uploading">{$translate('chat.uploadingImage')}</span>
        {/if}
      </div>
    {/if}
    {#if showVisionWarning}
      <p class="vision-warning">{$translate('openrouter.visionWarning')}</p>
    {/if}
    {#if commandInfo}<p class="command-info">{commandInfo}</p>{/if}
    <form
      class="composer"
      onsubmit={(e) => {
        e.preventDefault();
        send();
      }}
    >
      {#if showSlashMenu}
        <div class="slash-menu" role="listbox" aria-label={$translate('chat.commandsTitle')}>
          <p class="slash-menu-title">{$translate('chat.commandsTitle')}</p>
          {#each filteredSlashCommands as cmd (cmd.name)}
            <button
              type="button"
              class="slash-menu-item"
              role="option"
              aria-selected="false"
              onclick={() => selectSlashCommand(cmd.usage)}
            >
              <code>{cmd.usage}</code>
              <span>{$translate(cmd.key)}</span>
            </button>
          {/each}
        </div>
      {/if}
      <textarea
        bind:this={composerEl}
        bind:value={draft}
        rows="2"
        placeholder={sentRunId
          ? $translate('chat.followUpPlaceholder')
          : $translate('runs.composerPlaceholder')}
        title={$translate('chat.pasteImageHint')}
        disabled={!projectId || !selectedThreadId}
        onkeydown={(e) => {
          if (e.key === 'Escape' && showSlashMenu) {
            e.preventDefault();
            slashMenuDismissed = true;
            return;
          }
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            send();
          }
        }}
        onpaste={handlePaste}
      ></textarea>
      <div class="composer-controls">
        <input
          bind:this={fileInputEl}
          type="file"
          accept="image/*"
          multiple
          hidden
          onchange={pickImages}
        />
        <button
          type="button"
          class="composer-tool"
          title={$translate('chat.attachImage')}
          aria-label={$translate('chat.attachImage')}
          disabled={!projectId || !selectedThreadId}
          onclick={() => fileInputEl?.click()}><ImagePlus size={15} /></button
        >
        <button
          type="button"
          class="composer-tool"
          title={$translate('chat.commandsTitle')}
          aria-label={$translate('chat.commandsTitle')}
          disabled={!projectId || !selectedThreadId}
          onclick={openCommands}><TerminalSquare size={15} /></button
        >
        <span class="composer-divider"></span>
        <div class="mode-toggle" role="group" aria-label={$translate('chat.modeLabel')}>
          <button
            type="button"
            class:active={mode === 'do'}
            onclick={() => (mode = 'do')}
            title={$translate('chat.modeDoHint')}>{$translate('chat.modeDo')}</button
          >
          <button
            type="button"
            class:active={mode === 'plan'}
            onclick={() => (mode = 'plan')}
            title={$translate('chat.modePlanHint')}>{$translate('chat.modePlan')}</button
          >
        </div>
        {#if tasks.length > 0}
          <div class="task-select">
            <Select
              bind:value={attachedTaskId}
              label={$translate('chat.attachTask')}
              options={[
                { value: '', label: $translate('chat.noTask') },
                ...tasks.map((task) => {
                  const blocked = isTaskBlocked(tasks, task.id);
                  return {
                    value: task.id,
                    label: task.title,
                    disabled: blocked,
                    hint: blocked ? $translate('chat.taskBlockedOptionHint') : undefined,
                  };
                }),
              ]}
            />
          </div>
        {/if}
        <span class="composer-hint">{$translate('chat.sendHint')}</span>
        <button
          class="primary"
          type="submit"
          disabled={sending ||
            uploadingImage ||
            !draft.trim() ||
            !projectId ||
            !selectedThreadId ||
            attachedTaskBlocked}
          >{sentRunId ? $translate('chat.queueSend') : $translate('runs.send')}</button
        >
      </div>
    </form>
  </article>
</section>

<!-- Full-size view of a chat image. It is a plain overlay rather than a
     component of its own: the whole behaviour is "show this one image big,
     dismiss on anything", and the chat is the only place that needs it. -->
<svelte:window
  onkeydown={(event) => {
    if (event.key === 'Escape' && lightboxSrc) lightboxSrc = null;
  }}
/>
{#if lightboxSrc}
  <div
    class="lightbox"
    role="dialog"
    aria-modal="true"
    aria-label={$translate('chat.openImage')}
    tabindex="-1"
  >
    <!-- The backdrop is the dismiss target, and the button under it is what
         makes that reachable without a mouse. -->
    <button
      type="button"
      class="lightbox-dismiss"
      aria-label={$translate('chat.closeImage')}
      onclick={() => (lightboxSrc = null)}
    ></button>
    <img class="lightbox-image" src={lightboxSrc} alt="" />
    <button
      type="button"
      class="lightbox-close"
      title={$translate('chat.closeImage')}
      aria-label={$translate('chat.closeImage')}
      onclick={() => (lightboxSrc = null)}>×</button
    >
  </div>
{/if}

<style>
  .chat-layout {
    display: grid;
    grid-template-columns: 244px minmax(0, 1fr);
    flex: 1;
    min-height: 0;
    overflow: hidden;
    border: 1px solid var(--line);
    border-radius: var(--r-lg);
    background: linear-gradient(
      180deg,
      color-mix(in srgb, var(--surface) 82%, transparent),
      color-mix(in srgb, var(--surface) 55%, transparent)
    );
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, white 6%, transparent),
      var(--shadow-2);
  }
  .thread-list {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    padding: var(--sp-3);
    border-right: 1px solid var(--line);
    background: color-mix(in srgb, var(--surface-2) 45%, transparent);
  }
  .thread-list-header {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    height: 34px;
    margin-bottom: var(--sp-2);
  }
  .thread-list-header h2 {
    margin: 0;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--fs-2xs);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.12em;
  }
  .new-thread {
    min-height: 26px;
    padding: 0 var(--sp-2);
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--text-dim);
    box-shadow: none;
    font-size: var(--fs-xs);
    font-weight: 500;
  }
  .new-thread:hover:not(:disabled) {
    border-color: color-mix(in srgb, var(--heat-1) 45%, var(--line));
    background: var(--surface-3);
    color: var(--text);
  }
  .thread-items {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .thread-items button {
    display: flex;
    flex-direction: column;
    gap: 3px;
    align-items: flex-start;
    width: 100%;
    padding: var(--sp-2) var(--sp-3);
    border: 0;
    border-radius: var(--r-md);
    background: transparent;
    color: var(--text-dim);
    text-align: left;
    cursor: pointer;
    transition:
      background-color var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease);
  }
  .thread-items button:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .thread-items button.active {
    background: var(--surface-2);
    color: var(--text);
    box-shadow: inset 2px 0 var(--heat-1);
  }
  .thread-items strong {
    align-self: stretch;
    min-width: 0;
    overflow: hidden;
    font-size: var(--fs-sm);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .thread-items time {
    color: var(--muted);
    font-size: var(--fs-2xs);
  }
  .chat-panel {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .chat-toolbar {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-left: auto;
    flex: none;
  }
  .thread-tokens {
    margin: 0;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--fs-2xs);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .token-cache {
    color: var(--muted);
  }
  .lead-select {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    font-size: var(--fs-xs);
    color: var(--muted);
  }
  .lead-select :global(.sf-select) {
    width: auto;
    min-width: 132px;
    max-width: 200px;
  }
  .chat-error {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0;
    padding: 9px 18px;
    color: var(--red);
    font-size: var(--fs-sm);
  }
  .retry-button {
    flex: none;
    padding: 3px 12px;
    border: 1px solid var(--red);
    border-radius: 999px;
    background: transparent;
    color: var(--red);
    font: inherit;
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  .retry-button:hover {
    background: color-mix(in srgb, var(--red) 16%, transparent);
  }
  .mcp-withheld-banner {
    align-self: stretch;
    margin: 0;
    padding: 10px 13px;
    border-radius: var(--r-md);
    border: 1px solid color-mix(in srgb, var(--yellow) 45%, var(--line));
    background: color-mix(in srgb, var(--yellow) 12%, var(--surface));
    color: var(--text);
    font-size: var(--fs-sm);
    line-height: 1.5;
  }
  .mcp-withheld-banner strong {
    color: var(--yellow);
  }
  .studio-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 240px;
    margin-left: auto;
    overflow: hidden;
    padding: 3px 9px;
    border-radius: 999px;
    border: 1px solid var(--line);
    color: var(--muted);
    font-size: var(--fs-xs);
    white-space: nowrap;
    text-overflow: ellipsis;
    transition:
      background-color 200ms ease,
      color 200ms ease,
      border-color 200ms ease;
  }
  .sync-badge {
    margin-left: 0;
  }
  .studio-badge .dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--muted);
    transition: background-color 200ms ease;
  }
  .studio-badge.online {
    color: var(--text);
  }
  .studio-badge.online .dot {
    background: var(--green);
  }
  .studio-badge.warn {
    color: var(--yellow);
    border-color: color-mix(in srgb, var(--yellow) 45%, var(--line));
  }
  .studio-badge.warn .dot {
    background: var(--yellow);
  }
  button.studio-badge {
    background: transparent;
    font: inherit;
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  button.studio-badge:hover:not(:disabled) {
    background: var(--surface-2);
    color: var(--text);
  }
  button.studio-badge:disabled {
    cursor: default;
    opacity: 0.7;
  }
  .command-info {
    margin: 0;
    padding: 6px 18px;
    color: var(--accent);
    font-size: var(--fs-xs);
  }
  .vision-warning {
    margin: 8px 18px 0;
    color: var(--yellow);
    font-size: var(--fs-xs);
    line-height: 1.4;
  }
  .chat-panel {
    position: relative;
  }
  .scroll-down {
    position: absolute;
    right: 18px;
    bottom: 92px;
    width: 34px;
    height: 34px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    border: 1px solid var(--line);
    background: var(--surface-2);
    color: var(--text);
    font-size: var(--fs-xl);
    cursor: pointer;
    box-shadow: var(--shadow-soft);
  }
  .scroll-down:hover {
    background: color-mix(in srgb, var(--accent) 25%, var(--surface-2));
  }
  .message-list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding-block: var(--sp-5) var(--sp-4);
    padding-inline: max(var(--sp-5), calc((100% - 1080px) / 2));
  }
  .chat-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-4);
    flex: 1;
    min-height: 0;
    padding: var(--sp-6) var(--sp-4);
    text-align: center;
  }
  .chat-empty-mark {
    display: grid;
    place-items: center;
    width: 56px;
    height: 56px;
    border: 1px solid color-mix(in srgb, var(--heat-1) 30%, var(--line));
    border-radius: var(--r-xl);
    background: radial-gradient(
      circle at 50% 120%,
      color-mix(in srgb, var(--heat-2) 30%, transparent),
      var(--surface-2) 70%
    );
    color: var(--heat-1);
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, white 10%, transparent),
      0 0 28px -10px color-mix(in srgb, var(--heat-1) 80%, transparent);
  }
  .chat-empty-lead {
    max-width: 46ch;
    margin: 0;
    color: var(--text-dim);
    font-size: var(--fs-lg);
    line-height: 1.5;
  }
  .chat-empty-commands {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    align-items: center;
    width: 100%;
    max-width: 720px;
    margin-top: var(--sp-2);
  }
  .chat-empty-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
    gap: var(--sp-2);
    width: 100%;
  }
  .chat-empty-command {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: var(--sp-3);
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    background: var(--surface);
    color: var(--text-dim);
    text-align: left;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease),
      background-color var(--dur-fast) var(--ease),
      transform var(--dur-fast) var(--ease);
  }
  .chat-empty-command:hover:not(:disabled) {
    transform: translateY(-1px);
    border-color: color-mix(in srgb, var(--heat-1) 45%, var(--line));
    background: var(--surface-2);
  }
  .chat-empty-command code {
    color: var(--heat-1);
    font-size: var(--fs-xs);
  }
  .chat-empty-command span {
    color: var(--muted);
    font-size: var(--fs-xs);
    line-height: 1.4;
  }
  .bubble {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    max-width: 780px;
  }
  .bubble p {
    margin: 0;
    font-size: var(--fs-md);
    line-height: 1.6;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .message-images {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
  }
  .message-image-button {
    padding: 0;
    border: 0;
    background: none;
    cursor: zoom-in;
    line-height: 0;
    border-radius: var(--r-md);
  }
  .message-image-button:focus-visible {
    outline: 2px solid var(--accent, currentColor);
    outline-offset: 2px;
  }
  .message-image {
    max-width: 220px;
    max-height: 160px;
    border-radius: var(--r-md);
    border: 1px solid var(--line);
    object-fit: cover;
    display: block;
  }
  .lightbox {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    place-items: center;
    padding: 24px;
    background: rgba(0, 0, 0, 0.82);
  }
  /* Covers the whole overlay so a click anywhere outside the image dismisses
     it, while still being a real button for keyboard and screen readers. */
  .lightbox-dismiss {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: none;
    cursor: zoom-out;
  }
  .lightbox-image {
    position: relative;
    max-width: 100%;
    /* Full height minus the padding above, so a tall capture still fits
       without the overlay scrolling. */
    max-height: calc(100vh - 48px);
    object-fit: contain;
    border-radius: var(--r-md);
    box-shadow: 0 8px 40px rgba(0, 0, 0, 0.5);
  }
  .lightbox-close {
    position: absolute;
    top: 12px;
    right: 16px;
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    font-size: 24px;
    line-height: 1;
    color: #fff;
    background: rgba(0, 0, 0, 0.45);
    border: 1px solid rgba(255, 255, 255, 0.25);
    border-radius: 50%;
    cursor: pointer;
  }
  .lightbox-close:hover {
    background: rgba(0, 0, 0, 0.7);
  }
  .bubble-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--sp-2);
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--fs-2xs);
  }
  .bubble-label {
    color: var(--text-dim);
    font-weight: 500;
    letter-spacing: 0.1em;
    text-transform: uppercase;
  }
  .model-identity {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    max-width: min(100%, 430px);
    color: var(--muted);
  }
  .model-identity :global(svg) {
    flex: none;
    opacity: 0.7;
  }
  .model-identity code {
    min-width: 0;
    overflow: hidden;
    color: var(--muted);
    font-size: var(--fs-2xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bubble-user {
    align-self: flex-end;
    align-items: flex-end;
    max-width: min(68%, 620px);
    padding: var(--sp-3) var(--sp-4);
    border: 1px solid color-mix(in srgb, var(--heat-1) 22%, var(--line));
    border-radius: var(--r-lg);
    background: var(--surface-2);
  }
  .bubble-agent {
    position: relative;
    align-self: flex-start;
    width: 100%;
    padding-left: var(--sp-4);
    border-left: 2px solid var(--line);
    transition: border-color var(--dur-cool) var(--ease);
  }
  .bubble-agent.bubble-completed {
    border-left-color: color-mix(in srgb, var(--ok) 55%, var(--line));
  }
  .bubble-agent.bubble-failed {
    border-left-color: var(--danger);
  }
  .bubble-live {
    border-left-color: var(--heat-1);
  }
  .bubble-live::before {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: -2px;
    width: 2px;
    background: var(--heat-1);
    box-shadow: var(--glow-heat);
  }
  .bubble-live .bubble-label::after {
    content: '▊';
    margin-left: 3px;
    color: var(--heat-1);
    animation: caret-blink 1.1s steps(1) infinite;
  }
  @keyframes caret-blink {
    0%,
    50% {
      opacity: 1;
    }
    50.01%,
    100% {
      opacity: 0;
    }
  }
  .question-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 8px;
    padding: 10px;
    border-radius: var(--r-md);
    border: 1px solid var(--line);
    background: color-mix(in srgb, var(--accent) 8%, var(--surface));
  }
  /* No longer the latest turn in the thread (or a send is already in flight):
     the options are kept visible for context but cannot be clicked, so a
     stale question never sends an out-of-context answer. */
  .question-card.question-card-static {
    background: var(--surface);
    opacity: 0.85;
  }
  .question-text {
    margin: 0;
    font-size: var(--fs-md);
    line-height: 1.5;
  }
  .question-options {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .question-option {
    display: flex;
    flex-direction: column;
    gap: 2px;
    align-items: flex-start;
    padding: 7px 10px;
    border-radius: var(--r-sm);
    border: 1px solid var(--line);
    background: var(--surface-2);
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .question-option:hover:not(:disabled) {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 18%, var(--surface-2));
  }
  .question-option:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .question-option-label {
    font-size: var(--fs-sm);
    font-weight: 600;
  }
  .question-option-desc {
    font-size: var(--fs-xs);
    color: var(--muted);
  }
  /* A stuck-escalation card is the scheduler stepping in, not the agent
     asking its own question — a distinct border keeps that legible at a
     glance, and its Stop option borrows the same red treatment as the
     chat-error retry button so "end this run" reads as visually different
     from "just another answer". */
  .stuck-question-card {
    border-color: color-mix(in srgb, var(--red) 35%, var(--line));
  }
  .stuck-stop-option {
    border-color: var(--red);
    color: var(--red);
  }
  .stuck-stop-option:hover:not(:disabled) {
    background: color-mix(in srgb, var(--red) 16%, var(--surface-2));
    border-color: var(--red);
  }
  .progress-strip {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: var(--sp-2);
    margin: 0 var(--sp-5);
    padding: var(--sp-3) 0 0;
    border-top: 1px solid var(--line-soft);
  }
  .progress-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  /* Stop sits beside the elapsed text rather than near Send: it acts on the run
     in flight, and a control that ends work should not sit where the control
     that starts it is reached for. */
  .stop-button {
    flex: none;
    padding: 3px 12px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--surface-2);
    color: var(--text);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .stop-button:hover:not(:disabled) {
    border-color: var(--danger);
    color: var(--danger);
  }
  .stop-button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .progress-track {
    position: relative;
    width: 100%;
    height: 5px;
    border-radius: 99px;
    overflow: hidden;
    background: var(--surface-2);
    box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.25);
  }
  .progress-fill {
    height: 100%;
    border-radius: inherit;
    background: linear-gradient(
      90deg,
      var(--heat-3),
      var(--heat-2) 40%,
      var(--heat-1) 75%,
      var(--heat-0)
    );
    box-shadow: 0 0 10px color-mix(in srgb, var(--heat-1) 55%, transparent);
    transition: width var(--dur-slow) var(--ease);
  }
  .progress-fill.indeterminate {
    position: absolute;
    inset: 0;
    width: 42% !important;
    background: linear-gradient(
      90deg,
      transparent,
      var(--heat-2) 30%,
      var(--heat-0) 50%,
      var(--heat-2) 70%,
      transparent
    );
    animation: progress-slide 1.4s ease-in-out infinite;
  }
  @keyframes progress-slide {
    0% {
      transform: translateX(-100%);
    }
    100% {
      transform: translateX(250%);
    }
  }
  .progress-text-block {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .progress-text {
    margin: 0;
    font-size: var(--fs-xs);
    color: var(--muted);
  }
  .queue-panel {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: 8px;
    padding: 10px 18px 12px;
    border-top: 1px solid var(--line);
    background: color-mix(in srgb, var(--accent) 5%, var(--surface));
  }
  .queue-heading > div {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .queue-heading strong {
    flex: none;
    color: var(--text);
    font-size: var(--fs-xs);
    font-weight: 600;
  }
  .queue-heading span {
    min-width: 0;
    color: var(--muted);
    font-size: var(--fs-2xs);
  }
  .queue-list {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .queue-item {
    display: grid;
    grid-template-columns: 20px minmax(0, 1fr) 24px;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 4px 5px 4px 7px;
    border: 1px solid color-mix(in srgb, var(--accent) 18%, var(--line));
    border-radius: var(--r-md);
    background: var(--surface-2);
  }
  .queue-position {
    display: inline-grid;
    width: 20px;
    height: 20px;
    place-items: center;
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 18%, transparent);
    color: var(--text);
    font-size: var(--fs-2xs);
    font-variant-numeric: tabular-nums;
  }
  .queue-prompt {
    min-width: 0;
    overflow: hidden;
    color: var(--muted);
    font-size: var(--fs-xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .queue-cancel {
    display: inline-grid;
    width: 24px;
    height: 24px;
    padding: 0;
    place-items: center;
    border: 0;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .queue-cancel:hover:not(:disabled) {
    background: color-mix(in srgb, var(--red) 12%, transparent);
    color: var(--red);
  }
  .queue-cancel:disabled {
    opacity: 0.5;
    cursor: default;
  }
  /* Cache sits below spend, smaller again — the same "second line, quieter"
     treatment as the chat header's thread total. */
  .progress-cache {
    font-size: var(--fs-2xs);
    color: var(--muted);
  }
  .diff-panel {
    margin: 10px 18px 0;
  }
  .diff-panel summary {
    color: var(--muted);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .diff-panel summary:hover {
    color: var(--text);
  }
  .diff-muted {
    margin: 10px 18px 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .diff-studio-notice {
    margin: 10px 18px 0;
    padding: 8px 12px;
    border: 1px solid var(--warning);
    border-radius: var(--r-md);
    background: color-mix(in srgb, var(--warning) 12%, transparent);
    color: var(--warning);
    font-size: var(--fs-xs);
    line-height: 1.45;
  }
  .live-diff-card {
    align-self: flex-start;
    width: 100%;
    max-width: 780px;
    margin-top: 4px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    background: var(--surface-2);
  }
  .live-diff-heading {
    margin: 0 0 6px;
    color: var(--text-dim);
    font-size: var(--fs-2xs);
    font-weight: 500;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .diff-range-panel {
    margin-top: 14px;
  }
  .diff-range-body {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-top: 8px;
  }
  .diff-range-subtitle {
    margin: 0 0 6px;
    color: var(--text-dim);
    font-size: var(--fs-xs);
    font-weight: 500;
  }
  .diff-range-picker {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 10px;
    margin-bottom: 8px;
  }
  .diff-range-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 220px;
    color: var(--muted);
    font-size: var(--fs-2xs);
  }
  .diff-range-apply {
    height: 30px;
    padding: 0 14px;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-3);
    color: var(--text);
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  .diff-range-apply:hover:not(:disabled) {
    border-color: var(--text-dim);
  }
  .diff-range-apply:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .rollback-row {
    margin: 8px 18px 0;
  }
  .selective-rollback-row {
    margin-top: 6px;
  }
  .rollback-button {
    padding: 3px 12px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--surface-2);
    color: var(--muted);
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  .rollback-button:hover {
    border-color: var(--danger);
    color: var(--danger);
  }
  .rollback-confirm {
    max-width: 480px;
    padding: 10px 12px;
    border: 1px solid var(--danger);
    border-radius: var(--r-md);
    background: var(--surface-2);
  }
  .rollback-confirm-text {
    margin: 0;
    color: var(--text);
    font-size: var(--fs-sm);
  }
  .rollback-confirm-text code {
    font-family: 'Cascadia Code', Consolas, monospace;
  }
  .rollback-explain {
    margin: 6px 0 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .rollback-studio-notice {
    margin: 6px 0 0;
    color: var(--warning);
    font-size: var(--fs-xs);
  }
  .rollback-error {
    margin: 6px 0 0;
    color: var(--danger);
    font-size: var(--fs-xs);
  }
  .rollback-success {
    margin: 0;
    color: var(--text);
    font-size: var(--fs-sm);
  }
  .rollback-actions {
    display: flex;
    gap: 8px;
    margin-top: 10px;
  }
  .rollback-confirm-button {
    border-color: var(--danger);
    color: var(--danger);
  }
  .attached-task-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    width: fit-content;
    margin: 10px 18px 0;
    padding: 5px 10px;
    border-radius: 999px;
    border: 1px solid var(--line);
    background: color-mix(in srgb, var(--accent) 16%, var(--surface-2));
    color: var(--text);
    font-size: var(--fs-xs);
  }
  .attached-task-chip.blocked {
    border-color: var(--warning);
    background: color-mix(in srgb, var(--warning) 16%, var(--surface-2));
    color: var(--warning);
  }
  .task-blocked-hint {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 8px 18px 0;
    padding: 8px 12px;
    border: 1px solid var(--warning);
    border-radius: var(--r-md);
    background: color-mix(in srgb, var(--warning) 12%, transparent);
    color: var(--warning);
    font-size: var(--fs-xs);
    line-height: 1.45;
  }
  .chip-clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    width: 14px;
    height: 14px;
    border: 0;
    border-radius: 50%;
    background: transparent;
    color: var(--muted);
    font-size: var(--fs-md);
    line-height: 1;
    cursor: pointer;
  }
  .chip-clear:hover {
    color: var(--text);
  }
  .attachment-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 10px 18px 0;
  }
  .attachment-chip {
    position: relative;
    width: 48px;
    height: 48px;
  }
  .attachment-thumb {
    width: 100%;
    height: 100%;
    border-radius: var(--r-md);
    border: 1px solid var(--line);
    object-fit: cover;
  }
  .attachment-chip .chip-clear {
    position: absolute;
    top: -6px;
    right: -6px;
    width: 16px;
    height: 16px;
    background: var(--surface);
    border: 1px solid var(--line);
  }
  .attachment-uploading {
    font-size: var(--fs-xs);
    color: var(--muted);
  }
  .composer {
    position: relative;
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    width: 100%;
    max-width: 1080px;
    margin: var(--sp-2) auto var(--sp-3);
    padding: var(--sp-3) var(--sp-3) var(--sp-2);
    border: 1px solid var(--line);
    border-radius: var(--r-lg);
    background: linear-gradient(180deg, var(--surface-2), var(--surface));
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, white 7%, transparent),
      var(--shadow-2);
    transition:
      border-color var(--dur-base) var(--ease),
      box-shadow var(--dur-base) var(--ease);
  }
  .composer::before {
    content: '';
    position: absolute;
    inset: -1px;
    border-radius: inherit;
    padding: 1px;
    background: linear-gradient(
      120deg,
      transparent 20%,
      color-mix(in srgb, var(--heat-1) 55%, transparent),
      transparent 80%
    );
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--dur-base) var(--ease);
    -webkit-mask:
      linear-gradient(#000 0 0) content-box,
      linear-gradient(#000 0 0);
    mask:
      linear-gradient(#000 0 0) content-box,
      linear-gradient(#000 0 0);
    -webkit-mask-composite: xor;
    mask-composite: exclude;
  }
  .composer:focus-within {
    border-color: color-mix(in srgb, var(--heat-1) 40%, var(--line));
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, white 8%, transparent),
      var(--shadow-2),
      0 0 24px -8px color-mix(in srgb, var(--heat-1) 60%, transparent);
  }
  .composer:focus-within::before {
    opacity: 1;
  }
  .composer-controls {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .composer-tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    flex: none;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--text-dim);
    cursor: pointer;
    transition:
      background-color var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease);
  }
  .composer-tool:hover:not(:disabled) {
    border-color: var(--line);
    background: var(--surface-3);
    color: var(--heat-1);
  }
  .composer-divider {
    width: 1px;
    height: 18px;
    flex: none;
    background: var(--line);
  }
  .composer-hint {
    margin-left: auto;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--fs-2xs);
    white-space: nowrap;
  }
  .composer-controls > button[type='submit'] {
    flex: none;
  }
  @media (max-width: 900px) {
    .composer-hint {
      display: none;
    }
    .composer-controls > button[type='submit'] {
      margin-left: auto;
    }
  }
  .slash-menu {
    position: absolute;
    bottom: 100%;
    left: 12px;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: min(360px, calc(100% - 24px));
    margin-bottom: 6px;
    padding: 6px;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    background: var(--surface-2);
    box-shadow: var(--shadow-soft);
    animation: slash-menu-in 120ms ease;
  }
  @keyframes slash-menu-in {
    from {
      opacity: 0;
      transform: translateY(2px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .slash-menu-title {
    margin: 2px 6px 4px;
    color: var(--muted);
    font-size: var(--fs-2xs);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .slash-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border: 0;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .slash-menu-item:hover {
    background: var(--surface);
  }
  .slash-menu-item code {
    flex: none;
    color: var(--accent);
    font-size: var(--fs-xs);
  }
  .slash-menu-item span {
    min-width: 0;
    overflow: hidden;
    color: var(--muted);
    font-size: var(--fs-xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .task-select {
    flex: none;
    color: var(--muted);
  }
  .task-select :global(.sf-select) {
    width: auto;
    min-width: 120px;
    max-width: 200px;
    min-height: 28px;
    font-size: var(--fs-xs);
  }
  .composer textarea {
    width: 100%;
    min-height: 3rem;
    max-height: 38vh;
    padding: 0;
    border: 0;
    outline: 0;
    resize: none;
    background: transparent;
    color: inherit;
    font: inherit;
    font-size: var(--fs-md);
    line-height: 1.55;
  }
  .composer textarea::placeholder {
    color: var(--muted);
  }
  .composer textarea:focus-visible {
    outline: none;
    box-shadow: none;
  }
  .mode-toggle {
    display: inline-flex;
    flex: none;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    overflow: hidden;
  }
  .mode-toggle button {
    height: 26px;
    padding: 0 var(--sp-3);
    border: 0;
    background: var(--surface-2);
    color: var(--muted);
    font: inherit;
    font-size: var(--fs-xs);
    font-weight: 500;
    cursor: pointer;
  }
  .mode-toggle button + button {
    border-left: 1px solid var(--line);
  }
  .mode-toggle button.active {
    background: color-mix(in srgb, var(--heat-1) 22%, var(--surface-2));
    color: var(--text);
    font-weight: 600;
  }
  @media (max-width: 780px) {
    .chat-layout {
      grid-template-columns: 1fr;
      grid-template-rows: auto minmax(0, 1fr);
    }
    .thread-list {
      max-height: 220px;
      border-right: 0;
      border-bottom: 1px solid var(--line);
    }
    .queue-heading > div {
      align-items: flex-start;
      flex-direction: column;
      gap: 2px;
    }
  }
</style>
