<script lang="ts">
  import SyntaxCode from '$lib/components/SyntaxCode.svelte';
  import { diffFiles } from '$lib/diffFiles';

  export let diff = '';
  export let projectId = '';
  export let onOpenFile: (projectId: string, path: string) => void = () => {};

  $: files = diffFiles(diff);
</script>

<div class="diff-pre" data-testid="diff-viewer">
  {#each files as file, index (index)}
    {#if file.path}
      <div class="diff-file-row">
        {#if file.deleted}<code>{file.path}</code>
        {:else}<button type="button" onclick={() => onOpenFile(projectId, file.path)}
            >{file.path}</button
          >{/if}
      </div>
    {/if}
    {#each file.text.split('\n') as line}
      <div
        class:diff-add={line.startsWith('+') && !line.startsWith('+++')}
        class:diff-delete={line.startsWith('-') && !line.startsWith('---')}
        class="diff-line"
      >
        <SyntaxCode code={line} path={file.path} />
      </div>
    {/each}
  {/each}
</div>

<style>
  .diff-pre {
    overflow: auto;
    padding: 10px 0;
    margin: 0;
    color: var(--muted);
    font:
      0.78rem/1.55 'Cascadia Code',
      'SFMono-Regular',
      Consolas,
      monospace;
    white-space: pre;
  }
  .diff-line {
    min-height: 1.55em;
    padding: 0 14px;
  }
  .diff-add {
    background: color-mix(in srgb, var(--green) 14%, transparent);
  }
  .diff-delete {
    background: color-mix(in srgb, var(--red) 14%, transparent);
  }
  .diff-file-row {
    padding: 7px 14px;
    border-top: 1px solid var(--line);
    border-bottom: 1px solid var(--line);
    background: var(--surface-2);
  }
  .diff-file-row button {
    border: 0;
    padding: 0;
    color: var(--accent);
    background: transparent;
    cursor: pointer;
    font: inherit;
    text-decoration: underline;
  }
</style>
