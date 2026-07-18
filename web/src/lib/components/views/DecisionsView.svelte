<script lang="ts">
  import { CheckCircle2, ShieldAlert } from '@lucide/svelte';
  import { translate } from '$lib/i18n';
  import type { Decision } from '$lib/types';

  export let decisions: Decision[];
  export let projectName: (id: string) => string;
  export let onDecide: (id: string, status: string) => void;
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.decisions')}</p>
    <h1>{$translate('decisions.title')}</h1>
    <p>{$translate('decisions.subtitle')}</p>
  </div>
</section>
<section class="decision-list">
  {#each decisions.filter((decision) => decision.status === 'pending') as decision}
    <article class="decision-card">
      <header>
        <div>
          <span class={`risk risk-${decision.risk}`}>{decision.risk}</span>
          <h2>{decision.title}</h2>
          <small>{projectName(decision.projectId)}</small>
        </div>
        <ShieldAlert />
      </header>
      <dl>
        <div>
          <dt>{$translate('decisions.reason')}</dt>
          <dd>{decision.reason}</dd>
        </div>
        <div>
          <dt>{$translate('decisions.action')}</dt>
          <dd>{decision.proposedAction}</dd>
        </div>
        <div>
          <dt>{$translate('decisions.preview')}</dt>
          <dd><code>{decision.preview}</code></dd>
        </div>
      </dl>
      <footer>
        <button class="danger" onclick={() => onDecide(decision.id, 'rejected')}
          >{$translate('decisions.reject')}</button
        ><button class="primary" onclick={() => onDecide(decision.id, 'approved_once')}
          >{$translate('decisions.approve')}</button
        >
      </footer>
    </article>
  {:else}<div class="empty">
      <CheckCircle2 size={32} />
      <p>{$translate('decisions.empty')}</p>
    </div>{/each}
</section>
