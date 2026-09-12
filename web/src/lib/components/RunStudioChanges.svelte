<script lang="ts">
  import { getStudioChanges } from '$lib/api';
  import { translate } from '$lib/i18n';
  import StudioChangesPanel from './StudioChangesPanel.svelte';
  export let runId: string;
  export let updatedAt: string;
  let refresh = 0;
  function load(id: string, _updatedAt: string, _refresh: number) {
    return getStudioChanges(id);
  }
  $: request = load(runId, updatedAt, refresh);
</script>

{#await request}
  <p>{$translate('common.loading')}</p>
{:then changes}
  <StudioChangesPanel {changes} />
{:catch}
  <p role="alert">
    {$translate('studioChanges.loadError')}
    <button type="button" onclick={() => refresh++}>{$translate('studioChanges.retry')}</button>
  </p>
{/await}
