<script lang="ts">
  import {
    ChevronDown,
    ChevronRight,
    File,
    Folder,
    FolderOpen,
    RefreshCw,
    X,
  } from '@lucide/svelte';
  import { friendlyError, getProjectFileContent, getProjectFiles } from '$lib/api';
  import SyntaxCode from '$lib/components/SyntaxCode.svelte';
  import { formatDate, locale, translate } from '$lib/i18n';
  import type { Project, ProjectFileContent, ProjectFileEntry } from '$lib/types';

  export let project: Project | undefined;
  export let openPath = '';

  let currentPath = '';
  let directories = new Map<string, ProjectFileEntry[]>();
  let selectedPath = '';
  let content: ProjectFileContent | null = null;
  let loading = false;
  let contentLoading = false;
  let error = '';
  let contentError = '';
  let generation = 0;
  let contentGeneration = 0;
  let openToken = 0;
  let expanded = new Set<string>();

  async function loadDirectory(projectId: string, path: string, token: number) {
    loading = true;
    error = '';
    try {
      const result = await getProjectFiles(projectId, path);
      if (token !== generation || project?.id !== projectId) return;
      directories = new Map(directories).set(path, result.entries);
    } catch (cause) {
      if (token !== generation || project?.id !== projectId) return;
      error = friendlyError(cause, $translate);
    } finally {
      if (token === generation) loading = false;
    }
  }

  async function loadContent(projectId: string, path: string, token: number) {
    contentLoading = true;
    contentError = '';
    content = null;
    try {
      const result = await getProjectFileContent(projectId, path);
      if (token !== contentGeneration || project?.id !== projectId || selectedPath !== path) return;
      content = result;
    } catch (cause) {
      if (token !== contentGeneration || project?.id !== projectId || selectedPath !== path) return;
      contentError = friendlyError(cause, $translate);
    } finally {
      if (token === contentGeneration) contentLoading = false;
    }
  }

  function selectDirectory(path: string) {
    if (!project) return;
    openToken += 1;
    contentGeneration += 1;
    currentPath = path;
    selectedPath = '';
    content = null;
    if (path && expanded.has(path)) {
      expanded = new Set([...expanded].filter((item) => item !== path));
      return;
    }
    expanded = new Set([...expanded, path]);
    if (!directories.has(path)) {
      generation += 1;
      void loadDirectory(project.id, path, generation);
    }
  }

  function selectFile(entry: ProjectFileEntry) {
    if (entry.type !== 'file' || !project) return;
    openToken += 1;
    selectedPath = entry.path;
    contentGeneration += 1;
    void loadContent(project.id, entry.path, contentGeneration);
  }

  async function revealPath(projectId: string, path: string, token: number) {
    const parts = path.split('/').filter(Boolean);
    let directory = '';
    for (const part of parts.slice(0, -1)) {
      directory = directory ? `${directory}/${part}` : part;
      expanded = new Set([...expanded, directory]);
      if (directories.has(directory)) continue;
      try {
        const result = await getProjectFiles(projectId, directory);
        if (token !== openToken || project?.id !== projectId || openPath !== path) return;
        directories = new Map(directories).set(directory, result.entries);
      } catch {
        if (token !== openToken || project?.id !== projectId || openPath !== path) return;
        return;
      }
    }
    if (token !== openToken || project?.id !== projectId || openPath !== path) return;
    currentPath = directory;
    selectedPath = path;
    contentGeneration += 1;
    void loadContent(projectId, path, contentGeneration);
  }

  function refresh() {
    if (!project) return;
    generation += 1;
    void loadDirectory(project.id, currentPath, generation);
  }

  function resetForProject(projectId: string) {
    openToken += 1;
    generation += 1;
    contentGeneration += 1;
    currentPath = '';
    selectedPath = '';
    directories = new Map();
    content = null;
    error = '';
    contentError = '';
    expanded = new Set();
    void loadDirectory(projectId, '', generation);
  }

  $: projectId = project?.id ?? '';
  $: if (projectId !== loadedProjectId) {
    loadedProjectId = projectId;
    if (projectId) resetForProject(projectId);
  }
  let loadedProjectId = '';
  let lastOpenKey = '';

  $: openKey = `${projectId}:${openPath}`;
  $: if (openPath && openKey !== lastOpenKey && projectId) {
    lastOpenKey = openKey;
    openToken += 1;
    void revealPath(projectId, openPath, openToken);
  }

  function flattenEntries(
    directoryMap: Map<string, ProjectFileEntry[]>,
    expandedPaths: Set<string>,
  ): { entry: ProjectFileEntry; depth: number }[] {
    const flattened: { entry: ProjectFileEntry; depth: number }[] = [];
    function append(path: string, depth: number) {
      for (const entry of directoryMap.get(path) ?? []) {
        flattened.push({ entry, depth });
        if (entry.type === 'directory' && expandedPaths.has(entry.path))
          append(entry.path, depth + 1);
      }
    }
    append('', 0);
    return flattened;
  }
  $: flattened = flattenEntries(directories, expanded);
</script>

