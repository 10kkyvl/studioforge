<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ALargeSmall,
    Bot,
    ChevronDown,
    Database,
    KeyRound,
    Languages,
    Moon,
    Palette,
    PlugZap,
    RefreshCw,
    Save,
    Search,
    Sun,
    Trash2,
  } from '@lucide/svelte';
  import { detectPaths } from '$lib/api';
  import {
    getOpenRouterStatus,
    isRetryableOpenRouterTestError,
    removeOpenRouterKey,
    setOpenRouterKey,
    testOpenRouterKey,
  } from '$lib/openrouter';
  import {
    getNVIDIAStatus,
    isRetryableNVIDIATestError,
    removeNVIDIAKey,
    setNVIDIAKey,
    testNVIDIAKey,
  } from '$lib/nvidia';
  import { locale, translate, type Locale, type TranslationKey } from '$lib/i18n';
  import type {
    AppSettings,
    DetectedPaths,
    Diagnostics,
    OpenRouterStatus,
    ToolCandidate,
  } from '$lib/types';

  export let diagnostics: Diagnostics;
  export let settings: AppSettings;
  export let theme: string;
  export let fontSize: string;
  export let busy: string;
  export let onLocale: (locale: Locale) => void;
  export let onTheme: (theme: string) => void;
  export let onFontSize: (fontSize: string) => void;
  export let onRefresh: () => void;
  export let onBackup: () => void;
  export let onSave: (settings: AppSettings) => void;

  let orStatus: OpenRouterStatus | null = null;
  let orLoadingStatus = false;
  let orStatusError = '';
  let orKeyInput = '';
  let orSaving = false;
  let orRemoving = false;
  let orConfirmingRemove = false;
  let orTesting = false;
  let orTestOk: boolean | null = null;
  let orTestRetry = false;
  let orNotice = '';

  let nvStatus: OpenRouterStatus | null = null;
  let nvLoadingStatus = false;
  let nvStatusError = '';
  let nvKeyInput = '';
  let nvSaving = false;
  let nvRemoving = false;
  let nvConfirmingRemove = false;
  let nvTesting = false;
  let nvTestOk: boolean | null = null;
  let nvTestRetry = false;
  let nvNotice = '';

  async function loadOpenRouterStatus() {
    orLoadingStatus = true;
    orStatusError = '';
    try {
      orStatus = await getOpenRouterStatus();
    } catch (cause) {
      orStatusError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      orLoadingStatus = false;
    }
  }

  async function saveOpenRouterKey() {
    if (!orKeyInput.trim() || orSaving) return;
    orSaving = true;
    orStatusError = '';
    orNotice = '';
    orTestOk = null;
    orTestRetry = false;
    try {
      orStatus = await setOpenRouterKey(orKeyInput.trim());
      orKeyInput = '';
      orNotice = $translate('openrouter.keySaved');
    } catch (cause) {
      orStatusError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      orSaving = false;
    }
  }

  async function removeOpenRouterKeyClick() {
    if (orRemoving) return;
    orRemoving = true;
    orStatusError = '';
    orNotice = '';
    orTestOk = null;
    orTestRetry = false;
    try {
      await removeOpenRouterKey();
      orConfirmingRemove = false;
      orNotice = $translate('openrouter.keyRemoved');
      await loadOpenRouterStatus();
    } catch (cause) {
      orStatusError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      orRemoving = false;
    }
  }

  async function testOpenRouterConnection() {
    if (orTesting) return;
    orTesting = true;
    orStatusError = '';
    orNotice = '';
    orTestOk = null;
    orTestRetry = false;
    try {
      const result = await testOpenRouterKey();
      orStatus = result;
      orTestOk = result.ok;
    } catch (cause) {
      orStatusError = cause instanceof Error ? cause.message : String(cause);
      orTestRetry = isRetryableOpenRouterTestError(cause);
    } finally {
      orTesting = false;
    }
  }

  async function loadNVIDIAStatus() {
    nvLoadingStatus = true;
    nvStatusError = '';
    try {
      nvStatus = await getNVIDIAStatus();
    } catch (cause) {
      nvStatusError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      nvLoadingStatus = false;
    }
  }

  async function saveNVIDIAKey() {
    if (!nvKeyInput.trim() || nvSaving) return;
    nvSaving = true;
    nvStatusError = '';
    nvNotice = '';
    nvTestOk = null;
    nvTestRetry = false;
    try {
      nvStatus = await setNVIDIAKey(nvKeyInput.trim());
      nvKeyInput = '';
      nvNotice = $translate('nvidia.keySaved');
    } catch (cause) {
      nvStatusError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      nvSaving = false;
    }
  }

  async function removeNVIDIAKeyClick() {
    if (nvRemoving) return;
    nvRemoving = true;
    nvStatusError = '';
    nvNotice = '';
    nvTestOk = null;
    nvTestRetry = false;
    try {
      await removeNVIDIAKey();
      nvConfirmingRemove = false;
      nvNotice = $translate('nvidia.keyRemoved');
      await loadNVIDIAStatus();
    } catch (cause) {
      nvStatusError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      nvRemoving = false;
    }
  }

  async function testNVIDIAConnection() {
    if (nvTesting) return;
    nvTesting = true;
    nvStatusError = '';
    nvNotice = '';
    nvTestOk = null;
    nvTestRetry = false;
    try {
      const result = await testNVIDIAKey();
      nvStatus = result;
      nvTestOk = result.ok;
    } catch (cause) {
      nvStatusError = cause instanceof Error ? cause.message : String(cause);
      nvTestRetry = isRetryableNVIDIATestError(cause);
    } finally {
      nvTesting = false;
    }
  }

  type PathField = { key: keyof AppSettings; label: TranslationKey; placeholder: string };
  const pathFields: PathField[] = [
    { key: 'claude_path', label: 'settings.claudePath', placeholder: 'claude' },
    { key: 'rojo_path', label: 'settings.rojoPath', placeholder: 'rojo' },
    { key: 'git_path', label: 'settings.gitPath', placeholder: 'git' },
    { key: 'studio_mcp_path', label: 'settings.studioMcpPath', placeholder: '.../Roblox/mcp.bat' },
  ];

  let detected: DetectedPaths = {};
  let detecting = false;
  let detectError = '';
  // Tracks fields this component filled in, so the hint only claims credit for
  // values the operator has not yet saved.
  let autofilled = new Set<string>();

  // These take the map explicitly so the markup references `detected` directly.
  // A template expression only re-renders when a variable it names changes, so
  // calling best(field.key) would leave the status stale after detection lands.
  const best = (all: DetectedPaths, key: string): ToolCandidate | undefined =>
    all[key]?.find((candidate) => candidate.status === 'ok') ?? all[key]?.[0];

  const others = (all: DetectedPaths, key: string): ToolCandidate[] => {
    const top = best(all, key);
    return (all[key] ?? []).filter((candidate) => candidate.path !== top?.path);
  };

  async function runDetection(fill: 'empty' | 'none') {
    detecting = true;
    detectError = '';
    try {
      detected = await detectPaths();
      if (fill === 'empty') {
        for (const field of pathFields) {
          const candidate = best(detected, field.key);
          if (!settings[field.key] && candidate?.status === 'ok') {
            // Fill the form only. The stored setting stays empty until the
            // operator saves, so merely opening this page cannot pin a path that
            // a tool update would later invalidate.
            (settings as Record<string, unknown>)[field.key] = candidate.path;
            autofilled.add(field.key);
          }
        }
        settings = settings;
        autofilled = autofilled;
      }
    } catch (error) {
      detectError = error instanceof Error ? error.message : String(error);
    } finally {
      detecting = false;
    }
  }

  function apply(key: string, path: string) {
    (settings as Record<string, unknown>)[key] = path;
    autofilled.add(key);
    settings = settings;
    autofilled = autofilled;
  }

  function checkStatusLabel(status: string): string {
    const key = `state.${status}` as TranslationKey;
    return $translate(key) || status;
  }

  function checkNameLabel(id: string, name: string): string {
    const key = `check.${id}` as TranslationKey;
    return $translate(key) || name;
  }

  const checkHelpKeys: Record<string, TranslationKey> = {
    'Install Git or configure its executable path in Settings.': 'check.gitHelp',
    'Run `claude auth status`, then authenticate with Claude Code if needed.': 'check.claudeHelp',
    'Install Rojo 7 from the official Rojo documentation.': 'check.rojoHelp',
    'Update Roblox Studio, open Assistant settings, and enable Studio as MCP server.':
      'check.studioMcpHelp',
    'Add your OpenRouter API key in Settings and click Test connection.': 'check.openrouterHelp',
  };

  const checkMessageKeys: Record<string, TranslationKey> = {
    'Claude Code detected': 'check.claudeDetected',
    'Claude Code was not found. Install it, then run StudioForge doctor. Mock mode remains available.':
      'check.claudeNotFound',
    'Rojo CLI detected': 'check.rojoDetected',
    'Rojo CLI not found; install Rojo 7 and ensure rojo is on PATH': 'check.rojoNotFound',
    'Official Studio MCP launcher detected': 'check.studioMcpDetected',
  };

  function checkHelpLabel(help: string): string {
    const key = checkHelpKeys[help];
    return key ? $translate(key) : help;
  }

  function checkMessageLabel(message: string): string {
    const key = checkMessageKeys[message];
    return key ? $translate(key) : message;
  }

  onMount(() => {
    void runDetection('empty');
    void loadOpenRouterStatus();
    void loadNVIDIAStatus();
  });
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.settings')}</p>
    <h1>{$translate('settings.title')}</h1>
    <p>{$translate('settings.subtitle')}</p>
  </div>
