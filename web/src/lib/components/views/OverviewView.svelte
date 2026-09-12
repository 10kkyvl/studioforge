<script lang="ts">
  import {
    Bot,
    CircleDollarSign,
    Gauge,
    GitBranch,
    Pencil,
    Palette,
    Play,
    Plug,
    Save,
    Star,
    Trash2,
    Waypoints,
    X,
  } from '@lucide/svelte';
  import {
    clearMemory,
    deleteMemory,
    getMemory,
    getProjectStyles,
    setProjectStyle,
    updateMemory,
  } from '$lib/api';
  import { formatDate, formatMoney, locale, translate, type TranslationKey } from '$lib/i18n';
  import type { MemoryEntry, Project, Snapshot, StylePack } from '$lib/types';

  export let snapshot: Snapshot;
  export let project: Project | undefined;
  export let busy: string;
  export let onRun: () => void;
  export let onOpenRun: (runId: string) => void = () => {};

  let memory: MemoryEntry[] = [];
  let memoryBusy = false;
  let memoryError = '';
  let editingMemoryId = '';
  let editingContent = '';
  let loadedMemoryProject = '';
  let loadedMemoryRun = '';
  let memoryGeneration = 0;
  let stylePacks: StylePack[] = [];
  let selectedStyle = '';
  let styleBusy = false;
  let styleError = '';
  let styleGeneration = 0;
  let loadedStyleProject = '';

  async function loadStyles(projectId: string, generation: number) {
    styleBusy = true;
    styleError = '';
    try {
      const result = await getProjectStyles(projectId);
      if (generation !== styleGeneration || project?.id !== projectId) return;
      stylePacks = result.styles;
      selectedStyle = result.selected;
    } catch (error) {
      if (generation !== styleGeneration || project?.id !== projectId) return;
      styleError = error instanceof Error ? error.message : String(error);
    } finally {
      if (generation === styleGeneration) styleBusy = false;
    }
  }

  async function chooseStyle(event: Event) {
    const value = (event.currentTarget as HTMLSelectElement).value;
    if (!project || !value || value === selectedStyle || styleBusy) return;
    const projectId = project.id;
    const previous = selectedStyle;
    const generation = styleGeneration;
    styleBusy = true;
    styleError = '';
    selectedStyle = value;
    try {
      await setProjectStyle(projectId, value);
    } catch (error) {
      if (generation === styleGeneration && project?.id === projectId) {
        selectedStyle = previous;
        styleError = error instanceof Error ? error.message : $translate('overview.styleSaveError');
      }
    } finally {
      if (generation === styleGeneration) styleBusy = false;
    }
  }

  async function loadMemory(projectId: string, generation: number) {
    memoryBusy = true;
    memoryError = '';
    try {
      const entries = await getMemory(projectId);
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memory = entries;
    } catch (error) {
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memoryError = error instanceof Error ? error.message : String(error);
    } finally {
      if (generation === memoryGeneration) memoryBusy = false;
    }
  }

  $: memoryProjectId = project?.id ?? '';
  $: styleProjectId = project?.id ?? '';
  $: if (styleProjectId && styleProjectId !== loadedStyleProject) {
    styleGeneration += 1;
    const generation = styleGeneration;
    loadedStyleProject = styleProjectId;
    stylePacks = [];
    selectedStyle = project?.style ?? '';
    styleError = '';
    void loadStyles(styleProjectId, generation);
  }

  function startMemoryEdit(entry: MemoryEntry) {
    editingMemoryId = entry.id;
    editingContent = entry.content;
  }

  async function saveMemory(entry: MemoryEntry) {
    const generation = memoryGeneration;
    const projectId = project?.id;
    memoryBusy = true;
    try {
      const updated = await updateMemory(entry.id, { content: editingContent });
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memory = memory.map((item) =>
        item.id === updated.id ? { ...updated, injected: item.injected } : item,
      );
      editingMemoryId = '';
    } catch (error) {
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memoryError = error instanceof Error ? error.message : String(error);
    } finally {
      if (generation === memoryGeneration) memoryBusy = false;
    }
  }

  async function toggleMemoryPin(entry: MemoryEntry) {
    const generation = memoryGeneration;
    const projectId = project?.id;
    memoryBusy = true;
    try {
      const updated = await updateMemory(entry.id, { pinned: !entry.pinned });
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memory = memory.map((item) =>
        item.id === updated.id ? { ...updated, injected: item.injected } : item,
      );
    } catch (error) {
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memoryError = error instanceof Error ? error.message : String(error);
    } finally {
      if (generation === memoryGeneration) memoryBusy = false;
    }
  }

  async function removeMemory(entry: MemoryEntry) {
    if (!window.confirm($translate('memory.deleteConfirm'))) return;
    const generation = memoryGeneration;
    const projectId = project?.id;
    memoryBusy = true;
    try {
      await deleteMemory(entry.id);
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memory = memory.filter((item) => item.id !== entry.id);
    } catch (error) {
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memoryError = error instanceof Error ? error.message : String(error);
    } finally {
      if (generation === memoryGeneration) memoryBusy = false;
    }
  }

  async function removeAllMemory() {
    if (
      !project ||
      !window.confirm($translate('memory.clearConfirm').replace('{count}', String(memory.length)))
    )
      return;
    const generation = memoryGeneration;
    const projectId = project.id;
    memoryBusy = true;
    try {
      await clearMemory(projectId);
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memory = [];
    } catch (error) {
      if (generation !== memoryGeneration || project?.id !== projectId) return;
      memoryError = error instanceof Error ? error.message : String(error);
    } finally {
      if (generation === memoryGeneration) memoryBusy = false;
    }
  }

  $: studioMcpCheck = snapshot.diagnostics.dependencies.studioMcp;
  $: lastRun = project
    ? [...snapshot.runs]
        .filter((run) => run.projectId === project?.id)
        .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())[0]
    : undefined;
  $: latestRunKey = lastRun ? `${lastRun.id}:${lastRun.updatedAt}` : '';
  $: if (
    memoryProjectId &&
    (memoryProjectId !== loadedMemoryProject || latestRunKey !== loadedMemoryRun)
  ) {
    memoryGeneration += 1;
    const generation = memoryGeneration;
    loadedMemoryProject = memoryProjectId;
    loadedMemoryRun = latestRunKey;
    memory = [];
    editingMemoryId = '';
    editingContent = '';
    memoryError = '';
    void loadMemory(memoryProjectId, generation);
  }

  function runStatusLabel(status: string): string {
    const key = `status.${status}` as TranslationKey;
    return $translate(key) || status;
  }

  const STATE_KEYS: Record<string, TranslationKey> = {
    ok: 'state.ok',
    missing: 'state.missing',
    present: 'state.present',
    active: 'state.active',
    stopped: 'state.stopped',
    none: 'state.none',
  };
  function stateLabel(raw: string | undefined): string {
    if (raw === undefined) return $translate('common.none');
    const key = STATE_KEYS[raw];
    return key ? $translate(key) : raw;
  }
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('overview.title')}</p>
    <h1>{project?.name ?? $translate('overview.noSelection')}</h1>
    <p>{project?.description}</p>
  </div>
  {#if project}<button
      class="primary"
      onclick={onRun}
      disabled={snapshot.settings.safeMode || busy === `run-${project.id}`}
      ><Play size={17} />{$translate('overview.startRun')}</button
    >{/if}
</section>
{#if project}
  <section class="overview-grid">
    <article class="panel hero-panel">
      <div class="panel-icon"><Gauge /></div>
      <div>
        <span>{$translate('overview.lastRun')}</span>
        <h2>{lastRun ? runStatusLabel(lastRun.status) : $translate('overview.noData')}</h2>
        <p>{project.path}</p>
      </div>
    </article>
    <article class="panel">
      <GitBranch /><span>{$translate('overview.git')}</span><strong
        >{$translate('overview.noData')}</strong
      >
    </article>
    <article class="panel">
      <Waypoints /><span>{$translate('overview.rojo')}</span><strong
        >{stateLabel(snapshot.diagnostics.dependencies.rojo?.status)}</strong
      >
    </article>
    <article class="panel">
      <Waypoints /><span>{$translate('overview.rojoSync')}</span><strong
        >{project.sync.active
          ? $translate('overview.rojoSyncRunning')
          : $translate('overview.rojoSyncStopped')}</strong
      >
      {#if project.sync.active}
        <p class="panel-hint">{$translate('overview.rojoSyncPort')}: {project.sync.port}</p>
        {#if project.sync.recentLogs?.length}
          <pre class="log-lines">{project.sync.recentLogs.join('\n')}</pre>
        {/if}
      {/if}
    </article>
    <article class="panel" class:panel-warning={studioMcpCheck?.status === 'missing'}>
      <Plug /><span>{$translate('overview.studioMcp')}</span><strong
        >{stateLabel(studioMcpCheck?.status)}</strong
      >
      {#if studioMcpCheck?.status === 'missing'}
        <p class="panel-hint">{$translate('overview.studioMcpMissing')}</p>
      {/if}
    </article>
    <article class="panel">
      <Bot /><span>{$translate('overview.studio')}</span><strong
        >{snapshot.studios.find((studio) => studio.projectId === project?.id)?.name ??
          $translate('common.none')}</strong
      >
    </article>
    <article class="panel budget-panel">
      <CircleDollarSign /><span>{$translate('common.budget')}</span><strong
        >{formatMoney(project.budgetUsed, $locale)} / {formatMoney(
          project.budgetLimit,
          $locale,
        )}</strong
      >
    </article>
    <article class="panel style-panel">
      <Palette /><span>{$translate('overview.style')}</span>
      {#if styleBusy && !stylePacks.length}
        <strong>{$translate('overview.styleLoading')}</strong>
      {:else if stylePacks.length}
        <select
          aria-label={$translate('overview.style')}
          value={selectedStyle}
          onchange={chooseStyle}
          disabled={styleBusy}
        >
          {#each stylePacks as pack}
            <option value={pack.name}>{pack.name}{pack.builtIn ? '' : ' · custom'}</option>
          {/each}
        </select>
        <p class="panel-hint">{$translate('overview.styleHint')}</p>
      {:else}
        <strong>{$translate('common.none')}</strong>
      {/if}
      {#if styleError}<p class="panel-hint memory-error" role="alert">{styleError}</p>{/if}
    </article>
  </section>
  <section class="panel memory-panel">
    <div class="memory-heading">
      <div>
        <span class="memory-title"><Star size={17} />{$translate('memory.title')}</span>
        <p class="panel-hint">{$translate('memory.subtitle')}</p>
      </div>
      {#if memory.length}<button
          class="danger-button"
          onclick={removeAllMemory}
          disabled={memoryBusy}
        >
          <Trash2 size={15} />{$translate('memory.clear')}
        </button>{/if}
    </div>
    {#if memoryError}<p class="panel-hint memory-error" role="alert">
        {memoryError}
      </p>{:else if memoryBusy && !memory.length}
      <p class="panel-hint">{$translate('common.loading')}</p>
    {:else if !memory.length}
      <p class="panel-hint">{$translate('memory.empty')}</p>
    {:else}
      <div class="memory-list">
        {#each memory as entry (entry.id)}
          <article class="memory-entry" class:memory-injected={entry.injected}>
            {#if editingMemoryId === entry.id}
              <textarea
                bind:value={editingContent}
                rows="3"
                aria-label={$translate('memory.content')}
              ></textarea>
              <div class="memory-actions">
                <button
                  class="primary"
                  onclick={() => saveMemory(entry)}
                  disabled={memoryBusy || !editingContent.trim()}
                  ><Save size={14} />{$translate('common.save')}</button
                >
                <button onclick={() => (editingMemoryId = '')}
                  ><X size={14} />{$translate('common.cancel')}</button
                >
              </div>
            {:else}
              <p class="memory-content">{entry.content}</p>
              <div class="memory-meta">
                <span>{formatDate(entry.createdAt, $locale)}</span>
                {#if entry.runId}<button
                    class="memory-run-link"
                    onclick={() => onOpenRun(entry.runId!)}>{$translate('memory.sourceRun')}</button
                  >{/if}
                {#if entry.injected}<span class="memory-badge">{$translate('memory.injected')}</span
                  >{/if}
                {#if entry.pinned}<span class="memory-badge"
                    ><Star size={12} />{$translate('memory.pinned')}</span
                  >{/if}
              </div>
              <div class="memory-actions">
                <button onclick={() => startMemoryEdit(entry)} disabled={memoryBusy}
                  ><Pencil size={14} />{$translate('common.edit')}</button
                >
                <button onclick={() => toggleMemoryPin(entry)} disabled={memoryBusy}
                  ><Star size={14} />{entry.pinned
                    ? $translate('memory.unpin')
                    : $translate('memory.pin')}</button
                >
                <button
                  class="danger-button"
                  onclick={() => removeMemory(entry)}
                  disabled={memoryBusy}><Trash2 size={14} />{$translate('common.delete')}</button
                >
              </div>
            {/if}
          </article>
        {/each}
      </div>
    {/if}
  </section>
{/if}

<style>
  .memory-panel {
    grid-column: 1 / -1;
  }
  .style-panel {
    display: grid;
    gap: 0.45rem;
  }
  .style-panel select {
    width: 100%;
  }
  .memory-heading,
  .memory-meta,
  .memory-actions {
    display: flex;
    align-items: center;
    gap: 0.55rem;
  }
  .memory-heading {
    justify-content: space-between;
    gap: 1rem;
  }
  .memory-title {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    font-weight: 650;
  }
  .memory-list {
    display: grid;
    gap: 0.7rem;
    margin-top: 0.8rem;
  }
  .memory-entry {
    border: 1px solid var(--line);
    border-radius: 0.65rem;
    padding: 0.8rem;
  }
  .memory-entry.memory-injected {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent) 20%, transparent);
  }
  .memory-content {
    margin: 0 0 0.55rem;
    white-space: pre-wrap;
  }
  .memory-meta {
    color: var(--muted);
    flex-wrap: wrap;
    font-size: 0.78rem;
  }
  .memory-run-link {
    color: var(--accent);
    padding: 0;
    background: none;
    border: 0;
  }
  .memory-badge {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    color: var(--accent);
  }
  .memory-actions {
    justify-content: flex-end;
    margin-top: 0.65rem;
    flex-wrap: wrap;
  }
  .memory-actions button,
  .danger-button {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
  }
  .memory-error {
    color: var(--danger);
  }
  .memory-entry textarea {
    width: 100%;
    box-sizing: border-box;
  }
</style>
