import type { TranslationKey } from './i18n';
import type {
  ChatMessage,
  ChatThread,
  DetectedPaths,
  RunDiff,
  RunEvent,
  Snapshot,
  StudioStatus,
  SyncStatus,
  Task,
} from './types';

export class APIError extends Error {
  declare readonly status?: number;
  constructor(
    message: string,
    readonly code: string,
    readonly requestId?: string,
    status?: number,
  ) {
    super(message);
    Object.defineProperty(this, 'status', { value: status, enumerable: false });
  }
}

export function friendlyError(err: unknown, t: (key: TranslationKey) => string): string {
  if (err instanceof APIError) {
    if (err.code === 'timeout') return t('error.timeout');
    if (err.code === 'network') return t('error.network');
    if (err.status === 401 || err.status === 403) return t('error.session');
    if (err.status === 404) return t('error.notFound');
    if (err.status !== undefined && err.status >= 500) return t('error.server');
    return err.message;
  }
  return err instanceof Error ? err.message : String(err);
}

async function parse<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = body.error ?? {};
    throw new APIError(
      error.message ?? `HTTP ${response.status}`,
      error.code ?? 'http_error',
      error.requestId,
      response.status,
    );
  }
  return body as T;
}

// No request may hang forever: this app keeps one EventSource open per tab
// for the whole session (see connectEvents below), and Chrome caps a profile
// to 6 concurrent connections per origin on HTTP/1.1. Once that budget is
// exhausted a fetch can sit pending indefinitely rather than failing, which
// otherwise wedges any UI awaiting it (e.g. a spinner that only clears in a
// `finally`). Every request therefore gets its own deadline, combined with
// (not replacing) whatever signal the caller already passed in.
const REQUEST_TIMEOUT_MS = 15000;

function withTimeout(signal?: AbortSignal | null): AbortSignal {
  const timeout = AbortSignal.timeout(REQUEST_TIMEOUT_MS);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

async function fetchWithTimeout(input: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(input, { ...init, signal: withTimeout(init.signal) });
  } catch (cause) {
    // AbortSignal.timeout() aborts with a TimeoutError DOMException as its
    // reason, and fetch() rejects with that same reason — so this is exactly
    // our deadline firing, not a caller-initiated cancellation. Surface it as
    // a normal APIError instead of a bare AbortError so the UI can show a
    // real message rather than staying stuck (or throwing an unhandled type).
    if (cause instanceof DOMException && cause.name === 'TimeoutError') {
      throw new APIError('Request timed out. Check your connection and try again.', 'timeout');
    }
    throw new APIError(
      'Could not reach the server. Check your connection and try again.',
      'network',
    );
  }
}

export async function bootstrapFromHash(): Promise<void> {
  const hashMatch = location.hash.match(/^#bootstrap=(.+)$/);
  if (!hashMatch) return;
  const token = decodeURIComponent(hashMatch[1]);
  const response = await fetchWithTimeout('/api/v1/session/bootstrap', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  });
  history.replaceState(null, '', `${location.pathname}${location.search}`);
  await parse(response);
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetchWithTimeout(`/api/v1${path}`, {
    ...options,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(options.headers ?? {}) },
  });
  return parse<T>(response);
}

export const getSnapshot = () => request<Snapshot>('/snapshot');
export const detectPaths = () =>
  request<{ tools: DetectedPaths }>('/detect-paths').then((body) => body.tools);
export const post = <T>(path: string, body: unknown, headers?: HeadersInit) =>
  request<T>(path, { method: 'POST', headers, body: JSON.stringify(body) });
export const getThreads = (projectId: string) =>
  request<{ threads: ChatThread[] }>(`/projects/${projectId}/threads`).then((body) => body.threads);
export const createThread = (projectId: string, title: string) =>
  post<ChatThread>(`/projects/${projectId}/threads`, { title });
export const getThreadMessages = (threadId: string) =>
  request<{ messages: ChatMessage[] }>(`/threads/${threadId}/messages`).then(
    (body) => body.messages,
  );
export const getLead = (projectId: string) =>
  request<{ agentId: string }>(`/projects/${projectId}/lead`).then((body) => body.agentId);
export const setLead = (projectId: string, agentId: string): Promise<void> =>
  post<{ agentId: string }>(`/projects/${projectId}/lead`, { agentId }).then(() => undefined);
export const getPace = (projectId: string) =>
  request<{ typicalSeconds: number; samples: number }>(`/projects/${projectId}/pace`);
export const getRunDiff = (runId: string) => request<RunDiff>(`/runs/${runId}/diff`);
export const rollbackRun = (runId: string) =>
  post<{ branch: string; commitHash: string }>(`/runs/${runId}/rollback`, {});
export const getStudioStatus = (projectId?: string) =>
  request<StudioStatus>(
    projectId ? `/studio-status?project=${encodeURIComponent(projectId)}` : '/studio-status',
  );
