import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIError, getLead, getPace, getStudioStatus, request, setLead } from './api';

afterEach(() => vi.unstubAllGlobals());
describe('API client', () => {
  it('normalizes the JSON error envelope', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ error: { code: 'bad', message: 'Nope', requestId: 'r1' } }),
            { status: 400 },
          ),
        ),
    );
    await expect(request('/test')).rejects.toEqual(new APIError('Nope', 'bad', 'r1'));
  });
  it('returns typed JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 })),
    );
    await expect(request<{ ok: boolean }>('/test')).resolves.toEqual({ ok: true });
  });
});

describe('lead agent endpoints', () => {
  it('reads the lead agent id', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(new Response(JSON.stringify({ agentId: 'agent-1' }), { status: 200 })),
    );
    await expect(getLead('proj-1')).resolves.toBe('agent-1');
  });
  it('returns an empty string when no lead is set', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ agentId: '' }), { status: 200 })),
    );
    await expect(getLead('proj-1')).resolves.toBe('');
  });
  it('sets the lead agent id', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ agentId: 'agent-2' }), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    await expect(setLead('proj-1', 'agent-2')).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/projects/proj-1/lead',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ agentId: 'agent-2' }) }),
    );
  });
});

describe('studio status endpoint', () => {
  it('scopes the query to a project and returns the full status', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ open: 2, matched: 1, state: 'matched' }), { status: 200 }),
      );
    vi.stubGlobal('fetch', fetchMock);
    await expect(getStudioStatus('proj-1')).resolves.toEqual({
      open: 2,
      matched: 1,
      state: 'matched',
    });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/studio-status?project=proj-1',
      expect.objectContaining({ credentials: 'same-origin' }),
    );
  });
  it('omits the project parameter when no project is selected', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ open: 0, matched: 0, state: 'none' }), { status: 200 }),
      );
    vi.stubGlobal('fetch', fetchMock);
    await expect(getStudioStatus()).resolves.toEqual({ open: 0, matched: 0, state: 'none' });
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/studio-status', expect.anything());
  });
});

describe('pace endpoint', () => {
  it('reads the typical run duration and sample count', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ typicalSeconds: 42.5, samples: 3 }), { status: 200 }),
      );
    vi.stubGlobal('fetch', fetchMock);
    await expect(getPace('proj-1')).resolves.toEqual({ typicalSeconds: 42.5, samples: 3 });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/projects/proj-1/pace',
      expect.objectContaining({ credentials: 'same-origin' }),
    );
  });
  it('reports zero samples when there is no history', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ typicalSeconds: 0, samples: 0 }), { status: 200 }),
        ),
    );
    await expect(getPace('proj-1')).resolves.toEqual({ typicalSeconds: 0, samples: 0 });
  });
});
