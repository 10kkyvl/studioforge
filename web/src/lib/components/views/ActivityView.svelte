<script lang="ts">
  import {
    cacheTokens,
    formatDate,
    formatMoney,
    formatTokens,
    locale,
    spendTokens,
    translate,
  } from '$lib/i18n';
  import { isLegacyProvider } from '$lib/models';
  import type { Run } from '$lib/types';

  export let runs: Run[];
  export let projectName: (id: string) => string;
  export let agentName: (id: string) => string;
  export let statusLabel: (status: string) => string;
  export let onRunAction: (run: Run, command: string) => void;
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.activity')}</p>
    <h1>{$translate('activity.title')}</h1>
    <p>{$translate('activity.subtitle')}</p>
  </div>
</section>
<div class="table-wrap">
  <table>
    <thead
      ><tr
        ><th>{$translate('common.status')}</th><th>{$translate('common.project')}</th><th
          >{$translate('common.agent')}</th
        ><th>{$translate('common.model')}</th><th>{$translate('activity.resource')}</th><th
          >{$translate('activity.tokens')}</th
        ><th>{$translate('common.budget')}</th><th>{$translate('projects.updated')}</th><th
          >{$translate('common.actions')}</th
        ></tr
      ></thead
    >
    <tbody>
      {#each runs as run}
        {@const spend = spendTokens(run)}
        {@const cache = cacheTokens(run)}
        {@const legacy = isLegacyProvider(run.provider)}
        <tr>
          <td
            ><span class={`status status-${run.status}`} title={run.error || undefined}
              >{statusLabel(run.status)}</span
            ></td
          >
          <td>{projectName(run.projectId)}</td><td>{agentName(run.agentId)}</td>
          <td
            ><code>{run.provider}/{run.modelAlias}</code>
            {#if legacy}
              <span class="chip" title={$translate('run.legacyProviderHint')}
                >{$translate('run.legacyProvider')}</span
              >
            {/if}</td
          ><td
            >{#if run.requiredResource}<code>{run.requiredResource}</code>{/if}</td
          >
          <td>
            {#if spend > 0 || cache > 0}
              <div class="token-cell">
                <span>{formatTokens(spend, $locale)}</span>
                {#if cache > 0}
                  <span class="token-cache"
                    >{$translate('common.cache')} {formatTokens(cache, $locale)}</span
                  >
                {/if}
              </div>
            {:else}
              —
            {/if}
          </td>
          <td>{formatMoney(run.cost, $locale)}</td>
          <td>{formatDate(run.updatedAt, $locale)}</td>
          <td
            ><div class="row-actions">
              {#if run.status === 'running'}
                <button onclick={() => onRunAction(run, 'pause')}
                  >{$translate('common.pause')}</button
                >
                <button class="danger" onclick={() => onRunAction(run, 'cancel')}
                  >{$translate('common.cancel')}</button
                >
              {:else if run.status === 'paused'}
                <button
                  disabled={legacy}
                  title={legacy ? $translate('run.legacyProviderHint') : undefined}
                  onclick={() => onRunAction(run, 'resume')}>{$translate('common.resume')}</button
                >
                <button class="danger" onclick={() => onRunAction(run, 'cancel')}
                  >{$translate('common.cancel')}</button
                >
              {:else if run.status === 'waiting_decision'}
                <button class="danger" onclick={() => onRunAction(run, 'cancel')}
                  >{$translate('common.cancel')}</button
                >
              {:else if ['interrupted', 'failed', 'cancelled'].includes(run.status)}
                <button
                  disabled={legacy}
                  title={legacy ? $translate('run.legacyProviderHint') : undefined}
                  onclick={() => onRunAction(run, 'restart')}>{$translate('common.restart')}</button
                >
              {/if}
            </div></td
          >
        </tr>
      {:else}<tr><td colspan="9" class="empty-cell">{$translate('activity.empty')}</td></tr>{/each}
    </tbody>
  </table>
</div>

<style>
  .token-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  /* Cache rides under spend in the same cell, smaller and dimmer — a
     secondary reading, not a second column competing with it. */
  .token-cache {
    font-size: var(--fs-2xs);
    color: var(--muted);
  }
</style>
