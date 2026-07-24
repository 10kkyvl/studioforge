<script lang="ts">
  import { translate } from '$lib/i18n';
  import type { Project, StudioSession } from '$lib/types';

  export let studios: StudioSession[];
  export let projects: Project[];
  export let projectName: (id: string) => string;
  export let onBind: (sessionId: string, projectId: string) => void;
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
          >{$translate('studios.bind')}<select
            value={studio.projectId ?? ''}
            onchange={(event) => onBind(studio.id, event.currentTarget.value)}
            ><option value="">{$translate('common.none')}</option>{#each projects as project}<option
                value={project.id}>{project.name}</option
              >{/each}</select
          ></label
        >
      </article>
    {:else}<div class="empty">{$translate('studios.empty')}</div>{/each}
  </section>
{/if}
