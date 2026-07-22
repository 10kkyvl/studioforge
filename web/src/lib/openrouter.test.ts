import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  findModel,
  formatContextLength,
  getOpenRouterCapabilities,
  getOpenRouterModels,
  getOpenRouterStatus,
  groupCuratedByCategory,
  isFreeModel,
  removeOpenRouterKey,
  setOpenRouterKey,
  testOpenRouterKey,
} from './openrouter';
import type { OpenRouterCurated, OpenRouterModel } from './types';

afterEach(() => vi.unstubAllGlobals());

describe('isFreeModel', () => {
  it('is true when the model carries free:true', () => {
    expect(isFreeModel({ id: 'some/model', free: true })).toBe(true);
  });
  it('is true for the openrouter/free alias regardless of the free flag', () => {
    expect(isFreeModel({ id: 'openrouter/free' })).toBe(true);
  });
  it('is true for any id with a :free suffix', () => {
    expect(isFreeModel({ id: 'meta-llama/llama-3:free' })).toBe(true);
  });
  it('is false for a priced model', () => {
    expect(isFreeModel({ id: 'openai/gpt-4o', free: false })).toBe(false);
  });
  it('is false when there is no model to check', () => {
    expect(isFreeModel(undefined)).toBe(false);
    expect(isFreeModel(null)).toBe(false);
  });
});

describe('formatContextLength', () => {
  it('formats thousands with a K suffix', () => {
    expect(formatContextLength(128_000)).toBe('128K');
  });
  it('formats millions with an M suffix', () => {
    expect(formatContextLength(1_000_000)).toBe('1M');
  });
  it('trims a trailing .0 but keeps a real fraction', () => {
    expect(formatContextLength(1_500_000)).toBe('1.5M');
    expect(formatContextLength(2_000_000)).toBe('2M');
  });
  it('leaves small lengths as a plain number', () => {
    expect(formatContextLength(900)).toBe('900');
  });
  it('returns an empty string for zero or missing length', () => {
    expect(formatContextLength(0)).toBe('');
    expect(formatContextLength(-1)).toBe('');
  });
});

describe('groupCuratedByCategory', () => {
  const curated: OpenRouterCurated[] = [
    {
      id: 'a',
      category: 'Coding',
      recommendation: 'A',
      workload: 'w',
      free: false,
      vision: false,
      available: true,
    },
    {
      id: 'b',
      category: 'Vision',
      recommendation: 'B',
      workload: 'w',
      free: true,
      vision: true,
      available: true,
    },
    {
      id: 'c',
      category: 'Coding',
      recommendation: 'C',
      workload: 'w',
      free: false,
      vision: false,
      available: false,
    },
  ];

  it('orders groups per the categories list and keeps insertion order within a group', () => {
    expect(groupCuratedByCategory(curated, ['Vision', 'Coding'])).toEqual([
      { category: 'Vision', items: [curated[1]] },
      { category: 'Coding', items: [curated[0], curated[2]] },
    ]);
  });
  it('appends categories missing from the ordering list instead of dropping them', () => {
    expect(groupCuratedByCategory(curated, ['Coding'])).toEqual([
      { category: 'Coding', items: [curated[0], curated[2]] },
      { category: 'Vision', items: [curated[1]] },
    ]);
  });
  it('returns nothing for an empty curated list', () => {
    expect(groupCuratedByCategory([], ['Coding'])).toEqual([]);
  });
});

describe('findModel', () => {
  const models: OpenRouterModel[] = [
    {
      id: 'openai/gpt-4o',
      name: 'GPT-4o',
      contextLength: 128_000,
      vision: true,
      tools: true,
      structured: true,
      free: false,
      promptPrice: 2.5,
      completionPrice: 10,
    },
  ];
  it('finds a model by id', () => {
    expect(findModel(models, 'openai/gpt-4o')).toBe(models[0]);
  });
  it('returns undefined for an id not in the list', () => {
    expect(findModel(models, 'nope')).toBeUndefined();
  });
});

describe('OpenRouter API client', () => {
  it('reads key status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ state: 'configured', source: 'keychain', secure: true }), {
          status: 200,
        }),
      ),
    );
    await expect(getOpenRouterStatus()).resolves.toEqual({
      state: 'configured',
      source: 'keychain',
      secure: true,
    });
  });
  it('saves a key and returns status only, never the key itself', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ state: 'unverified', source: 'keychain', secure: true }), {
        status: 200,
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const result = await setOpenRouterKey('sk-or-secret');
    expect(result).toEqual({ state: 'unverified', source: 'keychain', secure: true });
    expect(JSON.stringify(result)).not.toContain('sk-or-secret');
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/openrouter/key',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ key: 'sk-or-secret' }) }),
    );
  });
  it('removes the stored key', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    await expect(removeOpenRouterKey()).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/openrouter/key',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });
  it('tests the connection and reports ok alongside the refreshed status', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ state: 'configured', source: 'keychain', secure: true, ok: true }),
          { status: 200 },
        ),
      );
    vi.stubGlobal('fetch', fetchMock);
    await expect(testOpenRouterKey()).resolves.toEqual({
      state: 'configured',
      source: 'keychain',
      secure: true,
      ok: true,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/openrouter/key/test',
      expect.objectContaining({ method: 'POST' }),
    );
  });
  it('fetches the model catalog without forcing a refresh by default', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ source: 'cache', models: [], curated: [], categories: [] }), {
        status: 200,
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    await getOpenRouterModels();
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/openrouter/models', expect.anything());
  });
  it('appends ?refresh=1 when a forced refresh is requested', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ source: 'live', models: [], curated: [], categories: [] }), {
        status: 200,
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    await getOpenRouterModels(true);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/openrouter/models?refresh=1',
      expect.anything(),
    );
  });
  it('looks up capabilities for a specific model id, URL-encoded', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          known: true,
          vision: false,
          tools: true,
          structured: true,
          contextLength: 32_000,
          free: false,
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    await getOpenRouterCapabilities('openai/gpt-4o mini');
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/openrouter/capabilities?model=openai%2Fgpt-4o%20mini',
      expect.anything(),
    );
  });
});
