<script lang="ts">
  import { Archive, Bot, Boxes, FolderKanban, Play, Plus, Rocket, Search } from '@lucide/svelte';
  import { brand } from '$lib/brand';
  import {
    cacheTokens,
    formatMoney,
    formatTokens,
    locale,
    spendTokens,
    translate,
  } from '$lib/i18n';
  import type { Project } from '$lib/types';

  export let projects: Project[];
  export let busy: string;
  export let safeMode: boolean;
  export let search: string;
  export let onNew: () => void;
  export let onSelect: (project: Project) => void;
  export let onArchive: (project: Project) => void;
  export let onRun: (project: Project) => void;
  export let onOpenStudio: (project: Project) => void;

  $: activeProjects = projects.filter(
    (project) =>
      !project.archived &&
      `${project.name} ${project.description} ${(project.tags ?? []).join(' ')}`
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  $: hasAnyProjects = projects.some((project) => !project.archived);
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{brand.name}</p>
    <h1>{$translate('projects.title')}</h1>
    <p>{$translate('projects.subtitle')}</p>
  </div>
  <button class="primary" onclick={onNew}><Plus size={17} />{$translate('projects.new')}</button>
</section>
<div class="toolbar">
  <label class="search">
    <Search size={17} />
    <input
      bind:value={search}
      placeholder={$translate('common.search')}
      aria-label={$translate('common.search')}
    />
  </label>
  <span class="metric">{$translate('nav.projects')}: {activeProjects.length}</span>
</div>
<section class="project-grid" aria-live="polite">
  {#each activeProjects as project}
    <article class="project-card" class:mock-card={project.mock}>
      <div class="card-top">
        <div class="project-glyph"><Boxes size={22} /></div>
        <div class="card-menu">
          {#if project.mock}<span class="chip">{$translate('common.mock')}</span>{/if}
          <button
            class="icon-button"
            aria-label={project.archived
              ? $translate('common.restore')
              : $translate('common.archive')}
            onclick={() => onArchive(project)}
            disabled={busy === `archive-${project.id}`}><Archive size={16} /></button
          >
        </div>
      </div>
      <button class="card-title" onclick={() => onSelect(project)}><h2>{project.name}</h2></button>
      <p>{project.description}</p>
      <div class="tags">
        {#each project.tags ?? [] as tag}<span>{tag}</span>{/each}
      </div>
      <div class="budget">
        <div>
          <span>{$translate('common.budget')}</span>
          <strong
            >{formatMoney(project.budgetUsed, $locale)} / {formatMoney(
              project.budgetLimit,
              $locale,
            )}</strong
          >
        </div>
        <div class="bar">
          <i
            style={`width:${Math.min(100, project.budgetLimit ? (project.budgetUsed / project.budgetLimit) * 100 : 0)}%`}
          ></i>
        </div>
      </div>
      {#if spendTokens(project) > 0 || cacheTokens(project) > 0}
        <p class="token-line">
          {$translate('common.spend')}
          {formatTokens(spendTokens(project), $locale)}{#if cacheTokens(project) > 0}
            <span class="token-cache"
              >· {$translate('common.cache')} {formatTokens(cacheTokens(project), $locale)}</span
            >{/if}
        </p>
      {/if}
      <footer>
        <span><Bot size={15} />{project.runningAgents} {$translate('projects.running')}</span>
        <div class="card-actions">
          <button
            class="run-button ghost"
            onclick={() => onOpenStudio(project)}
            disabled={busy === `open-studio-${project.id}`}
            title={$translate('projects.openStudio')}
          >
            <Rocket size={15} />{$translate('projects.openStudio')}
          </button>
          <button
            class="run-button"
            onclick={() => onRun(project)}
            disabled={safeMode || busy === `run-${project.id}`}
          >
            <Play size={15} />{$translate('overview.startRun')}
          </button>
        </div>
      </footer>
    </article>
  {:else}
    <div class="empty">
      <FolderKanban size={32} />
      {#if hasAnyProjects}
        <p>{$translate('projects.empty')}</p>
      {:else}
        <p>{$translate('projects.emptyNone')}</p>
        <button class="primary" onclick={onNew}
          ><Plus size={17} />{$translate('projects.new')}</button
        >
      {/if}
    </div>
  {/each}
</section>

<style>
  /* Same "second line, quieter" treatment as the chat header and the
     activity table: spend reads first, cache trails it, dimmer. */
  .token-line {
    margin: 6px 0 0;
    font-size: 0.72rem;
    color: var(--muted);
  }
  .token-cache {
    opacity: 0.75;
  }
  .card-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .run-button.ghost {
    background: transparent;
    border: 1px solid var(--line);
    color: var(--text);
  }
  .run-button.ghost:hover:not(:disabled) {
    background: var(--surface-2);
  }
</style>