<section class="page-heading files-heading" data-testid="files-view">
  <div>
    <p class="eyebrow">{$translate('nav.files')}</p>
    <h1>{$translate('files.title')}</h1>
    <p>{project?.name ?? $translate('files.noProject')}</p>
  </div>
  {#if project}
    <button type="button" onclick={refresh} disabled={loading} title={$translate('files.refresh')}>
      <RefreshCw size={16} />{$translate('files.refresh')}
    </button>
  {/if}
</section>

{#if !project}
  <section class="panel empty-cell">{$translate('files.noProject')}</section>
{:else}
  <section class="files-browser">
    <aside class="panel files-tree" aria-label={$translate('files.tree')}>
      <div class="files-tree-toolbar">
        <button type="button" class:active={currentPath === ''} onclick={() => selectDirectory('')}>
          <FolderOpen size={16} />{$translate('files.root')}
        </button>
      </div>
      {#if loading && !directories.has(currentPath)}
        <p class="files-muted">{$translate('common.loading')}</p>
      {:else if error}
        <p class="files-error" role="alert">{error}</p>
      {:else}
        <div class="files-entry-list">
          {#each flattened as item (item.entry.path)}
            {@const entry = item.entry}
            <button
              type="button"
              class:active={selectedPath === entry.path || currentPath === entry.path}
              class="file-entry"
              style={`padding-left: ${8 + item.depth * 18}px`}
              onclick={() =>
                entry.type === 'directory' ? selectDirectory(entry.path) : selectFile(entry)}
              title={entry.path}
              aria-expanded={entry.type === 'directory' ? expanded.has(entry.path) : undefined}
              disabled={entry.type === 'symlink'}
            >
              {#if entry.type === 'directory'}
                {#if expanded.has(entry.path)}<ChevronDown size={14} />{:else}<ChevronRight
                    size={14}
                  />{/if}
                <Folder size={16} />
              {:else}
                <span class="file-indent"></span><File size={16} />
              {/if}
              <span class="file-name">{entry.name}</span>
              {#if entry.type === 'symlink'}<span class="file-type"
                  >{$translate('files.symlink')}</span
                >{/if}
            </button>
          {/each}
          {#if !(directories.get(currentPath) ?? []).length && !loading}<p class="files-muted">
              {$translate('files.empty')}
            </p>{/if}
        </div>
      {/if}
    </aside>

    <article class="panel files-viewer" aria-label={$translate('files.viewer')}>
      {#if selectedPath}
        <header class="files-viewer-header">
          <div>
            <code>{selectedPath}</code><small
              >{#if content}{content.size} B · {formatDate(content.modifiedAt, $locale)}{/if}</small
            >
          </div>
          <button
            type="button"
            class="icon-button"
            onclick={() => {
              openToken += 1;
              contentGeneration += 1;
              selectedPath = '';
              content = null;
            }}
            aria-label={$translate('common.close')}><X size={16} /></button
          >
        </header>
        {#if contentLoading}
          <p class="files-muted">{$translate('common.loading')}</p>
        {:else if contentError}
          <p class="files-error" role="alert">{contentError}</p>
        {:else if content?.binary}
          <p class="files-muted">{$translate('files.binary')}</p>
        {:else if content}
          <pre class="file-content"><SyntaxCode code={content.content} path={content.path} /></pre>
        {/if}
      {:else}
        <div class="files-viewer-empty">
          <File size={30} />
          <p>{$translate('files.select')}</p>
        </div>
      {/if}
    </article>
  </section>
{/if}

<style>
  .files-heading {
    align-items: center;
  }
  .files-heading button {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    background: var(--surface);
    color: var(--text);
    font: inherit;
    cursor: pointer;
  }
  .files-heading button:disabled {
    opacity: 0.6;
    cursor: wait;
  }
  .files-browser {
    display: grid;
    grid-template-columns: minmax(220px, 0.34fr) minmax(0, 1fr);
    gap: 14px;
    min-height: 0;
    flex: 1;
  }
  .files-tree,
  .files-viewer {
    min-height: 420px;
    overflow: hidden;
  }
  .files-tree {
    padding: 10px;
  }
  .files-tree-toolbar {
    border-bottom: 1px solid var(--line);
    padding-bottom: 8px;
    margin-bottom: 8px;
  }
  .files-tree-toolbar button,
  .file-entry {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 7px;
    border: 0;
    background: transparent;
    border-radius: var(--r-sm);
    color: var(--muted);
    padding: 7px 8px;
    text-align: left;
    cursor: pointer;
  }
  .files-tree-toolbar button:hover,
  .file-entry:hover,
  .file-entry.active {
    background: var(--surface-2);
    color: var(--text);
  }
  .file-entry:disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }
  .file-indent {
    width: 14px;
    flex: none;
  }
  .file-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-type {
    margin-left: auto;
    font-size: var(--fs-2xs);
  }
  .files-muted,
  .files-error {
    color: var(--muted);
    padding: 14px;
  }
  .files-error {
    color: var(--danger);
  }
  .files-viewer {
    display: flex;
    flex-direction: column;
  }
  .files-viewer-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 14px;
    border-bottom: 1px solid var(--line);
  }
  .files-viewer-header small {
    display: block;
    color: var(--muted);
    margin-top: 4px;
  }
  .icon-button {
    display: grid;
    place-items: center;
    padding: 5px;
    border: 0;
    border-radius: var(--r-sm);
    background: transparent;
    cursor: pointer;
  }
  .icon-button:hover {
    background: var(--surface-2);
  }
  .file-content {
    flex: 1;
    margin: 0;
    padding: 16px;
    overflow: auto;
    color: var(--text);
    font:
      0.82rem/1.55 'Cascadia Code',
      'SFMono-Regular',
      Consolas,
      monospace;
    white-space: pre;
    tab-size: 2;
  }
  .files-viewer-empty {
    display: grid;
    place-items: center;
    align-content: center;
    flex: 1;
    color: var(--muted);
    gap: 8px;
  }
  .files-viewer-empty p {
    margin: 0;
  }
  @media (max-width: 760px) {
    .files-browser {
      grid-template-columns: 1fr;
    }
    .files-tree {
      min-height: 220px;
    }
  }
</style>
