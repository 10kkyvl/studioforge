<script lang="ts">
  import { formatDate, locale, translate } from '$lib/i18n';
  import type { StudioChange } from '$lib/types';
  export let changes: StudioChange[] = [];
  function operation(value: string, t: typeof $translate) {
    switch (value) {
      case 'create':
        return t('studioChanges.create');
      case 'modify':
        return t('studioChanges.modify');
      case 'delete':
        return t('studioChanges.delete');
      case 'reparent':
        return t('studioChanges.reparent');
      default:
        return t('studioChanges.unknown');
    }
  }
  function status(value: string, t: typeof $translate) {
    switch (value) {
      case 'succeeded':
        return t('studioChanges.succeeded');
      case 'error':
        return t('studioChanges.error');
      case 'pending':
        return t('studioChanges.pending');
      default:
        return t('studioChanges.uncertain');
    }
  }
  $: counts = changes.reduce<Record<string, number>>((result, change) => {
    result[change.operation] = (result[change.operation] ?? 0) + 1;
    return result;
  }, {});
</script>

<details class="studio-changes">
  <summary
    >{$translate('studioChanges.title')} · {changes.length}
    {$translate('studioChanges.records')}</summary
  >
  <p class="muted">{$translate('studioChanges.scope')}</p>
  {#if changes.length === 0}
    <p class="muted">{$translate('studioChanges.empty')}</p>
  {:else}
    <p>
      {Object.entries(counts)
        .map(([key, count]) => `${operation(key, $translate)}: ${count}`)
        .join(' · ')}
    </p>
    <ol>
      {#each changes as change (change.id)}
        <li>
          <strong>{operation(change.operation, $translate)}</strong> · <code>{change.tool}</code>
          <div><code>{change.target || $translate('studioChanges.unknownTarget')}</code></div>
          {#if change.properties.length}<div>
              {$translate('studioChanges.properties')}: {change.properties.join(', ')}
            </div>{/if}
          <small
            >{status(change.status, $translate)} · {formatDate(change.createdAt, $locale)}</small
          >
        </li>
      {/each}
    </ol>
  {/if}
</details>

<style>
  .studio-changes {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 12px;
    margin: 8px 0;
    overflow-wrap: anywhere;
  }
  summary {
    cursor: pointer;
    font-weight: 600;
  }
  .muted,
  small {
    color: var(--muted);
  }
  ol {
    padding-left: 24px;
    max-height: 400px;
    overflow: auto;
  }
  li {
    margin: 12px 0;
  }
</style>
