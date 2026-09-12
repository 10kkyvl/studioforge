<script lang="ts">
  import { highlightCode, languageForPath } from '$lib/codeHighlight';

  export let code = '';
  export let path = '';

  $: highlighted = highlightCode(code, languageForPath(path));
</script>

<code class="syntax-code">{@html highlighted}</code>

<style>
  :global(.syntax-code .syntax-keyword) {
    color: var(--accent-2);
  }
  :global(.syntax-code .syntax-string) {
    color: var(--green);
  }
  :global(.syntax-code .syntax-comment) {
    color: var(--muted);
    font-style: italic;
  }
  :global(.syntax-code .syntax-value) {
    color: var(--accent);
  }
  :global(.syntax-code .syntax-heading) {
    color: var(--accent);
    font-weight: 700;
  }
</style>
