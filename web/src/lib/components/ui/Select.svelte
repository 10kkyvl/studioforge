<script lang="ts">
  import { ChevronDown, Check } from '@lucide/svelte';
  import { tick } from 'svelte';
  import type { SelectOption } from './select';

  export let value: string = '';
  export let options: SelectOption[] = [];
  export let label: string = '';
  export let placeholder: string = '';
  export let disabled: boolean = false;
  export let id: string | undefined = undefined;
  export let name: string | undefined = undefined;
  export let className: string = '';
  export let onchange: ((value: string) => void) | undefined = undefined;

  let open = false;
  let triggerEl: HTMLButtonElement;
  let listEl: HTMLDivElement | undefined;
  let activeIndex = -1;
  let typeahead = '';
  let typeaheadTimer: ReturnType<typeof setTimeout> | undefined;
  let popupStyle = '';

  const listId = `sel-${Math.random().toString(36).slice(2, 9)}`;

  $: selected = options.find((option) => option.value === value);
  $: display = selected?.label ?? placeholder;

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return {
      destroy() {
        node.remove();
      },
    };
  }

  function position() {
    if (!triggerEl) return;
    const rect = triggerEl.getBoundingClientRect();
    const room = window.innerHeight - rect.bottom;
    const maxHeight = Math.min(320, Math.max(room - 12, 160));
    const dropUp = room < 180 && rect.top > room;
    const top = dropUp ? '' : `top:${rect.bottom + 6}px;`;
    const bottom = dropUp ? `bottom:${window.innerHeight - rect.top + 6}px;` : '';
    const width = Math.max(rect.width, 180);
    const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8));
    popupStyle =
      `left:${left}px;min-width:${rect.width}px;${top}${bottom}` +
      `max-height:${dropUp ? Math.min(320, rect.top - 12) : maxHeight}px;`;
  }

  async function openList() {
    if (disabled || open) return;
    position();
    open = true;
    activeIndex = options.findIndex((option) => option.value === value);
    if (activeIndex < 0) activeIndex = options.findIndex((option) => !option.disabled);
    await tick();
    scrollActiveIntoView();
  }

  function closeList(refocus = true) {
    if (!open) return;
    open = false;
    activeIndex = -1;
    if (refocus) triggerEl?.focus();
  }

  function scrollActiveIntoView() {
    listEl?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' });
  }

  function commit(option: SelectOption) {
    if (option.disabled) return;
    value = option.value;
    onchange?.(option.value);
    closeList();
  }

  function step(delta: number) {
    if (!options.length) return;
    let next = activeIndex;
    for (let i = 0; i < options.length; i += 1) {
      next = (next + delta + options.length) % options.length;
      if (!options[next].disabled) {
        activeIndex = next;
        void tick().then(scrollActiveIntoView);
        return;
      }
    }
  }

  function edge(toEnd: boolean) {
    const order = toEnd ? [...options].reverse() : options;
    const found = order.find((option) => !option.disabled);
    if (!found) return;
    activeIndex = options.indexOf(found);
    void tick().then(scrollActiveIntoView);
  }

  function matchTypeahead(key: string) {
    clearTimeout(typeaheadTimer);
    typeahead += key.toLowerCase();
    typeaheadTimer = setTimeout(() => (typeahead = ''), 600);
    const found = options.findIndex(
      (option) => !option.disabled && option.label.toLowerCase().startsWith(typeahead),
    );
    if (found < 0) return;
    if (open) {
      activeIndex = found;
      void tick().then(scrollActiveIntoView);
    } else {
      commit(options[found]);
    }
  }

  function onKeydown(event: KeyboardEvent) {
    if (disabled) return;
    switch (event.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        event.preventDefault();
        if (!open) void openList();
        else step(event.key === 'ArrowDown' ? 1 : -1);
        return;
      case 'Home':
      case 'End':
        if (!open) return;
        event.preventDefault();
        edge(event.key === 'End');
        return;
      case 'Enter':
      case ' ':
        event.preventDefault();
        if (!open) void openList();
        else if (activeIndex >= 0) commit(options[activeIndex]);
        return;
      case 'Escape':
        if (!open) return;
        event.preventDefault();
        closeList();
        return;
      case 'Tab':
        closeList(false);
        return;
      default:
        if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
          matchTypeahead(event.key);
        }
    }
  }

  function onPointerDownOutside(event: PointerEvent) {
    if (!open) return;
    const target = event.target as Node;
    if (triggerEl?.contains(target) || listEl?.contains(target)) return;
    closeList(false);
  }
