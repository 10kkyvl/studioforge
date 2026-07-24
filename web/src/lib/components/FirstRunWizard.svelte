<script lang="ts">
  import { CheckCircle2, RefreshCw, ShieldAlert } from '@lucide/svelte';
  import { translate, type TranslationKey } from '$lib/i18n';
  import type { Check } from '$lib/types';

  export let checks: Check[];
  export let safeMode: boolean;
  export let busy: string;
  export let onRefresh: () => void;
  export let onComplete: () => void;

  $: hasMissing = checks.some((check) => check.status !== 'ok');

  const knownCheckLabels: Record<string, TranslationKey> = {
    claude: 'check.claude',
    git: 'check.git',
    openrouter: 'check.openrouter',
    rojo: 'check.rojo',
    studioMcp: 'check.studioMcp',
    mcp: 'check.studioMcp',
    database: 'check.database',
    dataDirectory: 'check.dataDirectory',
  };
  function checkLabel(t: (key: TranslationKey) => string, check: Check): string {
    const key = knownCheckLabels[check.name];
    return key ? t(key) : check.name;
  }
</script>

<div class="modal-backdrop">
  <div class="wizard" role="dialog" aria-modal="true" aria-labelledby="wizard-title">
    <header>
      <div class="forge-mark">SF</div>
      <div>
        <h1 id="wizard-title">{$translate('wizard.title')}</h1>
        <p>{$translate('wizard.subtitle')}</p>
      </div>
    </header>
    {#if safeMode}<div class="safe-banner">
        <ShieldAlert size={18} />{$translate('wizard.safeMode')}
      </div>{/if}
    <div class="checks">
      {#each checks as check}
        <article>
          <span class:ok={check.status === 'ok'} class="check-icon"
            >{#if check.status === 'ok'}<CheckCircle2 size={20} />{:else}<ShieldAlert
                size={20}
              />{/if}</span
          >
          <div>
            <strong>{checkLabel($translate, check)}</strong>
            <p>{check.version || check.message}</p>
            {#if check.path}<code>{check.path}</code>{/if}
          </div>
          <span class={`status status-${check.status === 'ok' ? 'completed' : 'waiting_resources'}`}
            >{check.status === 'ok'
              ? $translate('wizard.detected')
              : $translate('wizard.missing')}</span
          >
        </article>
      {/each}
    </div>
    {#if hasMissing}<p class="wizard-hint">{$translate('wizard.canContinue')}</p>{/if}
    <footer>
      <button onclick={onRefresh}><RefreshCw size={16} />{$translate('wizard.recheck')}</button
      ><button class="primary" onclick={onComplete} disabled={busy === 'wizard'}
        >{$translate('wizard.complete')}</button
      >
    </footer>
  </div>
</div>
