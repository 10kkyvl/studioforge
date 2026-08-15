<script lang="ts">
  import Select from '$lib/components/ui/Select.svelte';
  import { onMount } from 'svelte';
  import { Bot, Play, Plus, Save } from '@lucide/svelte';
  import { formatMoney, locale, translate, type TranslationKey } from '$lib/i18n';
  import { isLegacyProvider, modelsFor } from '$lib/models';
  import { getOpenRouterModels } from '$lib/openrouter';
  import OpenRouterModelPicker from '$lib/components/OpenRouterModelPicker.svelte';
  import type { Agent, Diagnostics, OpenRouterModelsResponse, Project } from '$lib/types';

  export let agents: Agent[];
  export let project: Project | undefined;
  export let busy: string;
  export let diagnostics: Diagnostics | undefined = undefined;
  export let onCreate: (agent: Partial<Agent>) => void;
  export let onUpdate: (agent: Agent) => void;
  export let onRun: (agent: Agent) => void;

  function networkPolicyIsStrict(policy: string | undefined): boolean {
    return !!policy && policy !== 'unrestricted';
  }
  $: networkPolicyUnenforced = diagnostics ? diagnostics.os !== 'darwin' : true;

  // What a profile actually grants differs by provider and, on Claude, by what
  // its permission mode honours — so the picker says it at the point of
  // choosing rather than leaving the operator to read SECURITY.md.
  function permissionHint(
    permission: string | undefined,
  ): 'perm.readOnlyHint' | 'perm.workspaceWriteHint' | 'perm.dangerFullHint' {
    if (permission === 'read-only') return 'perm.readOnlyHint';
    if (permission === 'danger-full-access') return 'perm.dangerFullHint';
    return 'perm.workspaceWriteHint';
  }

  // Fetched once and shared by every openrouter model picker instance
  // (the create form and every agent row), rather than each one issuing its
  // own /openrouter/models request.
  let orModels: OpenRouterModelsResponse | null = null;
  let orLoading = false;
  let orError = '';

  async function loadOpenRouterModels() {
    orLoading = true;
    orError = '';
    try {
      orModels = await getOpenRouterModels();
    } catch (cause) {
      orError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      orLoading = false;
    }
  }

  onMount(() => {
    void loadOpenRouterModels();
  });

  let showCreate = false;
  const blankDraft: Partial<Agent> = {
    name: '',
    role: $translate('team.defaultRole'),
    provider: 'claude',
    modelAlias: 'default',
    allowUnverifiedModel: false,
    effort: 'medium',
    permission: 'workspace-write',
    networkPolicy: 'unrestricted',
    concurrency: 1,
    budget: 10,
    validateAfterRun: false,
    maxCorrectionRuns: 1,
    reviewBeforeApply: false,
  };
  let draft: Partial<Agent> = { ...blankDraft };

  function create() {
    onCreate(draft);
    draft = { ...blankDraft };
    showCreate = false;
  }
  const EFFORTS = ['low', 'medium', 'high', 'xhigh'] as const;
  $: providerOptions = [
    { value: 'claude', label: 'Claude Code' },
    { value: 'openrouter', label: 'OpenRouter' },
    { value: 'nvidia', label: 'NVIDIA NIM' },
    { value: 'mock', label: $translate('provider.mock') },
  ];
  $: effortOptions = EFFORTS.map((level) => ({
    value: level,
    label: $translate(`effort.${level}` as TranslationKey),
  }));
  $: permissionOptions = [
    { value: 'read-only', label: $translate('perm.readOnly') },
    { value: 'workspace-write', label: $translate('perm.workspaceWrite') },
    { value: 'danger-full-access', label: $translate('perm.dangerFull') },
  ];
  $: networkPolicyOptions = [
    { value: 'unrestricted', label: $translate('netpolicy.unrestricted') },
    { value: 'registry-only', label: $translate('netpolicy.registryOnly') },
    { value: 'none', label: $translate('netpolicy.none') },
  ];
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.team')}</p>
    <h1>{$translate('team.title')}</h1>
    {#if !project}<span class="chip">{$translate('common.allProjects')}</span>{/if}
    <p>{$translate('team.subtitle')}</p>
  </div>
  {#if project}<button class="primary" onclick={() => (showCreate = !showCreate)}
      ><Plus size={17} />{$translate('team.add')}</button
    >{/if}
</section>
{#if showCreate && project}
  <form
    class="agent-editor create-agent"
    onsubmit={(event) => {
      event.preventDefault();
      create();
    }}
  >
    <label>{$translate('team.name')}<input bind:value={draft.name} required /></label>
    <label>{$translate('team.role')}<input bind:value={draft.role} required /></label>
    <label
      >{$translate('team.provider')}<Select
        bind:value={draft.provider}
        label={$translate('team.provider')}
        options={providerOptions}
      /></label
    >
    {#if draft.provider === 'openrouter'}
      <label class="field-span-2"
        >{$translate('common.model')}<OpenRouterModelPicker
          bind:value={draft.modelAlias}
          bind:allowUnverified={draft.allowUnverifiedModel}
          models={orModels?.models ?? []}
          curated={orModels?.curated ?? []}
          categories={orModels?.categories ?? []}
          loading={orLoading}
          error={orError}
          datalistId="models-openrouter"
        /></label
      >
    {:else}
      <label
        >{$translate('common.model')}<input
          bind:value={draft.modelAlias}
          list={`models-${draft.provider}`}
          placeholder={$translate('team.cliDefault')}
        /></label
      >
    {/if}
    <label
      >{$translate('team.effort')}<Select
        bind:value={draft.effort}
        label={$translate('team.effort')}
        options={effortOptions}
      /></label
    >
    <label
      >{$translate('team.permission')}<Select
        bind:value={draft.permission}
        label={$translate('team.permission')}
        options={permissionOptions}
      /></label
    >
    <p class="path-hint" class:danger-hint={draft.permission === 'danger-full-access'}>
      {$translate(permissionHint(draft.permission))}
    </p>
    <label
      >{$translate('team.networkPolicy')}<Select
        bind:value={draft.networkPolicy}
        label={$translate('team.networkPolicy')}
        options={networkPolicyOptions}
      /></label
    >
    <p class="path-hint">{$translate('team.networkPolicyHint')}</p>
    {#if networkPolicyIsStrict(draft.networkPolicy) && networkPolicyUnenforced}
      <p class="path-hint danger-hint">{$translate('team.networkPolicyStrictWarning')}</p>
    {/if}
    <label
      >{$translate('common.budget')}<input
        type="number"
        min="0"
        step="0.5"
        bind:value={draft.budget}
      /></label
    >
    {#if draft.provider === 'claude'}
      <label class="checkbox"
        ><input type="checkbox" bind:checked={draft.validateAfterRun} /><span
          >{$translate('team.validateAfterRun')}</span
        ></label
      >
      <p class="path-hint">{$translate('team.validateAfterRunHint')}</p>
      {#if draft.validateAfterRun}
        <label
          >{$translate('team.maxCorrectionRuns')}<input
            type="number"
            min="0"
            step="1"
            bind:value={draft.maxCorrectionRuns}
          /></label
        >
      {/if}
    {/if}
    <label class="checkbox"
      ><input type="checkbox" bind:checked={draft.reviewBeforeApply} /><span
        >{$translate('team.reviewBeforeApply')}</span
      ></label
    >
    <p class="path-hint">{$translate('team.reviewBeforeApplyHint')}</p>
    <button class="primary" type="submit" disabled={busy === 'agent-create'}
      ><Plus size={16} />{$translate('team.create')}</button
    >
  </form>
{/if}
<section class="team-grid">
  {#each agents.filter((agent) => !project || agent.projectId === project.id) as agent}
    <article class="agent-card">
      <div class="avatar"><Bot /></div>
      <div>
        <h2>{agent.name}</h2>
        <p>{agent.role}</p>
        {#if isLegacyProvider(agent.provider)}
          <span class="chip" title={$translate('run.legacyProviderHint')}
            >{$translate('run.legacyProvider')}</span
          >
        {/if}
      </div>
      <form
        class="agent-editor"
        onsubmit={(event) => {
          event.preventDefault();
          onUpdate(agent);
        }}
      >
        <label
          >{$translate('team.provider')}<Select
            bind:value={agent.provider}
            label={$translate('team.provider')}
            options={providerOptions}
          /></label
        >
        {#if agent.provider === 'openrouter'}
          <label class="field-span-2"
            >{$translate('common.model')}<OpenRouterModelPicker
              bind:value={agent.modelAlias}
              bind:allowUnverified={agent.allowUnverifiedModel}
              models={orModels?.models ?? []}
              curated={orModels?.curated ?? []}
              categories={orModels?.categories ?? []}
              loading={orLoading}
              error={orError}
              datalistId="models-openrouter"
            /></label
          >
        {:else}
          <label
            >{$translate('common.model')}<input
              bind:value={agent.modelAlias}
              list={`models-${agent.provider}`}
              placeholder={$translate('team.cliDefault')}
            /></label
          >
        {/if}
        <label
          >{$translate('team.effort')}<Select
            bind:value={agent.effort}
            label={$translate('team.effort')}
            options={effortOptions}
          /></label
        >
        <label
          >{$translate('team.permission')}<Select
            bind:value={agent.permission}
            label={$translate('team.permission')}
            options={permissionOptions}
          /></label
        >
        <p class="path-hint" class:danger-hint={agent.permission === 'danger-full-access'}>
          {$translate(permissionHint(agent.permission))}
        </p>
        <label
          >{$translate('team.networkPolicy')}<Select
            bind:value={agent.networkPolicy}
            label={$translate('team.networkPolicy')}
            options={networkPolicyOptions}
          /></label
        >
        <p class="path-hint">{$translate('team.networkPolicyHint')}</p>
        {#if networkPolicyIsStrict(agent.networkPolicy) && networkPolicyUnenforced}
          <p class="path-hint danger-hint">{$translate('team.networkPolicyStrictWarning')}</p>
        {/if}
        <label
          >{$translate('common.budget')}<input
            type="number"
            min="0"
            step="0.5"
            bind:value={agent.budget}
          /><small>{formatMoney(agent.budget, $locale)}</small></label
        >
        <label class="checkbox"
          ><input type="checkbox" bind:checked={agent.enabled} /><span
            >{$translate('team.enabled')}</span
          ></label
        >
        {#if agent.provider === 'claude'}
          <label class="checkbox"
            ><input type="checkbox" bind:checked={agent.validateAfterRun} /><span
              >{$translate('team.validateAfterRun')}</span
            ></label
          >
          <p class="path-hint">{$translate('team.validateAfterRunHint')}</p>
          {#if agent.validateAfterRun}
            <label
              >{$translate('team.maxCorrectionRuns')}<input
                type="number"
                min="0"
                step="1"
                bind:value={agent.maxCorrectionRuns}
              /></label
            >
          {/if}
        {/if}
        <label class="checkbox"
          ><input type="checkbox" bind:checked={agent.reviewBeforeApply} /><span
            >{$translate('team.reviewBeforeApply')}</span
          ></label
        >
        <p class="path-hint">{$translate('team.reviewBeforeApplyHint')}</p>
        <footer>
          <button type="submit" disabled={busy === `agent-${agent.id}`}
            ><Save size={15} />{$translate('common.save')}</button
          ><button
            class="primary"
            type="button"
            onclick={() => onRun(agent)}
            disabled={!agent.enabled || busy === `run-${agent.projectId}`}
            ><Play size={15} />{$translate('team.run')}</button
          >
        </footer>
      </form>
    </article>
  {:else}
    <div class="empty">
      <Bot size={32} />
      <p>{$translate('team.noAgents')}</p>
    </div>
  {/each}
</section>

<!-- One suggestion list per provider, shared by the create form and every agent
     row. They stay <datalist> rather than <select> because the value is passed
     to the CLI verbatim: a model released after this build must still be
     typeable without waiting for StudioForge to ship an updated list. -->
{#each ['claude', 'nvidia'] as provider (provider)}
  {#if modelsFor(provider).length > 0}
    <datalist id={`models-${provider}`}>
      {#each modelsFor(provider) as model (model.id)}
        <option value={model.id}>{model.label}</option>
      {/each}
    </datalist>
  {/if}
{/each}
<!-- Shared by every OpenRouterModelPicker instance above (see datalistId
     prop) so its free-text fallback can still reach any model id, not just
     the curated shortlist. -->
<datalist id="models-openrouter">
  {#each orModels?.models ?? [] as model (model.id)}
    <option value={model.id}>{model.name}</option>
  {/each}
</datalist>
