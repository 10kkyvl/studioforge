import type { Run, RunEvent } from './types';

const REVIEW_OPEN_RAW_TYPE = 'scheduler.review';

export function isReviewGated(run: Pick<Run, 'status' | 'phase'> | undefined | null): boolean {
  return !!run && run.status === 'waiting_decision' && run.phase === 'review';
}

export function reviewOpenPayload(
  events: RunEvent[],
  runId: string,
): { checkpoint?: string; expiresAt?: string } | null {
  for (let i = events.length - 1; i >= 0; i -= 1) {
    const event = events[i];
    if (
      event.runId !== runId ||
      event.type !== 'review' ||
      event.rawType !== REVIEW_OPEN_RAW_TYPE
    ) {
      continue;
    }
    const payload = event.payload;
    if (!payload || typeof payload !== 'object') continue;
    const value = payload as Record<string, unknown>;
    return {
      checkpoint: typeof value.checkpoint === 'string' ? value.checkpoint : undefined,
      expiresAt: typeof value.expiresAt === 'string' ? value.expiresAt : undefined,
    };
  }
  return null;
}

export function reviewExpiresAtMs(
  run: Pick<Run, 'id' | 'updatedAt'>,
  events: RunEvent[],
  fallbackHours: number,
): number {
  const fromEvent = reviewOpenPayload(events, run.id)?.expiresAt;
  if (fromEvent) {
    const parsed = Date.parse(fromEvent);
    if (!Number.isNaN(parsed)) return parsed;
  }
  const openedAt = Date.parse(run.updatedAt);
  const base = Number.isNaN(openedAt) ? Date.now() : openedAt;
  const hours = fallbackHours > 0 ? fallbackHours : 24;
  return base + hours * 60 * 60 * 1000;
}

export function isReviewExpired(expiresAtMs: number, nowMs: number): boolean {
  return nowMs >= expiresAtMs;
}

export function reviewRemainingMs(expiresAtMs: number, nowMs: number): number {
  return Math.max(0, expiresAtMs - nowMs);
}

export function formatReviewCountdown(remainingMs: number): string {
  const totalSeconds = Math.floor(remainingMs / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const mm = minutes.toString().padStart(2, '0');
  const ss = seconds.toString().padStart(2, '0');
  return hours > 0 ? `${hours}:${mm}:${ss}` : `${mm}:${ss}`;
}
