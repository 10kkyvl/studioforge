import { describe, expect, it } from 'vitest';
import type { Run, RunEvent } from './types';
import {
  formatReviewCountdown,
  isReviewExpired,
  isReviewGated,
  reviewExpiresAtMs,
  reviewOpenPayload,
  reviewRemainingMs,
} from './review';

function run(overrides: Partial<Run> = {}): Run {
  return {
    id: 'run-1',
    projectId: 'proj-1',
    agentId: 'agent-1',
    provider: 'claude',
    modelAlias: 'default',
    status: 'waiting_decision',
    phase: 'review',
    cost: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    createdAt: '2026-08-15T10:00:00Z',
    updatedAt: '2026-08-15T10:00:00Z',
    validation: 'none',
    correctionDepth: 0,
    ...overrides,
  };
}

function reviewEvent(overrides: Partial<RunEvent> = {}): RunEvent {
  return {
    id: 1,
    projectId: 'proj-1',
    runId: 'run-1',
    type: 'review',
    rawType: 'scheduler.review',
    payload: { checkpoint: 'abc123', expiresAt: '2026-08-15T11:00:00Z' },
    createdAt: '2026-08-15T10:00:00Z',
    ...overrides,
  };
}

describe('isReviewGated', () => {
  it('is true for a run parked in waiting_decision/review', () => {
    expect(isReviewGated(run())).toBe(true);
  });
  it('is false for any other status', () => {
    expect(isReviewGated(run({ status: 'running' }))).toBe(false);
  });
  it('is false for waiting_decision in a different phase', () => {
    expect(isReviewGated(run({ phase: 'stuck' }))).toBe(false);
  });
  it('is false for a missing run', () => {
    expect(isReviewGated(undefined)).toBe(false);
    expect(isReviewGated(null)).toBe(false);
  });
});

describe('reviewOpenPayload', () => {
  it('reads checkpoint and expiresAt off the scheduler.review event', () => {
    expect(reviewOpenPayload([reviewEvent()], 'run-1')).toEqual({
      checkpoint: 'abc123',
      expiresAt: '2026-08-15T11:00:00Z',
    });
  });
  it('ignores events for other runs', () => {
    expect(reviewOpenPayload([reviewEvent({ runId: 'run-2' })], 'run-1')).toBeNull();
  });
  it('ignores review events that are not the opening one', () => {
    expect(
      reviewOpenPayload(
        [reviewEvent({ rawType: 'api.review', payload: { status: 'applied' } })],
        'run-1',
      ),
    ).toBeNull();
  });
  it('takes the most recent opening event when several exist', () => {
    const events = [
      reviewEvent({ id: 1, payload: { checkpoint: 'first', expiresAt: '2026-08-15T11:00:00Z' } }),
      reviewEvent({ id: 2, payload: { checkpoint: 'second', expiresAt: '2026-08-15T12:00:00Z' } }),
    ];
    expect(reviewOpenPayload(events, 'run-1')).toEqual({
      checkpoint: 'second',
      expiresAt: '2026-08-15T12:00:00Z',
    });
  });
});

describe('reviewExpiresAtMs', () => {
  it('prefers the expiresAt carried by the live event', () => {
    const ms = reviewExpiresAtMs(run(), [reviewEvent()], 24);
    expect(ms).toBe(Date.parse('2026-08-15T11:00:00Z'));
  });
  it('falls back to updatedAt plus the configured expiry when no event is known', () => {
    const ms = reviewExpiresAtMs(run({ updatedAt: '2026-08-15T10:00:00Z' }), [], 24);
    expect(ms).toBe(Date.parse('2026-08-15T10:00:00Z') + 24 * 60 * 60 * 1000);
  });
  it('treats a non-positive fallback as the 24h server default', () => {
    const ms = reviewExpiresAtMs(run({ updatedAt: '2026-08-15T10:00:00Z' }), [], 0);
    expect(ms).toBe(Date.parse('2026-08-15T10:00:00Z') + 24 * 60 * 60 * 1000);
  });
});

describe('isReviewExpired / reviewRemainingMs', () => {
  it('is not expired before the deadline', () => {
    expect(isReviewExpired(1000, 999)).toBe(false);
    expect(reviewRemainingMs(1000, 999)).toBe(1);
  });
  it('is expired exactly at and after the deadline', () => {
    expect(isReviewExpired(1000, 1000)).toBe(true);
    expect(isReviewExpired(1000, 1001)).toBe(true);
    expect(reviewRemainingMs(1000, 1500)).toBe(0);
  });
});

describe('formatReviewCountdown', () => {
  it('formats under an hour as mm:ss', () => {
    expect(formatReviewCountdown(65_000)).toBe('01:05');
  });
  it('formats an hour or more as h:mm:ss', () => {
    expect(formatReviewCountdown(3 * 60 * 60 * 1000 + 5 * 60 * 1000 + 9 * 1000)).toBe('3:05:09');
  });
  it('floors negative or zero remaining time to 00:00', () => {
    expect(formatReviewCountdown(0)).toBe('00:00');
  });
});
