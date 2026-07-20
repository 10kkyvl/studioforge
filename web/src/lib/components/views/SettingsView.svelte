<script lang="ts">
  import { onMount } from 'svelte';
  import { Bot, Database, Languages, Moon, RefreshCw, Save, Search, Sun } from '@lucide/svelte';
  import { detectPaths } from '$lib/api';
  import { locale, translate, type Locale, type TranslationKey } from '$lib/i18n';
  import type { AppSettings, DetectedPaths, Diagnostics, ToolCandidate } from '$lib/types';

  export let diagnostics: Diagnostics;
  export let settings: AppSettings;
  export let theme: string;
  export let busy: string;
  export let onLocale: (locale: Locale) => void;
  export let onTheme: (theme: string) => void;
  export let onRefresh: () => void;
  export let onBackup: () => void;
  export let onSave: (settings: AppSettings) => void;

  type PathField = { key: keyof AppSettings; label: TranslationKey; placeholder: string };
  const pathFields: PathField[] = [
    { key: 'codex_path', label: 'settings.codexPath', placeholder: 'codex' },
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

  onMount(() => {
    void runDetection('empty');
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
  <article class="settings-card">
    <header>
      {#if theme === 'dark'}<Moon />{:else}<Sun />{/if}
      <h2>{$translate('settings.theme')}</h2>
    </header>
    <div class="segmented">
      <button class:active={theme === 'dark'} onclick={() => onTheme('dark')}
        >{$translate('settings.dark')}</button
      ><button class:active={theme === 'light'} onclick={() => onTheme('light')}
        >{$translate('settings.light')}</button
      ><button class:active={theme === 'system'} onclick={() => onTheme('system')}
        >{$translate('settings.system')}</button
      >
    </div>
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
          ><option value="codex">Codex</option><option value="claude">Claude Code</option><option
            value="mock">Mock</option
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
          ><option value="low">low</option><option value="medium">medium</option><option
            value="high">high</option
          ><option value="xhigh">xhigh</option></select
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
        <dd>{diagnostics.database}</dd>
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
          <div><strong>{check.name}</strong><span class="chip">{check.status}</span></div>
          <code>{check.path || id}</code>
          {#if check.version}<small>{check.version}</small>{/if}
          {#if check.message}<p>{check.message}</p>{/if}
          {#if check.help}<p class="help">{check.help}</p>{/if}
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
