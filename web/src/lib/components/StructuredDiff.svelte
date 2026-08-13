<script lang="ts">
  import { translate } from '$lib/i18n';
  import type { DiffFile, DiffStats } from '$lib/types';

  export let stats: DiffStats;
  export let files: DiffFile[];
  export let note: string = '';
  export let emptyMessage: string = '';
  export let compact: boolean = false;

  function fileKey(file: DiffFile): string {
    return file.oldPath ? `${file.oldPath}->${file.path}` : file.path;
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
      default:
        return '';
    }
  }

  function markerFor(type: string): string {
    if (type === 'add') return '+';
    if (type === 'delete') return '-';
    return ' ';
  }
</script>

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
  </p>
  <ul class="sd-file-list" class:sd-compact={compact}>
    {#each files as file (fileKey(file))}
      <li class="sd-file">
        {#if file.hunks.length > 0}
          <details class="sd-file-details">
            <summary class="sd-file-summary">
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
            </summary>
            <div class="sd-hunks">
              {#each file.hunks as hunk (hunk.header)}
                <div class="sd-hunk-header">{hunk.header}</div>
                <div class="sd-hunk-lines">
                  {#each hunk.lines as line, lineIndex (lineIndex)}
                    <div class={`sd-line sd-line-${line.type}`}>
                      <span class="sd-line-marker">{markerFor(line.type)}</span>
                      <span class="sd-line-text">{line.text}</span>
                    </div>
                  {/each}
                </div>
              {/each}
            </div>
          </details>
        {:else}
          <div class="sd-file-summary sd-file-summary-static">
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
          </div>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .sd-note {
    margin: 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .sd-stats {
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
  .sd-hunks {
    max-height: 320px;
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-top: 0;
    border-bottom-left-radius: var(--r-sm);
    border-bottom-right-radius: var(--r-sm);
    background: var(--surface);
    overflow-y: auto;
  }
  .sd-hunk-header {
    margin: 8px 0 2px;
    color: var(--muted);
    font-family: 'Cascadia Code', Consolas, monospace;
    font-size: var(--fs-2xs);
  }
  .sd-hunk-header:first-child {
    margin-top: 0;
  }
  .sd-hunk-lines {
    font-family: 'Cascadia Code', Consolas, monospace;
    font-size: var(--fs-2xs);
    line-height: 1.5;
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
</style>
