<script lang="ts">
  import Select from '$lib/components/ui/Select.svelte';
  import {
    formatContextLength,
    groupCuratedByCategory,
    openRouterCompatibility,
  } from '$lib/openrouter';
  import { translate } from '$lib/i18n';
  import type { OpenRouterCurated, OpenRouterModel } from '$lib/types';

  // Two-way bound by the caller (bind:value), exactly like the free-text
  // model input it replaces — the value is a model id passed straight
  // through to the API, never a StudioForge-side tier. Optional because the
  // agent-create draft (Partial<Agent>) may not have picked a model yet.
  export let value: string | undefined;
  export let allowUnverified = false;
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
  $: compatibility = openRouterCompatibility(models, curated, value);
  // The <select> only reflects a curated pick; typing a custom id (or
  // picking one not in the curated set) falls back to its blank option so
  // the two controls never fight over which one is "selected".
  $: curatedSelectValue = value && curatedById.has(value) ? value : '';

  function displayName(id: string): string {
    return modelById.get(id)?.name ?? id;
  }

  function onCuratedChange(next: string) {
    if (next && next !== value) {
      allowUnverified = false;
      value = next;
    }
  }

  function onModelInput(event: Event & { currentTarget: HTMLInputElement }) {
    const next = event.currentTarget.value;
    if (next !== value) allowUnverified = false;
    value = next;
  }
</script>

<div class="openrouter-picker">
  {#if loading}
    <p class="path-hint">{$translate('openrouter.loadingModels')}</p>
  {:else if error}
    <p class="path-status" data-status="error">{error}</p>
  {:else if grouped.length > 0}
    <Select
      value={curatedSelectValue}
      label={$translate('common.model')}
      options={[
        { value: '', label: $translate('openrouter.picker.custom') },
        ...grouped.flatMap((group) =>
          group.items.map((item) => ({
            value: item.id,
            label: displayName(item.id),
            disabled: !item.available && item.verified,
            hint: `${group.category} · ${item.recommendation}${
              item.free ? ` · ${$translate('openrouter.freeTag')}` : ''
            }${!item.available ? ` (${$translate('openrouter.unavailable')})` : ''}`,
          })),
        ),
      ]}
      onchange={onCuratedChange}
    />
  {/if}
  <input
    value={value ?? ''}
    oninput={onModelInput}
    list={datalistId}
    placeholder={$translate('openrouter.picker.customPlaceholder')}
  />
  {#if value}
    <p class="capability-hint">
      {#if compatibility.contextLength}{formatContextLength(compatibility.contextLength)}
        {$translate('openrouter.capability.context')}{/if}
      · {compatibility.vision ? '✓' : '✕'}
      {$translate('openrouter.capability.vision')}
      · {compatibility.tools ? '✓' : '✕'}
      {$translate('openrouter.capability.tools')}
    </p>
  {/if}
  {#if value && !compatibility.verified && (!compatibility.known || compatibility.tools)}
    <label class="compatibility-warning">
      <input type="checkbox" bind:checked={allowUnverified} />
      <span>{$translate('openrouter.compatibilityWarning')}</span>
    </label>
  {/if}
  {#if value && compatibility.known && !compatibility.tools}
    <p class="incompatible-warning">{$translate('openrouter.incompatibleModel')}</p>
  {/if}
  {#if value && compatibility.free}
    <p class="free-warning">{$translate('openrouter.freeWarning')}</p>
  {/if}
</div>

<style>
  .openrouter-picker {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .openrouter-picker input {
    min-width: 0;
    padding: 9px;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--text);
    font: inherit;
  }
  .capability-hint {
    margin: 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .free-warning {
    margin: 0;
    color: var(--yellow);
    font-size: var(--fs-xs);
    line-height: 1.4;
  }
  .compatibility-warning {
    display: flex;
    gap: 7px;
    color: var(--yellow);
    font-size: var(--fs-xs);
    line-height: 1.4;
  }
  .incompatible-warning {
    margin: 0;
    color: var(--danger);
    font-size: var(--fs-xs);
    line-height: 1.4;
  }
</style>
