<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { Cpu, MessagesSquare, Plus } from '@lucide/svelte';
  import {
    APIError,
    attachmentUrl,
    createTask,
    createThread,
    getLead,
    getPace,
    getRunDiff,
    getStudioStatus,
    getThreadMessages,
    getThreads,
    post,
    rollbackRun,
    setLead,
    startSync,
    stopSync,
    uploadAttachment,
  } from '$lib/api';
  import { getOpenRouterCapabilities } from '$lib/openrouter';
  import { getNVIDIACapabilities } from '$lib/nvidia';
  import { aggregateOpenRouterMessages } from '$lib/openrouterStream';
  import { parseAttachments } from '$lib/attachments';
  import Markdown from '$lib/components/Markdown.svelte';
  import { foregroundRun, liveThreadRuns, queuedBehindForeground } from '$lib/runQueue';
  import { endsRun, mcpWithheldMessage } from '$lib/runStatus';
  import {
    extractQuestionFence,
    isStuckEscalation,
    normalizeQuestionPayload,
    shouldAnswerQuestion,
  } from '$lib/questionCard';
  import { isStaleGeneration } from '$lib/staleness';
  import { cacheTokens, formatDate, formatTokens, locale, spendTokens, translate } from '$lib/i18n';
  import type {
    Agent,
    ChatMessage,
    ChatThread,
    Project,
    Run,
    RunDiff,
    RunEvent,
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
  let runDiff: RunDiff | null = null;
  let loadingDiff = false;
  let confirmingRollback = false;
  let rollingBack = false;
  let rollbackError = '';
  let rollbackResult: { branch: string; commitHash: string } | null = null;
  let loadedProjectId: string | undefined;
  // Bumped once per project switch. Every async load below captures it before
  // its await and only applies the result if it is still current afterward —
  // guards against a slow/reordered response from project A landing in the UI
  // after project B is already selected.
  let projectGeneration = 0;
  let leadAgentId = '';
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
  } else if (!projectId && loadedProjectId) {
    loadedProjectId = undefined;
    projectGeneration += 1;
    threads = [];
    selectedThreadId = '';
    messages = [];
    submittedRuns = [];
    restoreActiveRun('');
    leadAgentId = '';
    typicalSeconds = 0;
    paceSamples = 0;
    studioStatus = STUDIO_OFFLINE;
    attachedTaskId = '';
    clearPendingAttachments();
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
  // A follow-up must not steal the live transcript or Stop button from the
  // run that is actually executing. It becomes foreground only after the
  // predecessor reaches a terminal state (from snapshot or SSE).
  $: if ((currentForegroundRun?.id ?? null) !== sentRunId) {
    sentRunId = currentForegroundRun?.id ?? null;
    sendStartMs = currentForegroundRun ? Date.parse(currentForegroundRun.createdAt) : 0;
    stopping = false;
  }
  $: activeRunEvents = sentRunId ? liveEvents.filter((event) => event.runId === sentRunId) : [];
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
    loadingDiff = false;
    resetRollbackState();
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
    restoreActiveRun(id);
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
  }

  async function loadRunDiff(runId: string) {
    loadingDiff = true;
    resetRollbackState();
    try {
      runDiff = await getRunDiff(runId);
    } catch (cause) {
      if (cause instanceof APIError) runDiff = null;
    } finally {
      loadingDiff = false;
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
      loadingDiff = false;
      resetRollbackState();
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
      resetRollbackState();
      onSent(run.id);
    } catch (cause) {
      // The send failed: drop the optimistic bubble and give the text and
      // attachments back.
      messages = messages.filter((m) => m !== optimistic);
      draft = prompt;
      pendingAttachments = attachments;
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      sending = false;
    }
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
    <p class="eyebrow">{$translate('nav.chat')}</p>
    <h1>{selectedThread?.title ?? $translate('chat.threadsTitle')}</h1>
  </div>
</section>
<section class="chat-layout">
  <div class="thread-list">
    <div class="thread-list-header">
      <h2>{$translate('chat.threadsTitle')}</h2>
      <button
        class="primary new-thread"
        onclick={newThread}
        disabled={!projectId || creatingThread}
      >
        <Plus size={15} />{$translate('chat.newThread')}
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
            <strong>{thread.title}</strong>
            <time>{formatDate(thread.updatedAt, $locale)}</time>
          </button>
        {:else}
          <div class="empty"><p>{$translate('chat.empty')}</p></div>
        {/each}
      {/if}
    </div>
  </div>
  <article class="chat-panel">
    <header>
      <div class="chat-title-block">
        <h2>{selectedThread?.title ?? $translate('chat.threadsTitle')}</h2>
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
      {#if studioActionable}
        <button
          type="button"
          class="studio-badge"
          class:warn={studioStatus.state === 'other'}
          disabled={openingStudio}
          title={$translate('chat.studioOpen')}
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
          title={studioHint}
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
          title={syncHint}
          aria-label={syncActive ? $translate('chat.syncStop') : $translate('chat.syncStart')}
          onclick={toggleSync}
        >
          <span class="dot"></span>{syncLabel}
        </button>
      {/if}
      {#if agents.length > 0}
        <label class="lead-select">
          <span>{$translate('chat.lead')}</span>
          <select
            value={leadKnown ? leadAgentId : ''}
            disabled={!projectId}
            onchange={(e) => changeLead(e.currentTarget.value)}
          >
            {#if !leadKnown}
              <option value="" disabled selected>{$translate('common.none')}</option>
            {/if}
            {#each agents as agent (agent.id)}
              <option value={agent.id}>{agent.name}</option>
            {/each}
          </select>
        </label>
      {/if}
    </header>
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
        <div class="empty">
          <MessagesSquare size={28} />
          <p>{$translate('chat.empty')}</p>
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
          <div class={`bubble bubble-${message.role}`}>
            <div class="bubble-meta">
              <span class="bubble-label"
                >{message.role === 'user' ? $translate('chat.you') : $translate('chat.agent')}</span
              >
              {#if message.role === 'agent' && message.status}
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
                  <img
                    class="message-image"
                    src={attachmentUrl(projectId, image)}
                    alt=""
                    loading="lazy"
                  />
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
                <Markdown source={messageText(event.payload)} />
              {/if}
            </div>
          {/if}
        {/each}
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
    {#if loadingDiff}
      <p class="diff-muted">{$translate('common.loading')}</p>
    {:else if runDiff}
      {#if runDiff.diff.trim() !== '' && !runDiff.note}
        <details class="diff-panel">
          <summary>{$translate('chat.diffChangedFiles')}</summary>
          <pre class="diff-pre">{runDiff.diff}</pre>
        </details>
      {:else}
        <p class="diff-muted">{runDiff.note || $translate('chat.diffNoChanges')}</p>
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
      {/if}
    {/if}
    {#if attachedTask}
      <div class="attached-task-chip">
        <span>{attachedTask.title}</span>
        <button
          type="button"
          class="chip-clear"
          onclick={() => (attachedTaskId = '')}
          aria-label={$translate('chat.noTask')}>×</button
        >
      </div>
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
        <label class="task-select">
          <span>{$translate('chat.attachTask')}</span>
          <select bind:value={attachedTaskId}>
            <option value="">{$translate('chat.noTask')}</option>
            {#each tasks as task (task.id)}
              <option value={task.id}>{task.title}</option>
            {/each}
          </select>
        </label>
      {/if}
      <textarea
        bind:value={draft}
        rows="2"
        placeholder={sentRunId
          ? $translate('chat.followUpPlaceholder')
          : $translate('runs.composerPlaceholder')}
        title={$translate('chat.pasteImageHint')}
        disabled={!projectId || !selectedThreadId}
        onkeydown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            send();
          }
        }}
        onpaste={handlePaste}
      ></textarea>
      <button
        class="primary"
        type="submit"
        disabled={sending || uploadingImage || !draft.trim() || !projectId || !selectedThreadId}
        >{sentRunId ? $translate('chat.queueSend') : $translate('runs.send')}</button
      >
    </form>
  </article>
</section>

<style>
  .chat-layout {
    display: grid;
    grid-template-columns: 280px minmax(0, 1fr);
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 11px;
    overflow: hidden;
    background: var(--surface);
  }
  .thread-list {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    border-right: 1px solid var(--line);
  }
  .thread-list-header {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: 9px;
    padding: 13px;
    border-bottom: 1px solid var(--line);
  }
  .thread-list-header h2 {
    margin: 0;
    font-size: 0.78rem;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.07em;
  }
  .new-thread {
    padding: 8px 10px;
    font-size: 0.78rem;
  }
  .thread-items {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .thread-items button {
    display: flex;
    flex-direction: column;
    gap: 4px;
    align-items: flex-start;
    width: 100%;
    padding: 12px 13px;
    border: 0;
    border-bottom: 1px solid var(--line);
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
  }
  .thread-items button:hover,
  .thread-items button.active {
    background: var(--surface-2);
  }
  .thread-items strong {
    align-self: stretch;
    min-width: 0;
    overflow: hidden;
    font-size: 0.82rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .thread-items time {
    color: var(--muted);
    font-size: 0.65rem;
  }
  .chat-panel {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .chat-panel > header {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 15px 18px;
    border-bottom: 1px solid var(--line);
  }
  .chat-panel > header h2 {
    margin: 0;
    font-size: 0.95rem;
  }
  .chat-title-block {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  /* The thread's lifetime spend, quieter than the title above it — a passive
     readout, not something that competes for attention on every render. */
  .thread-tokens {
    margin: 0;
    font-size: 0.68rem;
    color: var(--muted);
  }
  .token-cache {
    opacity: 0.75;
  }
  .lead-select {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    font-size: 0.72rem;
    color: var(--muted);
  }
  .lead-select select {
    padding: 5px 8px;
    border-radius: 0.4rem;
    border: 1px solid var(--line);
    background: var(--surface-2);
    color: var(--text);
    font: inherit;
    font-size: 0.78rem;
  }
  .chat-error {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0;
    padding: 9px 18px;
    color: var(--red);
    font-size: 0.78rem;
  }
  .retry-button {
    flex: none;
    padding: 3px 12px;
    border: 1px solid var(--red);
    border-radius: 999px;
    background: transparent;
    color: var(--red);
    font: inherit;
    font-size: 0.72rem;
    cursor: pointer;
  }
  .retry-button:hover {
    background: color-mix(in srgb, var(--red) 16%, transparent);
  }
  .mcp-withheld-banner {
    align-self: stretch;
    margin: 0;
    padding: 10px 13px;
    border-radius: 9px;
    border: 1px solid color-mix(in srgb, var(--yellow) 45%, var(--line));
    background: color-mix(in srgb, var(--yellow) 12%, var(--surface));
    color: var(--text);
    font-size: 0.8rem;
    line-height: 1.5;
  }
  .mcp-withheld-banner strong {
    color: var(--yellow);
  }
  .studio-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
    padding: 3px 9px;
    border-radius: 999px;
    border: 1px solid var(--line);
    color: var(--muted);
    font-size: 0.72rem;
  }
  .studio-badge .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--muted);
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
    font-size: 0.72rem;
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
    font-size: 0.74rem;
  }
  .vision-warning {
    margin: 8px 18px 0;
    color: var(--yellow);
    font-size: 0.72rem;
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
    font-size: 1.05rem;
    cursor: pointer;
    box-shadow: var(--shadow-soft);
  }
  .scroll-down:hover {
    background: color-mix(in srgb, var(--accent) 25%, var(--surface-2));
  }
  .message-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 16px 18px;
  }
  .bubble {
    max-width: 72%;
    padding: 10px 13px;
    border-radius: 12px;
    border: 1px solid var(--line);
    background: var(--surface-2);
  }
  .bubble p {
    margin: 5px 0 0;
    font-size: 0.85rem;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .message-images {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
  }
  .message-image {
    max-width: 220px;
    max-height: 160px;
    border-radius: 8px;
    border: 1px solid var(--line);
    object-fit: cover;
  }
  .bubble-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 0.68rem;
    color: var(--muted);
  }
  .bubble-label {
    font-weight: 700;
    color: var(--text);
  }
  .model-identity {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    max-width: min(100%, 430px);
    padding: 3px 7px;
    border: 1px solid color-mix(in srgb, var(--accent) 28%, var(--line));
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 8%, var(--surface));
    color: var(--text);
    line-height: 1;
  }
  .model-identity :global(svg) {
    flex: none;
    color: var(--accent);
  }
  .model-identity code {
    min-width: 0;
    overflow: hidden;
    color: var(--muted);
    font-size: 0.64rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bubble-user {
    align-self: flex-end;
    background: linear-gradient(
      145deg,
      color-mix(in srgb, var(--accent) 20%, var(--surface-2)),
      var(--surface-2)
    );
  }
  .bubble-agent {
    align-self: flex-start;
  }
  .bubble-live {
    border-style: dashed;
    opacity: 0.85;
  }
  .question-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 8px;
    padding: 10px;
    border-radius: 9px;
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
    font-size: 0.85rem;
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
    border-radius: 7px;
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
    font-size: 0.8rem;
    font-weight: 600;
  }
  .question-option-desc {
    font-size: 0.7rem;
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
    gap: 5px;
    padding: 10px 18px;
    border-top: 1px solid var(--line);
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
    font-size: 0.78rem;
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
    border-radius: 3px;
    overflow: hidden;
    background: var(--surface-2);
  }
  .progress-fill {
    height: 100%;
    border-radius: 3px;
    background: var(--accent);
    transition: width 0.4s ease;
  }
  .progress-fill.indeterminate {
    position: absolute;
    inset: 0;
    width: 40% !important;
    background: linear-gradient(90deg, transparent, var(--accent), transparent);
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
    font-size: 0.72rem;
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
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }
  .queue-heading strong {
    flex: none;
    color: var(--text);
    font-size: 0.74rem;
    font-weight: 650;
  }
  .queue-heading span {
    min-width: 0;
    color: var(--muted);
    font-size: 0.68rem;
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
    border-radius: 8px;
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
    font-size: 0.66rem;
    font-variant-numeric: tabular-nums;
  }
  .queue-prompt {
    min-width: 0;
    overflow: hidden;
    color: var(--muted);
    font-size: 0.72rem;
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
    border-radius: 6px;
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
    font-size: 0.66rem;
    opacity: 0.75;
  }
  .diff-panel {
    margin: 10px 18px 0;
  }
  .diff-panel summary {
    color: var(--muted);
    font-size: 0.78rem;
    cursor: pointer;
  }
  .diff-panel summary:hover {
    color: var(--text);
  }
  .diff-pre {
    margin: 8px 0 0;
    max-height: 260px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface-2);
    color: var(--text);
    font-family: 'Cascadia Code', Consolas, monospace;
    font-size: 0.72rem;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    overflow-y: auto;
  }
  .diff-muted {
    margin: 10px 18px 0;
    color: var(--muted);
    font-size: 0.72rem;
  }
  .rollback-row {
    margin: 8px 18px 0;
  }
  .rollback-button {
    padding: 3px 12px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--surface-2);
    color: var(--muted);
    font-size: 0.72rem;
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
    border-radius: 8px;
    background: var(--surface-2);
  }
  .rollback-confirm-text {
    margin: 0;
    color: var(--text);
    font-size: 0.78rem;
  }
  .rollback-confirm-text code {
    font-family: 'Cascadia Code', Consolas, monospace;
  }
  .rollback-explain {
    margin: 6px 0 0;
    color: var(--muted);
    font-size: 0.72rem;
  }
  .rollback-error {
    margin: 6px 0 0;
    color: var(--danger);
    font-size: 0.72rem;
  }
  .rollback-success {
    margin: 0;
    color: var(--text);
    font-size: 0.78rem;
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
    font-size: 0.72rem;
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
    font-size: 0.85rem;
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
    border-radius: 8px;
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
    font-size: 0.72rem;
    color: var(--muted);
  }
  .composer {
    display: flex;
    flex: none;
    gap: 0.5rem;
    align-items: flex-end;
    padding: 0.75rem;
    border-top: 1px solid var(--line);
  }
  .task-select {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: none;
    font-size: 0.68rem;
    color: var(--muted);
  }
  .task-select select {
    padding: 0.5rem 0.5rem;
    border-radius: 0.5rem;
    border: 1px solid var(--line);
    background: var(--surface-2);
    color: var(--text);
    font: inherit;
    font-size: 0.78rem;
    max-width: 160px;
  }
  .composer textarea {
    flex: 1;
    resize: vertical;
    min-height: 2.5rem;
    font: inherit;
    padding: 0.5rem 0.65rem;
    border-radius: 0.5rem;
    border: 1px solid var(--line);
    background: var(--surface-2);
    color: inherit;
  }
  .composer > button {
    white-space: nowrap;
  }
  .mode-toggle {
    display: inline-flex;
    flex: none;
    border: 1px solid var(--line);
    border-radius: 0.5rem;
    overflow: hidden;
  }
  .mode-toggle button {
    padding: 0.5rem 0.7rem;
    border: 0;
    background: var(--surface-2);
    color: var(--muted);
    font: inherit;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .mode-toggle button + button {
    border-left: 1px solid var(--line);
  }
  .mode-toggle button.active {
    background: color-mix(in srgb, var(--accent) 26%, var(--surface-2));
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
