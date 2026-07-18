import type {
  ChatMessage,
  ChatThread,
  DetectedPaths,
  RunEvent,
  Snapshot,
  StudioStatus,
  SyncStatus,
  Task,
} from './types';

export class APIError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly requestId?: string,
  ) {
    super(message);
  }
}

async function parse<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = body.error ?? {};
    throw new APIError(
      error.message ?? `HTTP ${response.status}`,
      error.code ?? 'http_error',
      error.requestId,
    );
  }
  return body as T;
}

export async function bootstrapFromHash(): Promise<void> {
  const match = location.hash.match(/^#bootstrap=(.+)$/);
  if (!match) return;
  const token = decodeURIComponent(match[1]);
  const response = await fetch('/api/v1/session/bootstrap', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  });
  history.replaceState(null, '', `${location.pathname}${location.search}`);
  await parse(response);
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
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
  t: { title: string; description?: string; acceptanceCriteria?: string; priority?: number },
) => post<Task>(`/projects/${projectId}/tasks`, t);
export const updateTask = (taskId: string, patch: Partial<Task>) =>
  post<Task>(`/tasks/${taskId}`, patch);
export const deleteTask = (taskId: string): Promise<void> =>
  request(`/tasks/${taskId}`, { method: 'DELETE' }).then(() => undefined);

export function connectEvents(
  onEvent: (event: RunEvent) => void,
  onStatus: (online: boolean) => void,
): () => void {
  const stream = new EventSource('/api/v1/events', { withCredentials: true });
  const handler = (event: MessageEvent<string>) => {
    try {
      onEvent(JSON.parse(event.data) as RunEvent);
    } catch {
      /* malformed server data is ignored and replayable */
    }
  };
  for (const type of [
    'status',
    'message',
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
  stream.onopen = () => onStatus(true);
  stream.onerror = () => onStatus(false);
  return () => stream.close();
}
