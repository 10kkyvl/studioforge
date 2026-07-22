import { APIError, post, request } from './api';
import type { OpenRouterCapabilities, OpenRouterKeyTestResult, OpenRouterStatus } from './types';

export const getNVIDIAStatus = () => request<OpenRouterStatus>('/nvidia/status');

export const setNVIDIAKey = (key: string) => post<OpenRouterStatus>('/nvidia/key', { key });

export const removeNVIDIAKey = (): Promise<void> =>
  request('/nvidia/key', { method: 'DELETE' }).then(() => undefined);

export const testNVIDIAKey = () => post<OpenRouterKeyTestResult>('/nvidia/key/test', {});

export function isRetryableNVIDIATestError(error: unknown): boolean {
  return (
    error instanceof APIError &&
    ['nvidia_test_network', 'nvidia_test_timeout', 'nvidia_test_upstream'].includes(error.code)
  );
}

export const getNVIDIACapabilities = (model: string) =>
  request<OpenRouterCapabilities>(`/nvidia/capabilities?model=${encodeURIComponent(model)}`);
