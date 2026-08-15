<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { getRunDiffStructured, resolveReview, reviewActionErrorMessage } from '$lib/api';
  import { translate } from '$lib/i18n';
  import {
    emptySelection,
    selectionCount,
    toApiSelection,
    type RollbackSelectionState,
  } from '$lib/rollbackSelection';
  import {
    formatReviewCountdown,
    isReviewExpired,
    reviewExpiresAtMs,
    reviewRemainingMs,
  } from '$lib/review';
  import StructuredDiff from '$lib/components/StructuredDiff.svelte';
  import type { Run, RunDiffStructured, RunEvent, ReviewAction } from '$lib/types';

  export let run: Run;
  export let events: RunEvent[] = [];
  export let reviewGateExpiryHours: number = 24;
  export let onResolved: () => void = () => {};

  let diff: RunDiffStructured | null = null;
  let loading = false;
  let selection: RollbackSelectionState = emptySelection();
  let loadedForRunId = '';
  let confirmAction: ReviewAction | null = null;
  let resolving = false;
  let lastAction: ReviewAction | null = null;
  let resolveError = '';
  let resolvedStatus = '';

  async function loadDiff(runId: string) {
    loading = true;
    try {
      const result = await getRunDiffStructured(runId);
      if (runId === loadedForRunId) diff = result;
    } catch {
      if (runId === loadedForRunId) diff = null;
    } finally {
      if (runId === loadedForRunId) loading = false;
    }
  }

  $: if (run.id !== loadedForRunId) {
    loadedForRunId = run.id;
    diff = null;
    selection = emptySelection();
    confirmAction = null;
    resolving = false;
    resolveError = '';
    resolvedStatus = '';
    void loadDiff(run.id);
  }

  let nowMs = Date.now();
  let tickTimer: ReturnType<typeof setInterval> | undefined;

  onMount(() => {
    tickTimer = setInterval(() => (nowMs = Date.now()), 1000);
  });

  onDestroy(() => {
    if (tickTimer) clearInterval(tickTimer);
  });

  $: expiresAtMs = reviewExpiresAtMs(run, events, reviewGateExpiryHours);
  $: expired = isReviewExpired(expiresAtMs, nowMs);
  $: countdown = formatReviewCountdown(reviewRemainingMs(expiresAtMs, nowMs));
  $: selCount = selectionCount(selection);

  function confirmTitle(action: ReviewAction | null): string {
    if (action === 'reject') return $translate('review.confirmRejectTitle');
    if (action === 'apply-selected') return $translate('review.confirmApplySelectedTitle');
    return '';
  }

  function confirmBody(action: ReviewAction | null): string {
    if (action === 'reject') return $translate('review.confirmRejectBody');
    if (action === 'apply-selected') return $translate('review.confirmApplySelectedBody');
    return '';
  }

  function resolvedMessage(status: string): string {
    if (status === 'applied') return $translate('review.resolvedApplied');
    if (status === 'rejected') return $translate('review.resolvedRejected');
    if (status === 'partial') return $translate('review.resolvedPartial');
    return status;
  }

  async function resolve(action: ReviewAction) {
    if (resolving) return;
    resolving = true;
    resolveError = '';
    lastAction = action;
    const runId = run.id;
    try {
      const result = await resolveReview(runId, action, toApiSelection(selection));
      if (runId === loadedForRunId) {
        resolvedStatus = result.status;
        confirmAction = null;
      }
      onResolved();
    } catch (cause) {
      if (runId === loadedForRunId) resolveError = reviewActionErrorMessage(cause, $translate);
    } finally {
      if (runId === loadedForRunId) resolving = false;
    }
  }
</script>

<div class="review-card">
  <header class="review-header">
    <strong>{$translate('review.heading')}</strong>
    {#if !expired}
      <span class="review-countdown">{$translate('review.remaining')}: {countdown}</span>
    {/if}
  </header>
  {#if expired}
    <p class="review-expired-heading">{$translate('review.expiredHeading')}</p>
    <p class="review-explain">{$translate('review.expiredExplain')}</p>
  {:else}
    <p class="review-explain">{$translate('review.explain')}</p>
    {#if loading}
      <p class="review-loading">{$translate('common.loading')}</p>
    {:else if diff}
      {#if diff.studioDirectEdits}
        <p class="review-studio-warning">{$translate('review.studioDirectEditsWarning')}</p>
      {/if}
      {#if diff.files.length > 0}
        <p class="review-select-hint">{$translate('review.selectHint')}</p>
        <StructuredDiff
          stats={diff.stats}
          files={diff.files}
          note={diff.note ?? ''}
          compact
          selectable
          bind:selection
          selectFileLabel={$translate('review.selectFileLabel')}
          selectHunkLabel={$translate('review.selectHunkLabel')}
        />
      {/if}
    {/if}
    {#if resolvedStatus}
      <p class="review-resolved">{resolvedMessage(resolvedStatus)}</p>
    {:else}
      {#if resolveError}
        <p class="review-error">{resolveError}</p>
      {/if}
      {#if confirmAction}
        <div class="review-confirm">
          <p class="review-confirm-title">{confirmTitle(confirmAction)}</p>
          <p class="review-confirm-body">{confirmBody(confirmAction)}</p>
          <div class="review-actions">
            <button type="button" onclick={() => (confirmAction = null)} disabled={resolving}>
              {$translate('common.cancel')}
            </button>
            <button
              type="button"
              class="danger"
              disabled={resolving}
              onclick={() => void resolve(confirmAction as ReviewAction)}
            >
              {resolving ? $translate('review.working') : confirmTitle(confirmAction)}
            </button>
          </div>
        </div>
      {:else}
        <div class="review-actions">
          <button
            type="button"
            class="primary"
            disabled={resolving}
            onclick={() => void resolve('apply')}
          >
            {resolving && lastAction === 'apply'
              ? $translate('review.working')
              : $translate('review.apply')}
          </button>
          <button
            type="button"
            class="danger"
            disabled={resolving}
            onclick={() => (confirmAction = 'reject')}
          >
            {$translate('review.reject')}
          </button>
          <button
            type="button"
            disabled={resolving || (selCount.files === 0 && selCount.hunks === 0)}
            onclick={() => (confirmAction = 'apply-selected')}
          >
            {$translate('review.applySelected')}
          </button>
        </div>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .review-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 8px 0;
    padding: 12px;
    border: 1px solid var(--warning);
    border-radius: var(--r-md);
    background: color-mix(in srgb, var(--warning) 10%, var(--surface-2));
  }
  .review-header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }
  .review-countdown {
    color: var(--muted);
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
  }
  .review-explain {
    margin: 0;
    color: var(--text-dim);
    font-size: var(--fs-sm);
  }
  .review-expired-heading {
    margin: 0;
    color: var(--muted);
    font-weight: 600;
  }
  .review-loading {
    margin: 0;
    color: var(--muted);
    font-size: var(--fs-sm);
  }
  .review-studio-warning {
    margin: 0;
    padding: 6px 8px;
    border-radius: var(--r-sm);
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--text);
    font-size: var(--fs-xs);
  }
  .review-select-hint {
    margin: 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .review-error {
    margin: 0;
    color: var(--danger);
    font-size: var(--fs-sm);
  }
  .review-resolved {
    margin: 0;
    color: var(--text);
    font-size: var(--fs-sm);
  }
  .review-confirm {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .review-confirm-title {
    margin: 0;
    font-weight: 600;
  }
  .review-confirm-body {
    margin: 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .review-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
