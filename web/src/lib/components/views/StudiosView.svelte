<script lang="ts">
  import { translate } from '$lib/i18n';
  import Select from '$lib/components/ui/Select.svelte';
  import type { Project, StudioSession } from '$lib/types';

  export let studios: StudioSession[];
  export let projects: Project[];
  export let projectName: (id: string) => string;
  export let onBind: (sessionId: string, projectId: string) => void;
  // onRecognise records this instance's reported name as the bound project's
  // roblox.com place name. A place opened from Roblox is reported by that name
  // and by no file name, so without it a run refuses the very Studio the
  // operator is working in.
  export let onRecognise: (projectId: string, placeName: string) => void = () => {};
  // detected is true until a refresh reports otherwise, so a daemon that has
  // never run a real discovery pass (e.g. --mock, or before the operator's
  // first click) does not read as "Studio MCP not detected" by default.
  export let detected = true;
  export let onRefresh: () => void = () => {};
  export let busy = false;

  function playStateLabel(state: string): string {
    if (state === 'play' || state === 'playing') return $translate('state.playing');
    if (state === 'edit' || state === 'editing') return $translate('state.editing');
    return state;
  }
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.studios')}</p>
    <h1>{$translate('studios.title')}</h1>
    <p>{$translate('studios.subtitle')}</p>
  </div>
  <button class="primary" type="button" onclick={onRefresh} disabled={busy}>
    {busy ? $translate('studios.refreshing') : $translate('studios.refresh')}
  </button>
</section>
{#if !detected}
  <div class="empty">{$translate('studios.notDetected')}</div>
{:else}
  <section class="studio-grid">
    {#each studios as studio}
      <article class="studio-card">
        <header>
          <div class="studio-light" class:active={studio.active}></div>
          <div>
            <h2>{studio.name}</h2>
            <code>{studio.instanceId}</code>
          </div>
          {#if studio.mock}<span class="chip">{$translate('common.mock')}</span>{/if}
        </header>
        <dl>
          <div>
            <dt>{$translate('studios.place')}</dt>
            <dd>{studio.placeId || '—'}</dd>
          </div>
          <div>
            <dt>{$translate('studios.playState')}</dt>
            <dd>{playStateLabel(studio.playState)}</dd>
          </div>
          <div>
            <dt>{$translate('common.project')}</dt>
            <dd>{studio.projectId ? projectName(studio.projectId) : $translate('common.none')}</dd>
          </div>
        </dl>
        <label
          >{$translate('studios.bind')}<Select
            value={studio.projectId ?? ''}
            label={$translate('studios.bind')}
            options={[
              { value: '', label: $translate('common.none') },
              ...projects.map((project) => ({ value: project.id, label: project.name })),
            ]}
            onchange={(next) => onBind(studio.id, next)}
          /></label
        >
        {#if studio.projectId && studio.name}
          <button
            class="recognise"
            type="button"
            title={$translate('studios.recogniseHint')}
            onclick={() => onRecognise(studio.projectId ?? '', studio.name)}
            >{$translate('studios.recognise')}</button
          >
        {/if}
      </article>
    {:else}<div class="empty">{$translate('studios.empty')}</div>{/each}
  </section>
{/if}
