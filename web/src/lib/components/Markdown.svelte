<script lang="ts">
  import { renderMarkdown } from '$lib/markdown';

  export let source: string;
</script>

<div class="markdown-body">{@html renderMarkdown(source)}</div>

<style>
  .markdown-body {
    overflow-wrap: anywhere;
    font-size: 0.85rem;
    line-height: 1.55;
  }
  .markdown-body :global(> :first-child) {
    margin-top: 5px;
  }
  .markdown-body :global(> :last-child) {
    margin-bottom: 0;
  }
  .markdown-body :global(p) {
    margin: 0.65em 0;
  }
  .markdown-body :global(h1),
  .markdown-body :global(h2),
  .markdown-body :global(h3),
  .markdown-body :global(h4) {
    margin: 1em 0 0.4em;
    color: var(--text);
    line-height: 1.25;
    letter-spacing: -0.01em;
  }
  .markdown-body :global(h1) {
    font-size: 1.18rem;
  }
  .markdown-body :global(h2) {
    font-size: 1.08rem;
  }
  .markdown-body :global(h3),
  .markdown-body :global(h4) {
    font-size: 0.98rem;
  }
  .markdown-body :global(ul),
  .markdown-body :global(ol) {
    margin: 0.55em 0;
    padding-left: 1.5em;
  }
  .markdown-body :global(li) {
    padding-left: 0.15em;
  }
  .markdown-body :global(li + li) {
    margin-top: 0.22em;
  }
  .markdown-body :global(blockquote) {
    margin: 0.75em 0;
    padding: 0.15em 0 0.15em 0.85em;
    border-left: 3px solid color-mix(in srgb, var(--accent) 55%, var(--line));
    color: var(--muted);
  }
  .markdown-body :global(pre) {
    max-width: 100%;
    margin: 0.75em 0;
    padding: 10px 12px;
    overflow-x: auto;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: color-mix(in srgb, var(--bg) 72%, var(--surface));
  }
  .markdown-body :global(code) {
    padding: 0.12em 0.32em;
    border-radius: 4px;
    background: color-mix(in srgb, var(--accent) 9%, var(--surface));
    color: color-mix(in srgb, var(--text) 88%, var(--accent));
  }
  .markdown-body :global(pre code) {
    padding: 0;
    background: transparent;
    color: inherit;
    white-space: pre;
  }
  .markdown-body :global(a) {
    color: var(--accent);
    text-decoration-thickness: 1px;
    text-underline-offset: 2px;
  }
  .markdown-body :global(a:hover) {
    text-decoration-thickness: 2px;
  }
  .markdown-body :global(hr) {
    margin: 1em 0;
    border: 0;
    border-top: 1px solid var(--line);
  }
  .markdown-body :global(table) {
    display: block;
    max-width: 100%;
    margin: 0.75em 0;
    overflow-x: auto;
    border-collapse: collapse;
  }
  .markdown-body :global(th),
  .markdown-body :global(td) {
    padding: 6px 9px;
    border: 1px solid var(--line);
    text-align: left;
  }
  .markdown-body :global(th) {
    background: var(--surface-3);
  }
  .markdown-body :global(input[type='checkbox']) {
    margin: 0 0.45em 0 0;
    accent-color: var(--accent);
  }
</style>
