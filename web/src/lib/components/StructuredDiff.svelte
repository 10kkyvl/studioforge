<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { translate } from '$lib/i18n';
  import type { DiffFile, DiffHunk, DiffLine, DiffStats } from '$lib/types';
  import {
    collapseContext,
    loadDiffViewMode,
    resolveViewMode,
    saveDiffViewMode,
    toSideBySide,
    type DiffViewMode,
    type SideRow,
  } from '$lib/diffView';
  import { languageForPath, tokenizeLine } from '$lib/diff/highlight';
  import { applySpans, intralineSpans, pairChangedLines, type Span } from '$lib/diff/intraline';
  import { splitPatchByFile } from '$lib/diff/patchsplit';
  import {
    emptySelection,
    toggleFile as toggleFileSelection,
    toggleHunk as toggleHunkSelection,
    type RollbackSelectionState,
  } from '$lib/rollbackSelection';

  type RenderToken = { text: string; kind: string; emph: boolean };
  type RenderLine = DiffLine & { renderTokens: RenderToken[] };
  type RenderHunk = Omit<DiffHunk, 'lines'> & { lines: RenderLine[] };
  type RenderFile = Omit<DiffFile, 'hunks'> & { hunks: RenderHunk[] };

  type SplitHunkRow =
    | { kind: 'collapsed'; key: string; count: number; lines: RenderLine[] }
    | { kind: 'side'; key: string; row: SideRow<RenderLine> };

  export let stats: DiffStats;
  export let files: DiffFile[];
  export let note: string = '';
  export let emptyMessage: string = '';
  export let compact: boolean = false;
  export let getRawPatch: (() => Promise<string>) | null = null;
  export let patchFileName: string = 'changes.patch';
  export let selectable: boolean = false;
  export let selection: RollbackSelectionState = emptySelection();
  export let selectFileLabel: string = '';
  export let selectHunkLabel: string = '';

  function handleToggleFile(path: string) {
    selection = toggleFileSelection(selection, path);
  }

  function handleToggleHunk(path: string, index: number) {
    selection = toggleHunkSelection(selection, path, index);
  }

  let rootEl: HTMLDivElement;
  let rootWidth = 0;
  let viewMode: DiffViewMode = loadDiffViewMode();
  let effectiveMode: DiffViewMode;
  $: effectiveMode = resolveViewMode(rootWidth, viewMode);

  let resizeObserver: ResizeObserver | undefined;

  onMount(() => {
    if (typeof ResizeObserver === 'undefined') return;
    resizeObserver = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) rootWidth = entry.contentRect.width;
    });
    resizeObserver.observe(rootEl);
  });

  onDestroy(() => {
    resizeObserver?.disconnect();
  });

  function setViewMode(mode: DiffViewMode) {
    viewMode = mode;
    saveDiffViewMode(mode);
  }

  let openState: Record<number, boolean> = {};
  let expandedRows = new Set<string>();
  let fileRefs: HTMLLIElement[] = [];

  function fileKey(file: DiffFile): string {
    return file.oldPath ? `${file.oldPath}->${file.path}` : file.path;
  }

  $: filesWithHunks = files.filter((file) => file.hunks.length > 0).length;
  $: expandByDefault = filesWithHunks <= 5;

  function isOpen(index: number): boolean {
    return openState[index] ?? expandByDefault;
  }

  function handleToggle(index: number, event: Event) {
    openState = { ...openState, [index]: (event.currentTarget as HTMLDetailsElement).open };
  }

  function jumpToFile(index: number) {
    openState = { ...openState, [index]: true };
    void tick().then(() => fileRefs[index]?.scrollIntoView({ block: 'nearest' }));
  }

  function basename(path: string): string {
    return path.split('/').pop() || path;
  }

  function statusLabel(status: string): string {
    switch (status) {
      case 'added':
        return $translate('diff.status.added');
      case 'deleted':
        return $translate('diff.status.deleted');
      case 'renamed':
        return $translate('diff.status.renamed');
      case 'copied':
        return $translate('diff.status.copied');
      case 'modified':
        return $translate('diff.status.modified');
      default:
        return '';
    }
  }

  function markerFor(type: string): string {
    if (type === 'add') return '+';
    if (type === 'delete') return '-';
    return ' ';
  }

  function rowKey(fileIndex: number, hunkIndex: number, rowIndex: number | string): string {
    return `${fileIndex}:${hunkIndex}:${rowIndex}`;
  }

  function toggleRow(key: string) {
    const next = new Set(expandedRows);
    if (next.has(key)) {
      next.delete(key);
    } else {
      next.add(key);
    }
    expandedRows = next;
  }

  function buildRenderHunk(hunk: DiffHunk, lang: ReturnType<typeof languageForPath>): RenderHunk {
    const tokensByIndex = hunk.lines.map((line) => tokenizeLine(line.text, lang));
    const emphByIndex = new Map<number, Span[]>();
    for (const { deleteIdx, addIdx } of pairChangedLines(hunk.lines)) {
      const spans = intralineSpans(hunk.lines[deleteIdx].text, hunk.lines[addIdx].text);
      if (!spans) continue;
      emphByIndex.set(deleteIdx, spans.old);
      emphByIndex.set(addIdx, spans.neu);
    }
    const lines: RenderLine[] = hunk.lines.map((line, index) => {
      const spans = emphByIndex.get(index);
      const tokens = spans
        ? applySpans(tokensByIndex[index], spans)
        : tokensByIndex[index].map((token) => ({ ...token, emph: false }));
      return { ...line, renderTokens: tokens };
    });
    return { ...hunk, lines };
  }

  function buildRenderFile(file: DiffFile): RenderFile {
    const lang = languageForPath(file.path);
    return { ...file, hunks: file.hunks.map((hunk) => buildRenderHunk(hunk, lang)) };
  }

  let renderFiles: RenderFile[] = [];
  $: renderFiles = files.map(buildRenderFile);

  function hunkRows(hunk: RenderHunk) {
    return collapseContext<RenderLine>(hunk.lines);
  }

  function splitHunkRows(hunk: RenderHunk): SplitHunkRow[] {
    const rows: SplitHunkRow[] = [];
    let buffer: RenderLine[] = [];
    let n = 0;
    const flush = () => {
      if (buffer.length === 0) return;
      for (const side of toSideBySide<RenderLine>(buffer))
        rows.push({ kind: 'side', key: `s${n++}`, row: side });
      buffer = [];
    };
    for (const row of collapseContext<RenderLine>(hunk.lines)) {
      if (row.kind === 'line') {
        buffer.push(row.line);
      } else {
        flush();
        rows.push({ kind: 'collapsed', key: `c${n++}`, count: row.count, lines: row.lines });
      }
    }
    flush();
    return rows;
  }

  let rawPatchCache: string | null = null;
  let rawPatchPromise: Promise<string> | null = null;
  $: {
    void files;
    rawPatchCache = null;
    rawPatchPromise = null;
  }

  function loadRawPatch(): Promise<string> {
    if (rawPatchCache !== null) return Promise.resolve(rawPatchCache);
    if (!rawPatchPromise) {
      const fetcher = getRawPatch;
      rawPatchPromise = (fetcher ? fetcher() : Promise.resolve('')).then((patch) => {
        rawPatchCache = patch;
        return patch;
      });
    }
    return rawPatchPromise;
  }

  let copiedKey: string | null = null;
  let copiedTimer: ReturnType<typeof setTimeout> | undefined;

  function flashCopied(key: string) {
    copiedKey = key;
    if (copiedTimer) clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => {
      copiedKey = null;
    }, 1500);
  }

  onDestroy(() => {
    if (copiedTimer) clearTimeout(copiedTimer);
  });

  function guardSummaryClick(event: MouseEvent, action: () => void) {
    event.preventDefault();
    event.stopPropagation();
    action();
  }

  async function copyPath(path: string, key: string) {
    try {
      await navigator.clipboard.writeText(path);
      flashCopied(key);
    } catch {}
  }

  async function copyPatch() {
    if (!getRawPatch) return;
    const patch = await loadRawPatch();
    try {
      await navigator.clipboard.writeText(patch);
      flashCopied('patch');
    } catch {}
  }

  async function copyFilePatch(file: DiffFile, key: string) {
    if (!getRawPatch) return;
    const raw = await loadRawPatch();
    const segments = splitPatchByFile(raw);
    const match =
      segments.find((segment) => segment.path === file.path) ??
      segments.find((segment) => segment.path === file.oldPath);
    try {
      await navigator.clipboard.writeText(match ? match.patch : raw);
      flashCopied(key);
    } catch {}
  }

  async function downloadPatch() {
    if (!getRawPatch) return;
    const patch = await loadRawPatch();
    const blob = new Blob([patch], { type: 'text/x-diff' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = patchFileName;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(url);
  }
</script>

{#snippet lineTokens(tokens: RenderToken[], lineType: string)}
  {#each tokens as tok, tokIndex (tokIndex)}
    <span
      class={`tok-${tok.kind}`}
      class:sd-emph-del={tok.emph && lineType === 'delete'}
      class:sd-emph-add={tok.emph && lineType === 'add'}>{tok.text}</span
    >
  {/each}
{/snippet}

<div class="sd-root" bind:this={rootEl}>
  {#if note}
    <p class="sd-note">{note}</p>
  {:else if files.length === 0}
    <p class="sd-note">{emptyMessage || $translate('chat.diffNoChanges')}</p>
  {:else}
    <p class="sd-stats">
      {stats.filesChanged}
      {$translate('diff.filesChanged')} ·
      <span class="sd-additions">+{stats.additions}</span>
      <span class="sd-deletions">&minus;{stats.deletions}</span>
      <span class="sd-stats-actions">
        {#if getRawPatch}
          <button type="button" class="sd-export-button" onclick={() => void copyPatch()}>
            {copiedKey === 'patch' ? $translate('diff.copied') : $translate('diff.copyPatch')}
          </button>
          <button type="button" class="sd-export-button" onclick={() => void downloadPatch()}>
            {$translate('diff.downloadPatch')}
          </button>
        {/if}
        {#if !compact}
          <span class="sd-view-toggle" role="group">
            <button
              type="button"
              class="sd-view-button"
              class:sd-view-active={viewMode === 'unified'}
              onclick={() => setViewMode('unified')}
            >
              {$translate('diff.viewUnified')}
            </button>
            <button
              type="button"
              class="sd-view-button"
              class:sd-view-active={viewMode === 'split'}
              onclick={() => setViewMode('split')}
            >
              {$translate('diff.viewSplit')}
            </button>
          </span>
        {/if}
      </span>
    </p>
    {#if files.length > 1 && !compact}
      <div class="sd-chip-strip">
        {#each files as file, fileIndex (fileKey(file))}
          <button
            type="button"
            class="sd-chip"
            aria-label={$translate('diff.jumpToFile')}
            onclick={() => jumpToFile(fileIndex)}
          >
            <span class="sd-chip-name">{basename(file.path)}</span>
            <span class="sd-additions">+{file.additions}</span>
            <span class="sd-deletions">&minus;{file.deletions}</span>
          </button>
        {/each}
      </div>
    {/if}
    <ul class="sd-file-list" class:sd-compact={compact}>
      {#each renderFiles as file, fileIndex (fileKey(file))}
        <li class="sd-file" bind:this={fileRefs[fileIndex]}>
          {#if file.hunks.length > 0}
            <details
              class="sd-file-details"
              open={isOpen(fileIndex)}
              ontoggle={(event) => handleToggle(fileIndex, event)}
            >
              <summary class="sd-file-summary">
                {#if selectable && !file.binary}
                  <input
                    type="checkbox"
                    class="sd-select-box"
                    checked={selection.files.has(file.path)}
                    aria-label={selectFileLabel || $translate('diff.selectFile')}
                    onclick={(event) => guardSummaryClick(event, () => handleToggleFile(file.path))}
                  />
                {/if}
                {#if statusLabel(file.status)}
                  <span class="sd-file-status">{statusLabel(file.status)}</span>
                {/if}
                <span class="sd-file-path">
                  {#if file.oldPath}<span class="sd-file-oldpath">{file.oldPath}</span> →
                  {/if}{file.path}
                </span>
                <span class="sd-file-counts">
                  <span class="sd-additions">+{file.additions}</span>
                  <span class="sd-deletions">&minus;{file.deletions}</span>
                </span>
                <span class="sd-file-actions">
                  <button
                    type="button"
                    class="sd-file-action"
                    onclick={(event) =>
                      guardSummaryClick(event, () => void copyPath(file.path, `path:${fileIndex}`))}
                  >
                    {copiedKey === `path:${fileIndex}`
                      ? $translate('diff.copied')
                      : $translate('diff.copyPath')}
                  </button>
                  {#if getRawPatch}
                    <button
                      type="button"
                      class="sd-file-action"
                      onclick={(event) =>
                        guardSummaryClick(
                          event,
                          () => void copyFilePatch(file, `filepatch:${fileIndex}`),
                        )}
                    >
                      {copiedKey === `filepatch:${fileIndex}`
                        ? $translate('diff.copied')
                        : $translate('diff.copyFilePatch')}
                    </button>
                  {/if}
                </span>
              </summary>
              <div class="sd-hunks">
                {#each file.hunks as hunk, hunkIndex (hunk.header)}
                  <div class="sd-hunk-header">
                    {#if selectable}
                      <input
                        type="checkbox"
                        class="sd-select-box"
                        checked={selection.hunks.get(file.path)?.has(hunkIndex) ?? false}
                        aria-label={selectHunkLabel || $translate('diff.selectHunk')}
                        onclick={() => handleToggleHunk(file.path, hunkIndex)}
                      />
                    {/if}
                    <span class="sd-hunk-header-text">{hunk.header}</span>
                  </div>
                  {#if effectiveMode === 'split'}
                    <div class="sd-hunk-split">
                      {#each splitHunkRows(hunk) as row (row.key)}
                        {#if row.kind === 'collapsed'}
                          {@const key = rowKey(fileIndex, hunkIndex, row.key)}
                          {#if expandedRows.has(key)}
                            {#each toSideBySide<RenderLine>(row.lines) as side, sideIndex (sideIndex)}
                              <div
                                class={`sd-split-row sd-split-left sd-line-${side.left?.type ?? 'empty'}`}
                              >
                                {#if side.left}<span class="sd-line-text"
                                    >{@render lineTokens(
                                      side.left.renderTokens,
                                      side.left.type,
                                    )}</span
                                  >{/if}
                              </div>
                              <div
                                class={`sd-split-row sd-split-right sd-line-${side.right?.type ?? 'empty'}`}
                              >
                                {#if side.right}<span class="sd-line-text"
                                    >{@render lineTokens(
                                      side.right.renderTokens,
                                      side.right.type,
                                    )}</span
                                  >{/if}
                              </div>
                            {/each}
                          {:else}
                            <button
                              type="button"
                              class="sd-collapsed-row"
                              onclick={() => toggleRow(key)}
                            >
                              ⋯ {row.count}
                              {$translate('diff.unchangedLines')}
                            </button>
                          {/if}
                        {:else}
                          <div
                            class={`sd-split-row sd-split-left sd-line-${row.row.left?.type ?? 'empty'}`}
                          >
                            {#if row.row.left}<span class="sd-line-text"
                                >{@render lineTokens(
                                  row.row.left.renderTokens,
                                  row.row.left.type,
                                )}</span
                              >{/if}
                          </div>
                          <div
                            class={`sd-split-row sd-split-right sd-line-${row.row.right?.type ?? 'empty'}`}
                          >
                            {#if row.row.right}<span class="sd-line-text"
                                >{@render lineTokens(
                                  row.row.right.renderTokens,
                                  row.row.right.type,
                                )}</span
                              >{/if}
                          </div>
                        {/if}
                      {/each}
                    </div>
                  {:else}
                    <div class="sd-hunk-lines">
                      {#each hunkRows(hunk) as row, rowIndex (rowKey(fileIndex, hunkIndex, rowIndex))}
                        {#if row.kind === 'collapsed'}
                          {@const key = rowKey(fileIndex, hunkIndex, rowIndex)}
                          {#if expandedRows.has(key)}
                            {#each row.lines as line, lineIndex (lineIndex)}
                              <div class={`sd-line sd-line-${line.type}`}>
                                <span class="sd-line-marker">{markerFor(line.type)}</span>
                                <span class="sd-line-text"
                                  >{@render lineTokens(line.renderTokens, line.type)}</span
                                >
                              </div>
                            {/each}
                          {:else}
                            <button
                              type="button"
                              class="sd-collapsed-row"
                              onclick={() => toggleRow(key)}
                            >
                              ⋯ {row.count}
                              {$translate('diff.unchangedLines')}
                            </button>
                          {/if}
                        {:else}
                          <div class={`sd-line sd-line-${row.line.type}`}>
                            <span class="sd-line-marker">{markerFor(row.line.type)}</span>
                            <span class="sd-line-text"
                              >{@render lineTokens(row.line.renderTokens, row.line.type)}</span
                            >
                          </div>
                        {/if}
                      {/each}
                    </div>
                  {/if}
                {/each}
              </div>
            </details>
          {:else}
            <div class="sd-file-summary sd-file-summary-static">
              {#if selectable && !file.binary}
                <input
                  type="checkbox"
                  class="sd-select-box"
                  checked={selection.files.has(file.path)}
                  aria-label={selectFileLabel || $translate('diff.selectFile')}
                  onclick={() => handleToggleFile(file.path)}
                />
              {/if}
              {#if statusLabel(file.status)}
                <span class="sd-file-status">{statusLabel(file.status)}</span>
              {/if}
              <span class="sd-file-path">
                {#if file.oldPath}<span class="sd-file-oldpath">{file.oldPath}</span> →
                {/if}{file.path}
              </span>
              <span class="sd-file-counts">
                <span class="sd-additions">+{file.additions}</span>
                <span class="sd-deletions">&minus;{file.deletions}</span>
              </span>
              {#if file.binary}<span class="sd-file-binary">{$translate('diff.binary')}</span>{/if}
              <span class="sd-file-actions">
                <button
                  type="button"
                  class="sd-file-action"
                  onclick={() => void copyPath(file.path, `path:${fileIndex}`)}
                >
                  {copiedKey === `path:${fileIndex}`
                    ? $translate('diff.copied')
                    : $translate('diff.copyPath')}
                </button>
                {#if getRawPatch}
                  <button
                    type="button"
                    class="sd-file-action"
                    onclick={() => void copyFilePatch(file, `filepatch:${fileIndex}`)}
                  >
                    {copiedKey === `filepatch:${fileIndex}`
                      ? $translate('diff.copied')
                      : $translate('diff.copyFilePatch')}
                  </button>
                {/if}
              </span>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .sd-root {
    min-width: 0;
  }
  .sd-note {
    margin: 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .sd-stats {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    margin: 0 0 6px;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .sd-additions {
    color: var(--green);
  }
  .sd-deletions {
    color: var(--red);
  }
  .sd-stats-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
    flex-wrap: wrap;
  }
  .sd-export-button {
    padding: 2px 8px;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--muted);
    font-size: var(--fs-2xs);
    cursor: pointer;
  }
  .sd-export-button:hover {
    color: var(--text);
  }
  .sd-view-toggle {
    display: inline-flex;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    overflow: hidden;
  }
  .sd-view-button {
    padding: 2px 8px;
    border: 0;
    background: var(--surface-2);
    color: var(--muted);
    font-size: var(--fs-2xs);
    cursor: pointer;
  }
  .sd-view-button + .sd-view-button {
    border-left: 1px solid var(--line);
  }
  .sd-view-active {
    background: var(--surface-3);
    color: var(--text);
  }
  .sd-chip-strip {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 0 0 8px;
  }
  .sd-chip {
    display: flex;
    align-items: baseline;
    gap: 4px;
    padding: 3px 8px;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--text);
    font-size: var(--fs-2xs);
    cursor: pointer;
  }
  .sd-chip-name {
    font-family: 'Cascadia Code', Consolas, monospace;
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sd-file-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .sd-compact .sd-file-summary {
    padding: 4px 8px;
  }
  .sd-file-summary {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 6px 10px;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--text);
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  .sd-file-summary-static {
    cursor: default;
  }
  .sd-file-details > summary::-webkit-details-marker {
    display: none;
  }
  .sd-file-details > summary {
    list-style: none;
  }
  .sd-file-details[open] > summary {
    border-bottom-left-radius: 0;
    border-bottom-right-radius: 0;
  }
  .sd-file-status {
    flex: none;
    color: var(--muted);
    text-transform: uppercase;
    font-size: var(--fs-2xs);
    letter-spacing: 0.06em;
  }
  .sd-file-path {
    flex: 1 1 auto;
    min-width: 0;
    overflow-wrap: anywhere;
    font-family: 'Cascadia Code', Consolas, monospace;
  }
  .sd-file-oldpath {
    color: var(--muted);
    text-decoration: line-through;
  }
  .sd-file-counts {
    flex: none;
    display: flex;
    gap: 6px;
    font-family: 'Cascadia Code', Consolas, monospace;
  }
  .sd-file-binary {
    flex: none;
    color: var(--muted);
    font-size: var(--fs-2xs);
  }
  .sd-file-actions {
    flex: none;
    display: flex;
    gap: 4px;
  }
  .sd-file-action {
    padding: 1px 6px;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface);
    color: var(--muted);
    font-size: var(--fs-2xs);
    cursor: pointer;
  }
  .sd-file-action:hover {
    color: var(--text);
  }
  .sd-hunks {
    max-height: 320px;
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-top: 0;
    border-bottom-left-radius: var(--r-sm);
    border-bottom-right-radius: var(--r-sm);
    background: var(--surface);
    overflow-y: auto;
    overflow-x: auto;
  }
  .sd-hunk-header {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 8px 0 2px;
    color: var(--muted);
    font-family: 'Cascadia Code', Consolas, monospace;
    font-size: var(--fs-2xs);
  }
  .sd-hunk-header:first-child {
    margin-top: 0;
  }
  .sd-select-box {
    flex: none;
    accent-color: var(--danger);
    cursor: pointer;
  }
  .sd-hunk-lines {
    font-family: 'Cascadia Code', Consolas, monospace;
    font-size: var(--fs-2xs);
    line-height: 1.5;
  }
  .sd-hunk-split {
    display: grid;
    grid-template-columns: 1fr 1fr;
    font-family: 'Cascadia Code', Consolas, monospace;
    font-size: var(--fs-2xs);
    line-height: 1.5;
  }
  .sd-split-row {
    padding: 0 4px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .sd-split-left {
    border-right: 1px solid var(--line);
  }
  .sd-collapsed-row {
    display: block;
    width: 100%;
    margin: 2px 0;
    padding: 3px 6px;
    border: 1px dashed var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--muted);
    font-family: 'Cascadia Code', Consolas, monospace;
    font-size: var(--fs-2xs);
    text-align: left;
    cursor: pointer;
    grid-column: 1 / -1;
  }
  .sd-line {
    display: flex;
    gap: 6px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .sd-line-marker {
    flex: none;
    width: 10px;
    color: var(--muted);
  }
  .sd-line-text {
    color: var(--text);
  }
  .sd-line-add {
    background: color-mix(in srgb, var(--green) 12%, transparent);
  }
  .sd-line-add .sd-line-marker {
    color: var(--green);
  }
  .sd-line-delete {
    background: color-mix(in srgb, var(--red) 12%, transparent);
  }
  .sd-line-delete .sd-line-marker {
    color: var(--red);
  }
  .tok-keyword {
    color: var(--steel);
  }
  .tok-string {
    color: var(--warn);
  }
  .tok-comment {
    color: var(--muted);
    font-style: italic;
  }
  .tok-number {
    color: var(--text-dim);
  }
  .sd-emph-add {
    background: color-mix(in srgb, var(--green) 30%, transparent);
    border-radius: 2px;
  }
  .sd-emph-del {
    background: color-mix(in srgb, var(--red) 30%, transparent);
    border-radius: 2px;
  }
</style>