</script>

<svelte:window
  on:pointerdown={onPointerDownOutside}
  on:resize={() => (open ? position() : undefined)}
/>

<button
  bind:this={triggerEl}
  {id}
  type="button"
  role="combobox"
  class={`sf-select ${className}`}
  class:is-open={open}
  class:is-placeholder={!selected}
  {disabled}
  aria-label={label || undefined}
  aria-haspopup="listbox"
  aria-expanded={open}
  aria-controls={open ? listId : undefined}
  aria-activedescendant={open && activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
  data-value={value}
  data-name={name}
  title={selected?.title ?? display}
  onclick={() => (open ? closeList() : openList())}
  onkeydown={onKeydown}
>
  <span class="sf-select-value">{display}</span>
  <ChevronDown class="sf-select-chevron" size={14} aria-hidden="true" />
</button>

{#if open}
  <div
    bind:this={listEl}
    use:portal
    id={listId}
    role="listbox"
    aria-label={label || undefined}
    class="sf-listbox"
    style={popupStyle}
  >
    {#each options as option, index (option.value + index)}
      <button
        id={`${listId}-${index}`}
        type="button"
        tabindex="-1"
        role="option"
        aria-selected={option.value === value}
        aria-disabled={option.disabled || undefined}
        class="sf-option"
        class:is-disabled={option.disabled}
        data-active={index === activeIndex}
        title={option.title}
        onclick={() => commit(option)}
        onpointerenter={() => (option.disabled ? null : (activeIndex = index))}
      >
        <span class="sf-option-check">
          {#if option.value === value}<Check size={13} aria-hidden="true" />{/if}
        </span>
        <span class="sf-option-body">
          <span class="sf-option-label">{option.label}</span>
          {#if option.hint}<span class="sf-option-hint">{option.hint}</span>{/if}
        </span>
      </button>
    {/each}
  </div>
{/if}

<style>
  .sf-select {
    display: inline-flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    width: 100%;
    min-height: 30px;
    padding: 0 var(--sp-2) 0 var(--sp-3);
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--text);
    font-family: var(--font-sans);
    font-size: var(--fs-sm);
    font-weight: 400;
    letter-spacing: 0;
    text-align: left;
    text-transform: none;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease),
      background-color var(--dur-fast) var(--ease);
  }
  .sf-select:hover:not(:disabled) {
    border-color: color-mix(in srgb, var(--heat-1) 45%, var(--line));
    background: var(--surface-3);
  }
  .sf-select.is-open {
    border-color: color-mix(in srgb, var(--heat-1) 60%, var(--line));
  }
  .sf-select.is-placeholder .sf-select-value {
    color: var(--muted);
  }
  .sf-select-value {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sf-select :global(.sf-select-chevron) {
    flex: none;
    color: var(--muted);
    transition: transform var(--dur-base) var(--ease);
  }
  .sf-select.is-open :global(.sf-select-chevron) {
    transform: rotate(180deg);
    color: var(--heat-1);
  }
  .sf-listbox {
    position: fixed;
    z-index: 80;
    display: flex;
    flex-direction: column;
    gap: 1px;
    max-width: min(420px, calc(100vw - 24px));
    padding: var(--sp-1);
    border: 1px solid color-mix(in srgb, var(--heat-1) 22%, var(--line));
    border-radius: var(--r-md);
    background: var(--surface-2);
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, white 7%, transparent),
      var(--shadow-3);
    overflow-y: auto;
    overscroll-behavior: contain;
    animation: sf-listbox-in var(--dur-fast) var(--ease-out);
  }
  @keyframes sf-listbox-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
  .sf-option:focus-visible {
    outline: none;
    box-shadow: none;
  }
  .sf-option {
    display: flex;
    border: 0;
    font-family: var(--font-sans);
    align-items: center;
    gap: var(--sp-2);
    padding: 6px var(--sp-2);
    border-radius: var(--r-sm);
    color: var(--text-dim);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .sf-option[data-active='true'] {
    background: color-mix(in srgb, var(--heat-1) 16%, var(--surface-3));
    color: var(--text);
  }
  .sf-option[aria-selected='true'] {
    color: var(--text);
    font-weight: 500;
  }
  .sf-option.is-disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .sf-option-check {
    display: grid;
    place-items: center;
    width: 14px;
    flex: none;
    color: var(--heat-1);
  }
  .sf-option-body {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .sf-option-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sf-option-hint {
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--fs-2xs);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
