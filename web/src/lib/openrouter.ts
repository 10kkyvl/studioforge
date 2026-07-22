import { post, request } from './api';
import type {
  OpenRouterCapabilities,
  OpenRouterCurated,
  OpenRouterKeyTestResult,
  OpenRouterModel,
  OpenRouterModelsResponse,
  OpenRouterStatus,
} from './types';

export const getOpenRouterStatus = () => request<OpenRouterStatus>('/openrouter/status');

// The key is send-only: this resolves to the same shape /status returns
// (state/source/secure), never the key itself, so nothing in the UI can hold
// onto or display key material even by accident.
export const setOpenRouterKey = (key: string) => post<OpenRouterStatus>('/openrouter/key', { key });

export const removeOpenRouterKey = (): Promise<void> =>
  request('/openrouter/key', { method: 'DELETE' }).then(() => undefined);

export const testOpenRouterKey = () => post<OpenRouterKeyTestResult>('/openrouter/key/test', {});

export const getOpenRouterModels = (refresh = false) =>
  request<OpenRouterModelsResponse>(
    refresh ? '/openrouter/models?refresh=1' : '/openrouter/models',
  );

export const getOpenRouterCapabilities = (model: string) =>
  request<OpenRouterCapabilities>(`/openrouter/capabilities?model=${encodeURIComponent(model)}`);

// OpenRouter has no single canonical "is this free" flag on every payload
// shape it hands back — the `models` list already carries one, but curated
// entries and a bare model id typed into the free-text field do not. This is
// the one place that gap is closed, so the picker, the free-model warning,
// and the tests all agree on what counts as free.
export function isFreeModel(model: { id: string; free?: boolean } | undefined | null): boolean {
  if (!model) return false;
  if (model.free) return true;
  return model.id === 'openrouter/free' || model.id.endsWith(':free');
}

// Compact context-length label for the capability hint, matching the
// K/M shorthand used elsewhere (see i18n.ts's formatTokens) but kept
// dependency-free and synchronous so it stays a trivial pure function.
export function formatContextLength(length: number): string {
  if (!length || length <= 0) return '';
  if (length >= 1_000_000) return `${trimZero(length / 1_000_000)}M`;
  if (length >= 1_000) return `${trimZero(length / 1_000)}K`;
  return `${length}`;
}

function trimZero(value: number): string {
  return value.toFixed(1).replace(/\.0$/, '');
}

export type CuratedGroup = { category: string; items: OpenRouterCurated[] };

// Groups curated picks by category in the server-provided `categories`
// order (the order it recommends showing them in), then appends any
// category that shows up in `curated` but was left out of that list rather
// than silently dropping its entries.
export function groupCuratedByCategory(
  curated: OpenRouterCurated[],
  categories: string[],
): CuratedGroup[] {
  const byCategory = new Map<string, OpenRouterCurated[]>();
  for (const item of curated) {
    const list = byCategory.get(item.category);
    if (list) list.push(item);
    else byCategory.set(item.category, [item]);
  }
  const known = categories.filter((category) => byCategory.has(category));
  const rest = [...byCategory.keys()].filter((category) => !categories.includes(category));
  return [...known, ...rest].map((category) => ({ category, items: byCategory.get(category)! }));
}

export function findModel(models: OpenRouterModel[], id: string): OpenRouterModel | undefined {
  return models.find((model) => model.id === id);
}
