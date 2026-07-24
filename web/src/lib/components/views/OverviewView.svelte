<script lang="ts">
  import { Bot, CircleDollarSign, Gauge, GitBranch, Play, Plug, Waypoints } from '@lucide/svelte';
  import { formatMoney, locale, translate, type TranslationKey } from '$lib/i18n';
  import type { Project, Snapshot } from '$lib/types';

  export let snapshot: Snapshot;
  export let project: Project | undefined;
  export let busy: string;
  export let onRun: () => void;

  $: studioMcpCheck = snapshot.diagnostics.dependencies.studioMcp;
  $: lastRun = project
    ? [...snapshot.runs]
        .filter((run) => run.projectId === project?.id)
        .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())[0]
    : undefined;

  function runStatusLabel(status: string): string {
    const key = `status.${status}` as TranslationKey;
    return $translate(key) || status;
  }

  const STATE_KEYS: Record<string, TranslationKey> = {
    ok: 'state.ok',
    missing: 'state.missing',
    present: 'state.present',
    active: 'state.active',
    stopped: 'state.stopped',
    none: 'state.none',
  };
  function stateLabel(raw: string | undefined): string {
    if (raw === undefined) return $translate('common.none');
    const key = STATE_KEYS[raw];
    return key ? $translate(key) : raw;
  }
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
        <span>{$translate('overview.lastRun')}</span>
        <h2>{lastRun ? runStatusLabel(lastRun.status) : $translate('overview.noData')}</h2>
        <p>{project.path}</p>
      </div>
    </article>
    <article class="panel">
      <GitBranch /><span>{$translate('overview.git')}</span><strong
        >{$translate('overview.noData')}</strong
      >
    </article>
    <article class="panel">
      <Waypoints /><span>{$translate('overview.rojo')}</span><strong
        >{stateLabel(snapshot.diagnostics.dependencies.rojo?.status)}</strong
      >
    </article>
    <article class="panel">
      <Waypoints /><span>{$translate('overview.rojoSync')}</span><strong
        >{project.sync.active
          ? $translate('overview.rojoSyncRunning')
          : $translate('overview.rojoSyncStopped')}</strong
      >
      {#if project.sync.active}
        <p class="panel-hint">{$translate('overview.rojoSyncPort')}: {project.sync.port}</p>
        {#if project.sync.recentLogs?.length}
          <pre class="log-lines">{project.sync.recentLogs.join('\n')}</pre>
        {/if}
      {/if}
    </article>
    <article class="panel" class:panel-warning={studioMcpCheck?.status === 'missing'}>
      <Plug /><span>{$translate('overview.studioMcp')}</span><strong
        >{stateLabel(studioMcpCheck?.status)}</strong
      >
      {#if studioMcpCheck?.status === 'missing'}
        <p class="panel-hint">{$translate('overview.studioMcpMissing')}</p>
      {/if}
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
