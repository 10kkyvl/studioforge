<script lang="ts">
  import { formatContextLength, groupCuratedByCategory, isFreeModel } from '$lib/openrouter';
  import { translate } from '$lib/i18n';
  import type { OpenRouterCurated, OpenRouterModel } from '$lib/types';

  // Two-way bound by the caller (bind:value), exactly like the free-text
  // model input it replaces — the value is a model id passed straight
  // through to the API, never a StudioForge-side tier. Optional because the
  // agent-create draft (Partial<Agent>) may not have picked a model yet.
  export let value: string | undefined;
  export let models: OpenRouterModel[] = [];
  export let curated: OpenRouterCurated[] = [];
  export let categories: string[] = [];
  export let loading = false;
  export let error = '';
  // Id of a <datalist> the caller renders once (shared by every picker
  // instance) so multiple agent rows never emit duplicate datalist ids.
  export let datalistId: string;

  $: grouped = groupCuratedByCategory(curated, categories);
  $: modelById = new Map(models.map((model) => [model.id, model]));
  $: curatedById = new Map(curated.map((item) => [item.id, item]));
  $: selectedModel = value ? modelById.get(value) : undefined;
  $: selectedCurated = value ? curatedById.get(value) : undefined;
  $: selectedIsFree = isFreeModel(selectedModel) || !!selectedCurated?.free;
  // The <select> only reflects a curated pick; typing a custom id (or
  // picking one not in the curated set) falls back to its blank option so
  // the two controls never fight over which one is "selected".
  $: curatedSelectValue = value && curatedById.has(value) ? value : '';

  function displayName(id: string): string {
    return modelById.get(id)?.name ?? id;
  }

  function onCuratedChange(event: Event & { currentTarget: HTMLSelectElement }) {
    if (event.currentTarget.value) value = event.currentTarget.value;
  }
</script>

<div class="openrouter-picker">
  {#if loading}
    <p class="path-hint">{$translate('openrouter.loadingModels')}</p>
  {:else if error}
    <p class="path-status" data-status="error">{error}</p>
  {:else if grouped.length > 0}
    <select value={curatedSelectValue} onchange={onCuratedChange}>
      <option value="">{$translate('openrouter.picker.custom')}</option>
      {#each grouped as group (group.category)}
        <optgroup label={group.category}>
          {#each group.items as item (item.id)}
            <option value={item.id} disabled={!item.available}>
              {displayName(item.id)} — {item.recommendation}{item.free
                ? ` · ${$translate('openrouter.freeTag')}`
                : ''}{!item.available ? ` (${$translate('openrouter.unavailable')})` : ''}
            </option>
          {/each}
        </optgroup>
      {/each}
    </select>
  {/if}
  <input
    bind:value
    list={datalistId}
    placeholder={$translate('openrouter.picker.customPlaceholder')}
  />
  {#if selectedModel}
    <p class="capability-hint">
      {#if selectedModel.contextLength}{formatContextLength(selectedModel.contextLength)}
        {$translate('openrouter.capability.context')}{/if}
      {#if selectedModel.vision}· 🖼 {$translate('openrouter.capability.vision')}{/if}
      {#if selectedModel.tools}· 🛠 {$translate('openrouter.capability.tools')}{/if}
    </p>
  {/if}
  {#if value && selectedIsFree}
    <p class="free-warning">{$translate('openrouter.freeWarning')}</p>
  {/if}
</div>

<style>
  .openrouter-picker {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .openrouter-picker select,
  .openrouter-picker input {
    min-width: 0;
    padding: 9px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--surface-2);
    color: var(--text);
    font: inherit;
  }
  .capability-hint {
    margin: 0;
    color: var(--muted);
    font-size: 0.7rem;
  }
  .free-warning {
    margin: 0;
    color: var(--yellow);
    font-size: 0.7rem;
    line-height: 1.4;
  }
</style>