</section>
<section class="settings-grid">
  <article class="settings-card">
    <header>
      <Languages />
      <h2>{$translate('settings.language')}</h2>
    </header>
    <div class="segmented">
      <button class:active={$locale === 'en'} onclick={() => onLocale('en')}>English</button><button
        class:active={$locale === 'ru'}
        onclick={() => onLocale('ru')}>Русский</button
      >
    </div>
  </article>
  <article class="settings-card appearance-card">
    <header>
      <Palette />
      <div>
        <h2>{$translate('settings.appearance')}</h2>
        <p>{$translate('settings.appearanceHint')}</p>
      </div>
    </header>
    <div class="preference-row">
      <span class="preference-label">
        {#if theme === 'dark'}<Moon size={16} />{:else}<Sun size={16} />{/if}
        {$translate('settings.theme')}
      </span>
      <div class="segmented" role="group" aria-label={$translate('settings.theme')}>
        <button
          class:active={theme === 'dark'}
          aria-pressed={theme === 'dark'}
          onclick={() => onTheme('dark')}>{$translate('settings.dark')}</button
        ><button
          class:active={theme === 'light'}
          aria-pressed={theme === 'light'}
          onclick={() => onTheme('light')}>{$translate('settings.light')}</button
        ><button
          class:active={theme === 'system'}
          aria-pressed={theme === 'system'}
          onclick={() => onTheme('system')}>{$translate('settings.system')}</button
        >
      </div>
    </div>
    <div class="preference-row">
      <span class="preference-label">
        <ALargeSmall size={17} />
        {$translate('settings.textSize')}
      </span>
      <div class="segmented" role="group" aria-label={$translate('settings.textSize')}>
        <button
          class:active={fontSize === 'compact'}
          aria-pressed={fontSize === 'compact'}
          onclick={() => onFontSize('compact')}>{$translate('settings.sizeCompact')}</button
        ><button
          class:active={fontSize === 'comfortable'}
          aria-pressed={fontSize === 'comfortable'}
          onclick={() => onFontSize('comfortable')}>{$translate('settings.sizeComfortable')}</button
        ><button
          class:active={fontSize === 'large'}
          aria-pressed={fontSize === 'large'}
          onclick={() => onFontSize('large')}>{$translate('settings.sizeLarge')}</button
        >
      </div>
    </div>
  </article>
  <article class="settings-card openrouter-card">
    <header>
      <KeyRound />
      <h2>{$translate('openrouter.title')}</h2>
    </header>
    {#if orLoadingStatus}
      <p class="path-hint">{$translate('common.loading')}</p>
    {:else if orStatus}
      <p class="or-state-row">
        <span class="chip or-state" data-state={orStatus.state}
          >{$translate(`openrouter.keyState.${orStatus.state}` as TranslationKey)}</span
        >
        <span class="or-source"
          >{$translate(`openrouter.source.${orStatus.source}` as TranslationKey)}</span
        >
      </p>
      {#if !orStatus.secure}
        <p class="path-status" data-status="missing">{$translate('openrouter.insecureWarning')}</p>
      {/if}
      {#if orTestOk !== null}
        <p class="or-test-result" data-ok={orTestOk}>
          {orTestOk ? $translate('openrouter.testOk') : $translate('openrouter.testFailed')}
        </p>
      {/if}
    {/if}
    {#if orStatusError}
      <p class="path-status" data-status="error">{orStatusError}</p>
      {#if orTestRetry}
        <button type="button" class="retry-button" onclick={testOpenRouterConnection}
          >{$translate('common.retry')}</button
        >
      {/if}
    {/if}
    {#if orNotice}
      <p class="or-notice">{orNotice}</p>
    {/if}
    <form
      class="or-key-form"
      onsubmit={(event) => {
        event.preventDefault();
        void saveOpenRouterKey();
      }}
    >
      <input
        type="password"
        autocomplete="off"
        bind:value={orKeyInput}
        placeholder={$translate('openrouter.keyPlaceholder')}
        aria-label={$translate('openrouter.keyLabel')}
      />
      <button class="primary" type="submit" disabled={orSaving || !orKeyInput.trim()}>
        <Save size={15} />{orSaving
          ? $translate('openrouter.saving')
          : orStatus && orStatus.state !== 'not_configured'
            ? $translate('openrouter.replace')
            : $translate('openrouter.save')}
      </button>
    </form>
    <footer>
      <button
        type="button"
        onclick={testOpenRouterConnection}
        disabled={orTesting || !orStatus || orStatus.state === 'not_configured'}
      >
        <PlugZap size={15} />{orTesting
          ? $translate('openrouter.testing')
          : $translate('openrouter.testConnection')}
      </button>
      {#if orConfirmingRemove}
        <button type="button" onclick={() => (orConfirmingRemove = false)} disabled={orRemoving}
          >{$translate('common.cancel')}</button
        ><button
          type="button"
          class="danger"
          onclick={removeOpenRouterKeyClick}
          disabled={orRemoving}
        >
          <Trash2 size={15} />{orRemoving
            ? $translate('openrouter.removing')
            : $translate('openrouter.removeConfirmButton')}
        </button>
      {:else}
        <button
          type="button"
          class="danger"
          onclick={() => (orConfirmingRemove = true)}
          disabled={!orStatus || orStatus.state === 'not_configured'}
        >
          <Trash2 size={15} />{$translate('openrouter.remove')}
        </button>
      {/if}
    </footer>
  </article>
  <article class="settings-card openrouter-card">
    <header>
      <KeyRound />
      <div>
        <h2>{$translate('nvidia.title')}</h2>
        <p>{$translate('nvidia.rateLimit')}</p>
      </div>
    </header>
    {#if nvLoadingStatus}
      <p class="path-hint">{$translate('common.loading')}</p>
    {:else if nvStatus}
      <p class="or-state-row">
        <span class="chip or-state" data-state={nvStatus.state}
          >{$translate(`openrouter.keyState.${nvStatus.state}` as TranslationKey)}</span
        >
        <span class="or-source"
          >{$translate(`openrouter.source.${nvStatus.source}` as TranslationKey)}</span
        >
      </p>
      {#if !nvStatus.secure}
        <p class="path-status" data-status="missing">{$translate('openrouter.insecureWarning')}</p>
      {/if}
      {#if nvTestOk !== null}
        <p class="or-test-result" data-ok={nvTestOk}>
          {nvTestOk ? $translate('openrouter.testOk') : $translate('openrouter.testFailed')}
        </p>
      {/if}
    {/if}
    {#if nvStatusError}
      <p class="path-status" data-status="error">{nvStatusError}</p>
      {#if nvTestRetry}
        <button type="button" class="retry-button" onclick={testNVIDIAConnection}
          >{$translate('common.retry')}</button
        >
      {/if}
    {/if}
    {#if nvNotice}<p class="or-notice">{nvNotice}</p>{/if}
    <form
      class="or-key-form"
      onsubmit={(event) => {
        event.preventDefault();
        void saveNVIDIAKey();
      }}
    >
      <input
        type="password"
        autocomplete="off"
        bind:value={nvKeyInput}
        placeholder={$translate('nvidia.keyPlaceholder')}
        aria-label={$translate('openrouter.keyLabel')}
      />
      <button class="primary" type="submit" disabled={nvSaving || !nvKeyInput.trim()}>
        <Save size={15} />{nvSaving
          ? $translate('openrouter.saving')
          : nvStatus && nvStatus.state !== 'not_configured'
            ? $translate('openrouter.replace')
            : $translate('openrouter.save')}
      </button>
    </form>
    <footer>
      <button
        type="button"
        onclick={testNVIDIAConnection}
        disabled={nvTesting || !nvStatus || nvStatus.state === 'not_configured'}
      >
        <PlugZap size={15} />{nvTesting
          ? $translate('openrouter.testing')
          : $translate('openrouter.testConnection')}
      </button>
      {#if nvConfirmingRemove}
        <button type="button" onclick={() => (nvConfirmingRemove = false)} disabled={nvRemoving}
          >{$translate('common.cancel')}</button
        ><button type="button" class="danger" onclick={removeNVIDIAKeyClick} disabled={nvRemoving}>
          <Trash2 size={15} />{nvRemoving
            ? $translate('openrouter.removing')
            : $translate('openrouter.removeConfirmButton')}
        </button>
      {:else}
        <button
          type="button"
          class="danger"
          onclick={() => (nvConfirmingRemove = true)}
          disabled={!nvStatus || nvStatus.state === 'not_configured'}
        >
          <Trash2 size={15} />{$translate('openrouter.remove')}
        </button>
      {/if}
    </footer>
  </article>
  <form
    class="settings-card integration-settings"
    onsubmit={(event) => {
      event.preventDefault();
      onSave({ ...settings });
    }}
  >
    <header>
      <Bot />
      <div>
        <h2>{$translate('settings.integrations')}</h2>
        <p>{$translate('settings.integrationsHint')}</p>
      </div>
    </header>
    <div class="settings-fields">
      <label
        >{$translate('settings.defaultProvider')}<select bind:value={settings.default_provider}
          ><option value="claude">Claude Code</option><option value="openrouter">OpenRouter</option
          ><option value="nvidia">NVIDIA NIM</option><option value="mock"
            >{$translate('provider.mock')}</option
          ></select
        ></label
      >
      <label
        >{$translate('settings.defaultModel')}<input
          bind:value={settings.default_model}
          placeholder={$translate('team.cliDefault')}
        /></label
      >
      <label
        >{$translate('settings.defaultEffort')}<select bind:value={settings.default_effort}
          ><option value="low">{$translate('effort.low')}</option><option value="medium"
            >{$translate('effort.medium')}</option
          ><option value="high">{$translate('effort.high')}</option><option value="xhigh"
            >{$translate('effort.xhigh')}</option
          ></select
        ></label
      >
      <label
        >{$translate('settings.concurrency')}<input
          type="number"
          min="1"
          max="32"
          bind:value={settings.concurrency}
        /></label
      >
      {#each pathFields as field (field.key)}
        {@const candidate = best(detected, field.key)}
        <div class="wide path-field">
          <label
            >{$translate(field.label)}
            <span class="path-input">
              <input bind:value={settings[field.key]} placeholder={field.placeholder} />
              <button
                type="button"
                class="detect"
                disabled={detecting}
                onclick={() => runDetection('none')}
              >
                <Search size={14} />{detecting
                  ? $translate('settings.detecting')
                  : $translate('settings.detect')}
              </button>
            </span>
          </label>
          {#if candidate}
            <p class="path-status" data-status={candidate.status}>
              <span class="chip">{$translate('settings.detected')}</span>
              <code>{candidate.path}</code>
              {#if candidate.version}<small>{candidate.version}</small>{/if}
              {#if candidate.message}<small>{candidate.message}</small>{/if}
              {#if !settings[field.key]}
                <button type="button" class="link" onclick={() => apply(field.key, candidate.path)}
                  >{$translate('settings.detect')}</button
                >
              {/if}
            </p>
            {#each others(detected, field.key) as other (other.path)}
              <p class="path-status alternative">
                <span class="chip">{$translate('settings.otherCandidates')}</span>
                <code>{other.path}</code>
                <button type="button" class="link" onclick={() => apply(field.key, other.path)}
                  >{$translate('settings.detect')}</button
                >
              </p>
            {/each}
          {:else if !detecting}
            <p class="path-status" data-status="missing">
              <span class="chip">{$translate('settings.notDetected')}</span>
              <small>{$translate('settings.pathHint')}</small>
            </p>
          {/if}
          {#if autofilled.has(field.key)}
            <p class="path-hint">{$translate('settings.autofilled')}</p>
          {/if}
        </div>
      {/each}
      <div class="wide path-field">
        <label class="checkbox"
          ><input
            type="checkbox"
            checked={settings.studio_auto_open !== 'false'}
            onchange={(event) =>
              (settings.studio_auto_open = event.currentTarget.checked ? 'true' : 'false')}
          /><span>{$translate('settings.studioAutoOpen')}</span></label
        >
        <p class="path-hint">{$translate('settings.studioAutoOpenHint')}</p>
      </div>
      <div class="wide path-field">
        <label
          >{$translate('settings.playtestWindow')}<input
            type="number"
            min="5"
            step="5"
            value={settings.playtest_window_seconds}
            onchange={(event) => (settings.playtest_window_seconds = event.currentTarget.value)}
          /></label
        >
        <p class="path-hint">{$translate('settings.playtestWindowHint')}</p>
      </div>
      <div class="wide path-field">
        <details class="advanced-routing">
          <summary><ChevronDown size={14} />{$translate('openrouter.routing.title')}</summary>
          <div class="settings-fields">
            <label
              >{$translate('openrouter.routing.dataCollection')}<select
                bind:value={settings.openrouter_data_collection}
                ><option value="">{$translate('openrouter.routing.providerDefault')}</option><option
                  value="allow">{$translate('openrouter.routing.allow')}</option
                ><option value="deny">{$translate('openrouter.routing.deny')}</option></select
              ></label
            >
            <label
              >{$translate('openrouter.routing.zdr')}<select bind:value={settings.openrouter_zdr}
                ><option value="">{$translate('openrouter.routing.providerDefault')}</option><option
                  value="true">{$translate('openrouter.routing.on')}</option
                ><option value="false">{$translate('openrouter.routing.off')}</option></select
              ></label
            >
            <label
              >{$translate('openrouter.routing.allowFallbacks')}<select
                bind:value={settings.openrouter_allow_fallbacks}
                ><option value="">{$translate('openrouter.routing.providerDefault')}</option><option
                  value="true">{$translate('openrouter.routing.on')}</option
                ><option value="false">{$translate('openrouter.routing.off')}</option></select
              ></label
            >
          </div>
        </details>
      </div>
      {#if detectError}
        <p class="wide path-status" data-status="error">
          {$translate('settings.detectFailed')}: {detectError}
        </p>
      {/if}
    </div>
    <footer>
      <button type="button" onclick={() => runDetection('none')} disabled={detecting}
        ><Search size={16} />{$translate('settings.detectAll')}</button
      ><button class="primary" type="submit" disabled={busy === 'settings'}
        ><Save size={16} />{$translate('common.save')}</button
      >
    </footer>
  </form>
  <article class="settings-card diagnostics-card">
    <header>
      <Database />
      <h2>{$translate('settings.diagnostics')}</h2>
    </header>
    <dl>
      <div>
        <dt>{$translate('settings.database')}</dt>
        <dd>{checkStatusLabel(diagnostics.database)}</dd>
      </div>
      <div>
        <dt>{$translate('settings.wal')}</dt>
        <dd>{diagnostics.wal ? $translate('common.active') : $translate('common.none')}</dd>
      </div>
      <div>
        <dt>{$translate('settings.fts')}</dt>
        <dd>{diagnostics.fts5 ? $translate('common.active') : $translate('common.none')}</dd>
      </div>
      <div>
        <dt>{$translate('settings.safe')}</dt>
        <dd>{diagnostics.safeMode ? $translate('common.active') : $translate('common.none')}</dd>
      </div>
      <div>
        <dt>{$translate('settings.dataPath')}</dt>
        <dd><code>{diagnostics.dataPath}</code></dd>
      </div>
    </dl>
    <h3>{$translate('settings.status')}</h3>
    <div class="dependency-grid">
      {#each Object.entries(diagnostics.dependencies) as [id, check]}
        <section class="dependency" data-status={check.status}>
          <div>
            <strong>{checkNameLabel(id, check.name)}</strong><span class="chip"
              >{checkStatusLabel(check.status)}</span
            >
          </div>
          <code>{check.path || id}</code>
          {#if check.version}<small>{check.version}</small>{/if}
          {#if check.message}<p>{checkMessageLabel(check.message)}</p>{/if}
          {#if check.help}<p class="help">{checkHelpLabel(check.help)}</p>{/if}
        </section>
      {/each}
    </div>
    <footer>
      <button onclick={onRefresh}><RefreshCw size={16} />{$translate('wizard.recheck')}</button
      ><button class="primary" onclick={onBackup} disabled={busy === 'backup'}
        ><Database size={16} />{$translate('settings.backup')}</button
      >
    </footer>
  </article>
</section>
