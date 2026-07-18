<script lang="ts">
  import { Bot, CircleDollarSign, Gauge, GitBranch, Play, Waypoints } from '@lucide/svelte';
  import { formatMoney, locale, translate } from '$lib/i18n';
  import type { Project, Snapshot } from '$lib/types';

  export let snapshot: Snapshot;
  export let project: Project | undefined;
  export let busy: string;
  export let onRun: () => void;
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('overview.title')}</p>
    <h1>{project?.name ?? $translate('overview.noSelection')}</h1>
    <p>{project?.description}</p>
  </div>
  {#if project}<button
      class="primary"
      onclick={onRun}
      disabled={snapshot.settings.safeMode || busy === `run-${project.id}`}
      ><Play size={17} />{$translate('overview.startRun')}</button
    >{/if}
</section>
{#if project}
  <section class="overview-grid">
    <article class="panel hero-panel">
      <div class="panel-icon"><Gauge /></div>
      <div>
        <span>{$translate('overview.health')}</span>
        <h2>{$translate('common.verified')}</h2>
        <p>{project.path}</p>
      </div>
    </article>
    <article class="panel">
      <GitBranch /><span>{$translate('overview.git')}</span><strong
        >{$translate('common.active')}</strong
      >
    </article>
    <article class="panel">
      <Waypoints /><span>{$translate('overview.rojo')}</span><strong
        >{snapshot.diagnostics.dependencies.rojo?.status ?? $translate('common.none')}</strong
      >
    </article>
    <article class="panel">
      <Bot /><span>{$translate('overview.studio')}</span><strong
        >{snapshot.studios.find((studio) => studio.projectId === project?.id)?.name ??
          $translate('common.none')}</strong
      >
    </article>
    <article class="panel budget-panel">
      <CircleDollarSign /><span>{$translate('common.budget')}</span><strong
        >{formatMoney(project.budgetUsed, $locale)} / {formatMoney(
          project.budgetLimit,
          $locale,
        )}</strong
      >
    </article>
  </section>
{/if}
