<script lang="ts">
  import {
    AlertTriangle,
    CheckCircle2,
    CircleDashed,
    Info,
    RefreshCw,
    ShieldAlert,
    XCircle,
  } from '@lucide/svelte';
  import { translate, type TranslationKey } from '$lib/i18n';
  import {
    buildWizardChecks,
    checkHelpLabel,
    checkLabel,
    checkMessageLabel,
    deriveGates,
    type WizardCheck,
  } from '$lib/wizard';
  import type { Diagnostics } from '$lib/types';

  export let diagnostics: Diagnostics;
  export let safeMode: boolean;
  export let busy: string;
  export let onRefresh: () => void;
  export let onComplete: () => void;
  export let onOpenSettings: (anchor?: string) => void;

  $: allChecks = buildWizardChecks(diagnostics);
  $: requiredChecks = allChecks.filter((check) => check.severity === 'blocker');
  $: providerChecks = allChecks.filter((check) => check.severity === 'provider');
  $: integrationChecks = allChecks.filter((check) => check.severity === 'integration');
  $: sections = [
    { key: 'wizard.section.required' as TranslationKey, checks: requiredChecks, intro: false },
    { key: 'wizard.section.providers' as TranslationKey, checks: providerChecks, intro: true },
    {
      key: 'wizard.section.integrations' as TranslationKey,
      checks: integrationChecks,
      intro: false,
    },
  ];
  $: gates = deriveGates(allChecks, { mockMode: diagnostics.mockMode });
  $: isRefreshing = busy === 'refresh';
  let announcedRefresh = false;
  let liveMessage = '';
  $: {
    if (isRefreshing) {
      liveMessage = $translate('wizard.liveRegionChecking');
      announcedRefresh = true;
    } else if (announcedRefresh) {
      liveMessage = $translate('wizard.liveRegionDone');
      announcedRefresh = false;
    }
  }

  function statusLabel(check: WizardCheck): string {
    return $translate(`wizard.status.${check.status}` as TranslationKey);
  }

  function secondaryLine(check: WizardCheck): string {
    if (check.version) return check.version;
    if (check.raw.message) return checkMessageLabel($translate, check.raw.message);
    return '';
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
    {#if safeMode}
      <div class="safe-banner"><ShieldAlert size={18} />{$translate('wizard.safeModeExplain')}</div>
    {/if}
    {#if diagnostics.mockMode}
      <div class="mock-banner"><Info size={18} />{$translate('wizard.mockMode')}</div>
    {/if}
    {#if gates.blocked}
      <div class="blocked-banner" role="alert">
        <strong>{$translate('wizard.blockedTitle')}</strong>
        <p>{$translate('wizard.blockedBody')}</p>
      </div>
    {/if}
    {#each sections as section (section.key)}
      {#if section.checks.length}
        <section class="wizard-section">
          <h2>{$translate(section.key)}</h2>
          {#if section.intro}
            <p class="wizard-hint">{$translate('wizard.needProvider')}</p>
            <ul class="provider-guidance">
              <li>{$translate('wizard.providerGuidance.claude')}</li>
              <li>{$translate('wizard.providerGuidance.openrouter')}</li>
              <li>{$translate('wizard.providerGuidance.nvidia')}</li>
            </ul>
          {/if}
          <div class="checks">
            {#each section.checks as check (check.id)}
              <article data-check={check.id}>
                <span class="check-icon" data-status={check.status}>
                  {#if check.status === 'ok'}<CheckCircle2 size={20} />
                  {:else if check.status === 'warning'}<AlertTriangle size={20} />
                  {:else if check.status === 'error'}<XCircle size={20} />
                  {:else}<CircleDashed size={20} />{/if}
                </span>
                <div>
                  <strong>{checkLabel($translate, check.id, check.raw.name)}</strong>
                  <p>{secondaryLine(check)}</p>
                  {#if check.raw.help}
                    <p class="help">{checkHelpLabel($translate, check.raw.help)}</p>
                  {/if}
                  {#if check.path}<code>{check.path}</code>{/if}
                </div>
                <span class="check-actions">
                  <span class={`status status-${check.status}`}>{statusLabel(check)}</span>
                  {#if check.fixableInSettings}
                    <button
                      type="button"
                      class="link"
                      onclick={() => onOpenSettings(check.settingsAnchor)}
                      >{$translate('wizard.openInSettings')}</button
                    >
                  {/if}
                </span>
              </article>
            {/each}
          </div>
        </section>
      {/if}
    {/each}
    {#if !gates.blocked && gates.missingOptional.length > 0}
      <p class="wizard-hint">{$translate('wizard.canContinue')}</p>
    {/if}
    <div class="sr-only" aria-live="polite" role="status">{liveMessage}</div>
    <footer>
      <button onclick={onRefresh}><RefreshCw size={16} />{$translate('wizard.recheck')}</button
      ><button class="primary" onclick={onComplete} disabled={gates.blocked || busy === 'wizard'}
        >{gates.blocked
          ? $translate('wizard.blockedTitle')
          : gates.missingOptional.length === 0
            ? $translate('wizard.complete')
            : $translate('wizard.limitedModeContinue')}</button
      >
    </footer>
  </div>
</div>
