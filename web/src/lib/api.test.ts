import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  APIError,
  attachmentUrl,
  connectEvents,
  getLead,
  getPace,
  getStudioStatus,
  request,
  setLead,
  startSync,
  stopSync,
  uploadAttachment,
} from './api';

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
  it('accepts a 2xx response other than 200, e.g. 202 Accepted', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 202 })),
    );
    await expect(request<{ ok: boolean }>('/test')).resolves.toEqual({ ok: true });
  });
  it('maps a raw network failure to a retryable APIError instead of the bare browser error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    await expect(request('/test')).rejects.toBeInstanceOf(APIError);
    await expect(request('/test')).rejects.toMatchObject({ code: 'network' });
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

describe('sync endpoints', () => {
  it('starts a live-sync session and returns its status', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ active: true, port: 34872, startedAt: '2026-07-19T00:00:00Z' }),
        {
          status: 200,
        },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    await expect(startSync('proj-1')).resolves.toEqual({
      active: true,
      port: 34872,
      startedAt: '2026-07-19T00:00:00Z',
    });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/projects/proj-1/sync',
      expect.objectContaining({ method: 'POST' }),
    );
  });
  it('stops a live-sync session', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    await expect(stopSync('proj-1')).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/projects/proj-1/sync',
      expect.objectContaining({ method: 'DELETE' }),
    );
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

describe('attachment endpoints', () => {
  it('uploads a pasted image as multipart, not JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ path: '.studioforge/attachments/2026-07-19-abc123.png' }), {
        status: 201,
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const file = new File(['fake-bytes'], 'clip.png', { type: 'image/png' });
    await expect(uploadAttachment('proj-1', file)).resolves.toEqual({
      path: '.studioforge/attachments/2026-07-19-abc123.png',
    });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/projects/proj-1/attachments');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    // No Content-Type header of our own: the browser must set the multipart
    // boundary itself, which it can only do if this stays unset.
    expect(init.headers).toBeUndefined();
    expect(init.body).toBeInstanceOf(FormData);
    expect((init.body as FormData).get('file')).toBe(file);
  });
  it('surfaces the server error envelope on a rejected upload', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: {
              code: 'unsupported_type',
              message: 'Only PNG, JPEG, GIF, or WebP images are accepted',
            },
          }),
          { status: 400 },
        ),
      ),
    );
    const file = new File(['not-an-image'], 'notes.txt', { type: 'text/plain' });
    await expect(uploadAttachment('proj-1', file)).rejects.toBeInstanceOf(APIError);
  });
  it("builds the download URL from the path's basename", () => {
    expect(attachmentUrl('proj-1', '.studioforge/attachments/2026-07-19-abc123.png')).toBe(
      '/api/v1/projects/proj-1/attachments/2026-07-19-abc123.png',
    );
  });
});

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  readyState = FakeEventSource.CONNECTING;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closeCalled = false;
  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }
  addEventListener() {}
  close() {
    this.closeCalled = true;
    this.readyState = FakeEventSource.CLOSED;
  }
}

describe('connectEvents', () => {
  it('still closes the connection on disconnect after a transient error the browser auto-recovers from', () => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
    const disconnect = connectEvents(
      () => {},
      () => {},
    );
    const stream = FakeEventSource.instances[0];
    stream.onerror?.();
    stream.readyState = FakeEventSource.OPEN;
    stream.onopen?.();
    disconnect();
    expect(stream.closeCalled).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(1);
  });
});
