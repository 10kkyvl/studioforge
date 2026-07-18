<script lang="ts">
  import { Library } from '@lucide/svelte';
  import { translate } from '$lib/i18n';
</script>

<section class="page-heading">
  <div>
    <p class="eyebrow">{$translate('nav.assets')}</p>
    <h1>{$translate('assets.title')}</h1>
    <p>{$translate('assets.subtitle')}</p>
  </div>
</section>
<div class="empty asset-empty">
  <Library size={36} />
  <h2>{$translate('assets.empty')}</h2>
  <code>{$translate('assets.workflow')}</code>
</div>