export const startSync = (projectId: string) => post<SyncStatus>(`/projects/${projectId}/sync`, {});
export const stopSync = (projectId: string): Promise<void> =>
  request(`/projects/${projectId}/sync`, { method: 'DELETE' }).then(() => undefined);
// Bypasses request(): a multipart body must let the browser set its own
// Content-Type (with the boundary it generated), and request() always forces
// application/json.
export const uploadAttachment = (projectId: string, file: File | Blob) => {
  const form = new FormData();
  form.append('file', file);
  return fetch(`/api/v1/projects/${projectId}/attachments`, {
    method: 'POST',
    credentials: 'same-origin',
    body: form,
  }).then((response) => parse<{ path: string }>(response));
};
// The chip/thumbnail path is the composer's own reference
// (".studioforge/attachments/<name>"); the server only serves by basename,
// so this always strips down to that before building the URL.
export const attachmentUrl = (projectId: string, path: string) =>
  `/api/v1/projects/${projectId}/attachments/${encodeURIComponent(path.split('/').pop() ?? '')}`;
export const createTask = (
  projectId: string,
  t: {
    title: string;
    description?: string;
    acceptanceCriteria?: string;
    priority?: number;
    dependencies?: string[];
  },
) => post<Task>(`/projects/${projectId}/tasks`, t);
export const updateTask = (taskId: string, patch: Partial<Task>) =>
  post<Task>(`/tasks/${taskId}`, patch);
export const deleteTask = (taskId: string): Promise<void> =>
  request(`/tasks/${taskId}`, { method: 'DELETE' }).then(() => undefined);

// Shared, module-level SSE connection. The caller (web/src/routes/+page.svelte)
// invokes connectEvents unconditionally on every visibilitychange-to-visible
// event, not just on genuine connection loss — so this must be idempotent:
// reuse the live EventSource instead of tearing it down and reopening with a
// stale baked-in `after` cursor (which would replay already-seen events on
// the next real reconnect). Multiple call sites can therefore hold a
// "connection" concurrently; each gets its own unsubscribe, and the
// underlying EventSource is only closed once every subscriber has left.
let sharedStream: EventSource | null = null;
const eventSubscribers = new Set<(event: RunEvent) => void>();
const statusSubscribers = new Set<(online: boolean) => void>();

function openSharedStream(after?: number) {
  // `after` asks the server to replay everything since that event id (see
  // the `sse` handler's `after` query param in internal/api/api.go) before
  // switching to live delivery. This only matters when we're actually
  // opening a fresh connection (first call, or the previous one errored/
  // closed) — reused connections keep delivering live events to whichever
  // subscribers are currently registered, so no cursor is lost.
  const url = after ? `/api/v1/events?after=${after}` : '/api/v1/events';
  const stream = new EventSource(url, { withCredentials: true });
  const handler = (event: MessageEvent<string>) => {
    // Only JSON.parse is guarded: malformed server data is genuinely
    // recoverable (the next event replaces it, nothing was lost). A bug in a
    // subscriber's own onEvent callback is not the same kind of failure and
    // must not be swallowed here too — a prior version of this handler wrapped
    // both in one try/catch, so an exception thrown while processing a live
    // event (e.g. a future change to a subscriber) would silently drop that
    // event and every one after it for the rest of the connection's life, with
    // no console error and no visible symptom beyond the UI simply never
    // updating again.
    let parsed: RunEvent;
    try {
      parsed = JSON.parse(event.data) as RunEvent;
    } catch {
      /* malformed server data is ignored and replayable */
      return;
    }
    for (const onEvent of eventSubscribers) onEvent(parsed);
  };
  for (const type of [
    'status',
    'message',
    'question',
    'tool',
    'artifact',
    'usage',
    'result',
    'event',
    'error',
    'stderr',
  ]) {
    stream.addEventListener(type, handler as EventListener);
  }
  stream.onopen = () => {
    for (const onStatus of statusSubscribers) onStatus(true);
  };
  stream.onerror = (event: Event) => {
    if (event instanceof MessageEvent) return;
    for (const onStatus of statusSubscribers) onStatus(false);
  };
  sharedStream = stream;
}

export function connectEvents(
  onEvent: (event: RunEvent) => void,
  onStatus: (online: boolean) => void,
  after?: number,
): () => void {
  eventSubscribers.add(onEvent);
  statusSubscribers.add(onStatus);
  const live =
    sharedStream !== null &&
    (sharedStream.readyState === EventSource.OPEN ||
      sharedStream.readyState === EventSource.CONNECTING);
  if (!live) {
    openSharedStream(after);
  } else if (sharedStream!.readyState === EventSource.OPEN) {
    onStatus(true);
  }
  return () => {
    eventSubscribers.delete(onEvent);
    statusSubscribers.delete(onStatus);
    if (eventSubscribers.size === 0 && sharedStream) {
      sharedStream.close();
      sharedStream = null;
    }
  };
}
