<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { MessagesSquare, Plus } from '@lucide/svelte';
  import {
    APIError,
    createTask,
    createThread,
    getLead,
    getPace,
    getStudioStatus,
    getThreadMessages,
    getThreads,
    post,
    setLead,
  } from '$lib/api';
  import { formatDate, formatTokens, locale, totalTokens, translate } from '$lib/i18n';
  import type {
    Agent,
    ChatMessage,
    ChatThread,
    Run,
    RunEvent,
    StudioStatus,
    Task,
    TokenUsage,
  } from '$lib/types';

  export let projectId: string | undefined;
  export let agentName: (id: string) => string;
  export let statusLabel: (status: string) => string;
  export let liveEvents: RunEvent[] = [];
  export let onSent: (runId: string) => void = () => {};
  export let agents: Agent[] = [];
  export let tasks: Task[] = [];

  const TERMINAL_STATUSES = new Set(['completed', 'failed', 'cancelled']);

  let threads: ChatThread[] = [];
  let selectedThreadId = '';
  let messages: ChatMessage[] = [];
  let loadingThreads = false;
  let loadingMessages = false;
  let sending = false;
  let draft = '';
  let messageListEl: HTMLDivElement;
  let atBottom = true;
  let mode: 'do' | 'plan' = 'do';
  let error = '';
  let sentRunId: string | null = null;
  let loadedProjectId: string | undefined;
  let leadAgentId = '';
  let attachedTaskId = '';
  let sendStartMs = 0;
  let nowMs = Date.now();
  let typicalSeconds = 0;
  let paceSamples = 0;
  const STUDIO_OFFLINE: StudioStatus = { open: 0, matched: 0, state: 'none' };
  const STUDIO_POLL_MS = 10_000;

  let studioStatus: StudioStatus = STUDIO_OFFLINE;
  let openingStudio = false;
  let stopping = false;
  let commandInfo = '';
  let progressInterval: ReturnType<typeof setInterval> | undefined;
  let studioInterval: ReturnType<typeof setInterval> | undefined;

  $: if (projectId && projectId !== loadedProjectId) {
    loadedProjectId = projectId;
    void loadThreads(projectId);
    void loadLead(projectId);
    void loadPace(projectId);
    void loadStudioStatus();
  } else if (!projectId && loadedProjectId) {
    loadedProjectId = undefined;
    threads = [];
    selectedThreadId = '';
    messages = [];
    sentRunId = null;
    leadAgentId = '';
    typicalSeconds = 0;
    paceSamples = 0;
    studioStatus = STUDIO_OFFLINE;
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
  });

  async function loadStudioStatus() {
    try {
      studioStatus = await getStudioStatus(projectId);
    } catch {
      studioStatus = STUDIO_OFFLINE;
    }
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
      else {
        error = cause instanceof Error ? cause.message : String(cause);
        stopping = false;
      }
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
    error = '';
    try {
      await openStudio();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    }
  }

  // Slash commands let the operator act without leaving the composer.
  async function runCommand(text: string): Promise<boolean> {
    const [name, ...rest] = text.slice(1).split(' ');
    const arg = rest.join(' ').trim();
    draft = '';
    error = '';
    try {
      if (name === 'task' && arg && projectId) {
        await createTask(projectId, { title: arg });
        commandInfo = `✓ task: ${arg}`;
      } else if (name === 'plan') {
        mode = 'plan';
        commandInfo = '✓ mode: plan';
      } else if (name === 'do') {
        mode = 'do';
        commandInfo = '✓ mode: do';
      } else if (name === 'open' && projectId) {
        await openStudio();
        commandInfo = '✓ opening Studio…';
      } else if (name === 'build' && arg) {
        await submitRun(
          `As the project orchestrator, plan and build this, delegating to the engineer and QA sub-agents where useful, then hand off a short summary of what changed and what was verified: ${arg}`,
        );
      } else if (name === 'playtest' && arg) {
        await submitRun(
          `Build the change, then in Roblox Studio start Play mode, read the console with get_console_output, fix any errors, stop Play, and report the result: ${arg}`,
        );
      } else {
        commandInfo = 'commands: /task <title>, /build <desc>, /playtest <desc>, /plan, /do, /open';
      }
    } catch (cause) {
      commandInfo = '';
      error = cause instanceof Error ? cause.message : String(cause);
    }
    return true;
  }

  $: elapsedSeconds = sentRunId && sendStartMs ? Math.max(0, (nowMs - sendStartMs) / 1000) : 0;
  $: progressCapped = typicalSeconds <= 0 || elapsedSeconds >= typicalSeconds;
  $: progressPercent =
    typicalSeconds > 0 ? Math.min((elapsedSeconds / typicalSeconds) * 100, 100) : 0;
  $: stepCount = activeRunEvents.length;
  // Usage events carry the run's total so far rather than a delta, so the
  // newest one wins; adding them up would count the same tokens repeatedly.
  $: liveTokens = activeRunEvents.reduce(
    (total, event) =>
      event.type === 'usage' ? totalTokens(event.payload as Partial<TokenUsage>) : total,
    0,
  );
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
  $: leadKnown = !!leadAgentId && agents.some((agent) => agent.id === leadAgentId);
  $: attachedTask = attachedTaskId ? tasks.find((task) => task.id === attachedTaskId) : undefined;
  $: activeRunEvents = sentRunId ? liveEvents.filter((event) => event.runId === sentRunId) : [];
  // Only the agent's own text belongs in the chat — drop system/tool/status/
  // stderr events so the conversation is not buried in machine chatter.
  $: activeLiveEvents = activeRunEvents.filter(
    (event) => event.type === 'message' && messageText(event.payload).trim() !== '',
  );

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

  let handledRunId = '';
  $: if (sentRunId && sentRunId !== handledRunId) {
    const currentRunId = sentRunId;
    const terminal = liveEvents.some(
      (event) =>
        event.runId === currentRunId && event.type === 'status' && isTerminalStatus(event.payload),
    );
    if (terminal) {
      // Reload history first, then drop the live bubbles, so the just-streamed
      // reply never blinks out before its persisted copy arrives.
      handledRunId = currentRunId;
      stopping = false;
      const threadAtSend = selectedThreadId;
      void loadMessages(threadAtSend).then(() => {
        if (sentRunId === currentRunId) sentRunId = null;
      });
    }
  }

  function isTerminalStatus(payload: unknown): boolean {
    if (payload && typeof payload === 'object' && 'status' in payload) {
      const value = (payload as Record<string, unknown>).status;
      return typeof value === 'string' && TERMINAL_STATUSES.has(value);
    }
    return false;
  }

  async function loadThreads(id: string) {
    loadingThreads = true;
    error = '';
    try {
      threads = await getThreads(id);
      sentRunId = null;
      if (threads[0]) {
        await selectThread(threads[0].id);
      } else {
        selectedThreadId = '';
        messages = [];
        atBottom = true;
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loadingThreads = false;
    }
  }

  async function selectThread(id: string) {
    selectedThreadId = id;
    sentRunId = null;
    await loadMessages(id);
    // Opening a thread lands on its newest message regardless of where the
    // previous thread was scrolled to.
    await scrollToBottom();
  }

  async function loadMessages(id: string) {
    if (!id) return;
    loadingMessages = true;
    error = '';
    try {
      messages = await getThreadMessages(id);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loadingMessages = false;
    }
  }

  async function loadLead(id: string) {
    try {
      leadAgentId = await getLead(id);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    }
  }

  async function loadPace(id: string) {
    try {
      const pace = await getPace(id);
      typicalSeconds = pace.typicalSeconds;
      paceSamples = pace.samples;
    } catch {
      typicalSeconds = 0;
      paceSamples = 0;
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
      error = cause instanceof Error ? cause.message : String(cause);
    }
  }

  async function newThread() {
    if (!projectId) return;
    error = '';
    try {
      const thread = await createThread(projectId, '');
      threads = [thread, ...threads];
      selectedThreadId = thread.id;
      messages = [];
      sentRunId = null;
      atBottom = true;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    }
  }

  async function submitRun(prompt: string) {
    if (!projectId || !selectedThreadId || sending) return;
    sending = true;
    error = '';
    commandInfo = '';
    draft = '';
    sendStartMs = Date.now();
    nowMs = sendStartMs;
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
        },
        { 'Idempotency-Key': crypto.randomUUID() },
      );
      sentRunId = run.id;
      onSent(run.id);
    } catch (cause) {
      // The send failed: drop the optimistic bubble and give the text back.
      messages = messages.filter((m) => m !== optimistic);
      draft = prompt;
      error = cause instanceof Error ? cause.message : String(cause);
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
      <button class="primary new-thread" onclick={newThread} disabled={!projectId}>
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
      <h2>{selectedThread?.title ?? $translate('chat.threadsTitle')}</h2>
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
    {#if error}<p class="chat-error">{error}</p>{/if}
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
          <div class={`bubble bubble-${message.role}`}>
            <div class="bubble-meta">
              <span class="bubble-label"
                >{message.role === 'user' ? $translate('chat.you') : $translate('chat.agent')}</span
              >
              {#if message.role === 'agent' && message.status}
                <span class={`status status-${message.status}`}>{statusLabel(message.status)}</span>
              {/if}
              <time>{formatDate(message.at, $locale)}</time>
            </div>
            <p>{message.text}</p>
          </div>
        {/each}
        {#each activeLiveEvents as event (event.id)}
          <div class="bubble bubble-agent bubble-live">
            <div class="bubble-meta">
              <span class="bubble-label"
                >{event.agentId ? agentName(event.agentId) : $translate('chat.agent')}</span
              >
              <time
                >{new Intl.DateTimeFormat($locale, { timeStyle: 'medium' }).format(
                  new Date(event.createdAt),
                )}</time
              >
            </div>
            <p>{messageText(event.payload)}</p>
          </div>
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
          <p class="progress-text">
            {$translate('chat.working')}
            {formatElapsed(elapsedSeconds)} · {stepCount}
            {$translate('chat.steps')}{#if liveTokens > 0}
              · {formatTokens(liveTokens, $locale)}
              {$translate('chat.tokens')}{/if}{#if paceSamples > 0}
              · ~{formatElapsed(typicalSeconds)} {$translate('chat.typical')}{/if}
          </p>
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
        placeholder={$translate('runs.composerPlaceholder')}
        disabled={!projectId || !selectedThreadId}
        onkeydown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            send();
          }
        }}
      ></textarea>
      <button
        class="primary"
        type="submit"
        disabled={sending || !draft.trim() || !projectId || !selectedThreadId}
        >{$translate('runs.send')}</button
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
    font-size: 0.82rem;
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
    margin: 0;
    padding: 9px 18px;
    color: var(--red);
    font-size: 0.78rem;
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
    background: #39d98a;
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
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35);
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
  .bubble-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.68rem;
    color: var(--muted);
  }
  .bubble-label {
    font-weight: 700;
    color: var(--text);
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
    border-color: var(--danger, #d9534f);
    color: var(--danger, #d9534f);
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
  .progress-text {
    margin: 0;
    font-size: 0.72rem;
    color: var(--muted);
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
  }
</style>
