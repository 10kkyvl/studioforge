<script lang="ts">
  import { getUsage } from '$lib/api';
  import { formatMoney, formatDate, translate, locale } from '$lib/i18n';
  import type { Project } from '$lib/types';
  import { usageCSV, usageGroups, usageTokens, usageClass, type UsageReport } from '$lib/usage';
  export let project: Project | undefined;
  export let onOpenRun: (id: string) => void;
  let report: UsageReport | null = null;
  let error = '';
  let loading = false;
  let loaded = '';
  let generation = 0;
  let group: 'agentId' | 'model' | 'provider' = 'agentId';
  let period: 'days' | 'weeks' = 'days';
  let sort: 'cost' | 'tokens' | 'date' = 'cost';
  $: if ((project?.id ?? '') !== loaded) {
    loaded = project?.id ?? '';
    report = null;
    error = '';
    generation++;
    if (loaded) void load(loaded);
  }
  async function load(id: string) {
    const request = ++generation;
    loading = true;
    error = '';
    try {
      const value = await getUsage(id);
      if (request === generation) report = value;
    } catch (e) {
      if (request === generation) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (request === generation) loading = false;
    }
  }
  $: groups = usageGroups(report?.runs ?? [], group);
  $: sorted = [...(report?.runs ?? [])].sort((a, b) =>
    sort === 'cost'
      ? b.cost - a.cost
      : sort === 'tokens'
        ? usageTokens(b) - usageTokens(a)
        : b.createdAt.localeCompare(a.createdAt),
  );
  $: buckets = report?.[period] ?? [];
  $: maxCost = Math.max(...buckets.map((b) => b.cost), 0.01);
  $: paid = report?.runs.filter((r) => usageClass(r) === 'paid').length ?? 0;
  $: zero = report?.runs.filter((r) => usageClass(r) === 'zero').length ?? 0;
  $: unknown = report?.runs.filter((r) => usageClass(r) === 'unknown').length ?? 0;
  $: free = report?.runs.filter((r) => usageClass(r) === 'free').length ?? 0;
  function download() {
    const url = URL.createObjectURL(
      new Blob(['\uFEFF' + usageCSV(sorted)], { type: 'text/csv;charset=utf-8' }),
    );
    const link = document.createElement('a');
    link.href = url;
    link.download = 'studioforge-usage.csv';
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
</script>

{#if project}
  <section class="usage-view" data-testid="usage-view">
    <header>
      <h2>{$translate('usage.title')}</h2>
      <button disabled={loading} onclick={() => load(project!.id)}
        >{$translate('usage.refresh')}</button
      ><button disabled={!report} onclick={download}>CSV</button>
    </header>
    {#if error}<p role="alert">{error}</p>{/if}
    {#if loading}<p role="status">{$translate('common.loading')}</p>{/if}
    {#if report}
      <div class="summary">
        <article>
          <span>{$translate('usage.total')}</span><strong
            >{formatMoney(report.total, $locale)}</strong
          >
        </article>
        <article>
          <span>{$translate('usage.last24')}</span><strong
            >{formatMoney(report.last24Hours, $locale)} / {report.dailyLimit > 0
              ? formatMoney(report.dailyLimit, $locale)
              : $translate('usage.noLimit')}</strong
          >
        </article>
        <article>
          <span>{$translate('usage.typical')}</span><strong
            >{report.paceSamples > 0 ? `${Math.round(report.typicalSeconds)} s` : '—'}</strong
          ><small>{report.paceSamples} {$translate('usage.samples')}</small>
        </article>
      </div>
      <p>
        {$translate('usage.free')}: {free} · {$translate('usage.paid')}: {paid} · {$translate(
          'usage.zero',
        )}: {zero} · {$translate('usage.unknown')}: {unknown}
      </p>
      <p class="muted">{$translate('usage.accounting')}</p>
      <h3>{$translate('usage.time')}</h3>
      <label
        >{$translate('usage.period')}
        <select aria-label={$translate('usage.period')} bind:value={period}
          ><option value="days">{$translate('usage.days')}</option><option value="weeks"
            >{$translate('usage.weeks')}</option
          ></select
        ></label
      >
      <div class="spend-chart">
        {#each buckets as bucket}<div class="bar-row">
            <span>{bucket.key}</span><meter
              min="0"
              max={maxCost}
              value={bucket.cost}
              aria-label={bucket.key}
            ></meter><span>{formatMoney(bucket.cost, $locale)}</span>
          </div>{/each}
        {#if !buckets.length}<p>{$translate('usage.empty')}</p>{/if}
      </div>
      <h3>{$translate('usage.breakdown')}</h3>
      <label
        >{$translate('usage.group')}
        <select aria-label={$translate('usage.group')} bind:value={group}
          ><option value="agentId">{$translate('usage.agent')}</option><option value="model"
            >{$translate('usage.model')}</option
          ><option value="provider">{$translate('usage.provider')}</option></select
        ></label
      >
      <div class="table-scroll">
        <table>
          <thead
            ><tr
              ><th>{$translate('usage.group')}</th><th>{$translate('usage.cost')}</th><th
                >{$translate('usage.tokens')}</th
              ><th>{$translate('usage.runs')}</th></tr
            ></thead
          ><tbody
            >{#each groups as item}<tr
                ><td>{item.name}</td><td>{formatMoney(item.cost, $locale)}</td><td
                  >{item.tokens.toLocaleString()}</td
                ><td>{item.runs}</td></tr
              >{/each}</tbody
          >
        </table>
      </div>
      <h3>{$translate('usage.runs')}</h3>
      <label
        >{$translate('usage.sort')}
        <select aria-label={$translate('usage.sort')} bind:value={sort}
          ><option value="cost">{$translate('usage.cost')}</option><option value="tokens"
            >{$translate('usage.tokens')}</option
          ><option value="date">{$translate('usage.date')}</option></select
        ></label
      >
      <div class="table-scroll">
        <table>
          <thead
            ><tr
              ><th>{$translate('usage.date')}</th><th>{$translate('usage.agent')}</th><th
                >{$translate('usage.provider')}</th
              ><th>{$translate('usage.model')}</th><th>{$translate('usage.cost')}</th><th
                >{$translate('usage.tokens')}</th
              ></tr
            ></thead
          ><tbody
            >{#each sorted as run}<tr
                ><td
                  ><button class="run-link" onclick={() => onOpenRun(run.id)}
                    >{formatDate(run.createdAt, $locale)}</button
                  ></td
                ><td>{run.agentName}</td><td>{run.provider}</td><td
                  >{run.model}<small> · {$translate(`usage.${usageClass(run)}`)}</small></td
                ><td
                  >{run.recorded ? formatMoney(run.cost, $locale) : $translate('usage.unknown')}</td
                ><td>{usageTokens(run).toLocaleString()}</td></tr
              >{/each}</tbody
          >
        </table>
      </div>
    {/if}
  </section>
{/if}

<style>
  .usage-view {
    display: grid;
    gap: 1rem;
    min-width: 0;
  }
  header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  h2 {
    margin-right: auto;
  }
  h3,
  p {
    margin: 0;
  }
  .summary {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 1rem;
  }
  .summary article {
    display: grid;
    gap: 0.4rem;
    padding: 1rem;
    border: 1px solid var(--border);
    border-radius: 12px;
  }
  .summary strong {
    font-size: 1.3rem;
  }
  .muted,
  small {
    color: var(--muted);
  }
  .table-scroll {
    overflow: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    text-align: left;
  }
  th,
  td {
    padding: 0.65rem;
    border-bottom: 1px solid var(--border);
  }
  .bar-row {
    display: grid;
    grid-template-columns: 7rem minmax(60px, 1fr) 6rem;
    gap: 0.75rem;
    align-items: center;
  }
  meter {
    width: 100%;
  }
  .spend-chart {
    max-height: 320px;
    overflow: auto;
  }
  button,
  select {
    font: inherit;
    color: inherit;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.45rem 0.65rem;
  }
  button {
    cursor: pointer;
  }
  button:disabled {
    cursor: default;
    opacity: 0.5;
  }
  button:hover:not(:disabled) {
    background: var(--surface-2);
  }
  .run-link {
    text-align: left;
    background: transparent;
    border: 0;
    padding: 0;
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  label {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
</style>
