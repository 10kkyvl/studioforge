<script lang="ts">
  import { formatDate, locale, translate } from '$lib/i18n';
  import type { Run, RunEvent } from '$lib/types';

  export let runs: Run[];
  export let selectedRunId: string;
  export let selectedRun: Run | undefined;
  export let events: RunEvent[];
  export let projectName: (id: string) => string;
  export let agentName: (id: string) => string;
  export let statusLabel: (status: string) => string;
  export let validationLabel: (validation: string) => string;
  export let payloadText: (payload: unknown) => string;
  export let onSend: (prompt: string) => void = () => {};
  export let busy = false;
  let draft = '';

  function send() {
    const text = draft.trim();
    if (!text) return;
    onSend(text);
    draft = '';
  }

  // A run that scheduled a correction carries its own parentRunId only on the
  // correction, not on the parent — so "has this run got a correction" is a
  // reverse lookup over the whole list rather than a field on the run itself.
  function correctionFor(runId: string): Run | undefined {
    return runs.find((candidate) => candidate.parentRunId === runId);
  }
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.runs')}</p>
    <h1>{$translate('runs.title')}</h1>
    <p>{$translate('runs.subtitle')}</p>
  </div>
</section>
<section class="runs-layout">
  <div class="run-list">
    {#each runs as run}
      {@const correction = correctionFor(run.id)}
      <div
        class="run-row"
        class:active={selectedRunId === run.id}
        role="button"
        tabindex="0"
        onclick={() => (selectedRunId = run.id)}
        onkeydown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            selectedRunId = run.id;
          }
        }}
      >
        <span class={`status-dot status-${run.status}`}></span>
        <div>
          <strong>{projectName(run.projectId)}</strong><small
            >{agentName(run.agentId)} · {statusLabel(run.status)}</small
          >
          {#if run.validation && run.validation !== 'none'}
            <small class={`validation-badge validation-${run.validation}`}
              >{validationLabel(run.validation)}</small
            >
          {/if}
          {#if run.parentRunId}
            <small class="correction-link">
              {$translate('runs.correctionOf')}
              <button
                type="button"
                class="link"
                onclick={(event) => {
                  event.stopPropagation();
                  selectedRunId = run.parentRunId ?? selectedRunId;
                }}>{run.parentRunId.slice(0, 8)}</button
              >
            </small>
          {:else if correction}
            <small class="correction-link">
              {$translate('runs.hasCorrection')}
              <button
                type="button"
                class="link"
                onclick={(event) => {
                  event.stopPropagation();
                  selectedRunId = correction.id;
                }}>{correction.id.slice(0, 8)}</button
              >
            </small>
          {/if}
        </div>
        <time>{formatDate(run.updatedAt, $locale)}</time>
      </div>
    {/each}
  </div>
  <article class="event-panel">
    <header>
      <div>
        <h2>{$translate('runs.events')}</h2>
        {#if selectedRun}<span class={`status status-${selectedRun.status}`}
            >{statusLabel(selectedRun.status)}</span
          >
          {#if selectedRun.validation && selectedRun.validation !== 'none'}
            <span class={`status validation-badge validation-${selectedRun.validation}`}
              >{validationLabel(selectedRun.validation)}</span
            >
          {/if}
        {/if}
      </div>
      {#if selectedRun}<code>{selectedRun.id}</code>{/if}
    </header>
    <div class="event-log" aria-live="polite">
      {#if !selectedRun}<div class="empty">{$translate('runs.select')}</div>
      {:else if events.length === 0}<div class="empty">{$translate('runs.noEvents')}</div>
      {:else}{#each events as event}<div class={`event event-${event.type}`}>
            <time
              >{new Intl.DateTimeFormat($locale, { timeStyle: 'medium' }).format(
                new Date(event.createdAt),
              )}</time
            ><span>{event.rawType || event.type}</span>
            <p>{payloadText(event.payload)}</p>
          </div>{/each}{/if}
    </div>
    <form
      class="composer"
      onsubmit={(e) => {
        e.preventDefault();
        send();
      }}
    >
      <textarea
        bind:value={draft}
        rows="2"
        placeholder={$translate('runs.composerPlaceholder')}
        onkeydown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            send();
          }
        }}
      ></textarea>
      <button class="primary" type="submit" disabled={busy || !draft.trim()}
        >{$translate('runs.send')}</button
      >
    </form>
  </article>
</section>

<style>
  .composer {
    display: flex;
    gap: 0.5rem;
    align-items: flex-end;
    padding: 0.75rem;
    border-top: 1px solid var(--border, rgba(255, 255, 255, 0.08));
  }
  .composer textarea {
    flex: 1;
    resize: vertical;
    min-height: 2.5rem;
    font: inherit;
    padding: 0.5rem 0.65rem;
    border-radius: 0.5rem;
    border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
    background: var(--surface-2, rgba(255, 255, 255, 0.04));
    color: inherit;
  }
  .composer button {
    white-space: nowrap;
  }
  .validation-badge {
    display: inline-block;
    margin-left: 0.4rem;
    padding: 0.05rem 0.4rem;
    border-radius: 999px;
    font-size: 0.7rem;
    background: var(--surface-2, rgba(255, 255, 255, 0.08));
  }
  .validation-passed {
    color: var(--success, #2f9e5b);
  }
  .validation-failed,
  .validation-correction_failed {
    color: var(--danger, #d64545);
  }
  .validation-inconclusive {
    color: var(--muted, #9a9a9a);
  }
  .validation-corrected {
    color: var(--accent, #4a8fe7);
  }
  .correction-link {
    display: block;
    margin-top: 0.15rem;
    opacity: 0.8;
  }
  .correction-link .link {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent, #4a8fe7);
    text-decoration: underline;
    cursor: pointer;
    font: inherit;
  }
</style>
