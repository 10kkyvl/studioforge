<script lang="ts">
  import { X } from '@lucide/svelte';
  import { translate } from '$lib/i18n';

  export let value: {
    name: string;
    path: string;
    description: string;
    create: boolean;
    openStudio?: boolean;
  };
  export let busy: string;
  export let onClose: () => void;
  export let onSubmit: () => void;
</script>

<div class="modal-backdrop">
  <dialog open class="project-modal" aria-labelledby="new-project-title">
    <form
      onsubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <header>
        <h2 id="new-project-title">{$translate('projects.new')}</h2>
        <button
          type="button"
          class="icon-button"
          aria-label={$translate('common.close')}
          onclick={onClose}><X /></button
        >
      </header>
      <label>{$translate('projects.name')}<input bind:value={value.name} required /></label>
      <label>{$translate('project.folderLabel')}<input bind:value={value.path} required /></label>
      <p class="path-hint">{$translate('project.folderHint')}</p>
      <label
        >{$translate('projects.description')}<textarea bind:value={value.description} rows="3"
        ></textarea></label
      >
      <label class="checkbox"
        ><input type="checkbox" bind:checked={value.create} /><span
          >{$translate('projects.createDirectory')}</span
        ></label
      >
      <label class="checkbox"
        ><input type="checkbox" bind:checked={value.openStudio} /><span
          >{$translate('projects.openAfterCreate')}</span
        ></label
      >
      <footer>
        <button type="button" onclick={onClose}>{$translate('common.cancel')}</button><button
          class="primary"
          type="submit"
          disabled={busy === 'project'}>{$translate('projects.create')}</button
        >
      </footer>
    </form>
  </dialog>
</div>
