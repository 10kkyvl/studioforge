<script lang="ts">
  import { Bot, Play, Plus, Save } from '@lucide/svelte';
  import { formatMoney, locale, translate } from '$lib/i18n';
  import { modelsFor } from '$lib/models';
  import type { Agent, Project } from '$lib/types';

  export let agents: Agent[];
  export let project: Project | undefined;
  export let busy: string;
  export let onCreate: (agent: Partial<Agent>) => void;
  export let onUpdate: (agent: Agent) => void;
  export let onRun: (agent: Agent) => void;

  let showCreate = false;
  const blankDraft: Partial<Agent> = {
    name: '',
    role: 'Roblox Engineer',
    provider: 'codex',
    modelAlias: 'default',
    effort: 'medium',
    permission: 'workspace-write',
    concurrency: 1,
    budget: 10,
    validateAfterRun: false,
    maxCorrectionRuns: 1,
  };
  let draft: Partial<Agent> = { ...blankDraft };

  function create() {
    onCreate(draft);
    draft = { ...blankDraft };
    showCreate = false;
  }
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.team')}</p>
    <h1>{$translate('team.title')}</h1>
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
      >{$translate('team.provider')}<select bind:value={draft.provider}
        ><option value="codex">Codex</option><option value="claude">Claude Code</option><option
          value="mock">Mock</option
        ></select
      ></label
    >
    <label
      >{$translate('common.model')}<input
        bind:value={draft.modelAlias}
        list={`models-${draft.provider}`}
        placeholder={$translate('team.cliDefault')}
      /></label
    >
    <label
      >{$translate('team.effort')}<select bind:value={draft.effort}
        ><option value="low">low</option><option value="medium">medium</option><option value="high"
          >high</option
        ><option value="xhigh">xhigh</option></select
      ></label
    >
    <label
      >{$translate('team.permission')}<select bind:value={draft.permission}
        ><option value="read-only">read-only</option><option value="workspace-write"
          >workspace-write</option
        ><option value="danger-full-access">danger-full-access</option></select
      ></label
    >
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
      </div>
      <form
        class="agent-editor"
        onsubmit={(event) => {
          event.preventDefault();
          onUpdate(agent);
        }}
      >
        <label
          >{$translate('team.provider')}<select bind:value={agent.provider}
            ><option value="codex">Codex</option><option value="claude">Claude Code</option><option
              value="mock">Mock</option
            ></select
          ></label
        >
        <label
          >{$translate('common.model')}<input
            bind:value={agent.modelAlias}
            list={`models-${agent.provider}`}
            placeholder={$translate('team.cliDefault')}
          /></label
        >
        <label
          >{$translate('team.effort')}<select bind:value={agent.effort}
            ><option value="low">low</option><option value="medium">medium</option><option
              value="high">high</option
            ><option value="xhigh">xhigh</option></select
          ></label
        >
        <label
          >{$translate('team.permission')}<select bind:value={agent.permission}
            ><option value="read-only">read-only</option><option value="workspace-write"
              >workspace-write</option
            ><option value="danger-full-access">danger-full-access</option></select
          ></label
        >
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
{#each ['claude', 'codex'] as provider (provider)}
  {#if modelsFor(provider).length > 0}
    <datalist id={`models-${provider}`}>
      {#each modelsFor(provider) as model (model.id)}
        <option value={model.id}>{model.label}</option>
      {/each}
    </datalist>
  {/if}
{/each}
