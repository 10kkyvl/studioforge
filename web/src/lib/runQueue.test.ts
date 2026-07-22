import { describe, expect, it } from 'vitest';
import { foregroundRun, liveThreadRuns, queuedBehindForeground } from './runQueue';
import type { Run } from './types';

function run(id: string, status: string, createdAt: string): Run {
  return {
    id,
    projectId: 'p',
    agentId: 'a',
    threadId: 't',
    provider: 'mock',
    modelAlias: 'balanced',
    status,
    phase: status,
    cost: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    createdAt,
    updatedAt: createdAt,
    validation: 'none',
    correctionDepth: 0,
  };
}

describe('chat run queue', () => {
  it('keeps progress on the executing run when a newer follow-up is queued', () => {
    const first = run('first', 'running', '2026-07-22T10:00:00Z');
    const followUp = run('follow-up', 'waiting_resources', '2026-07-22T10:00:01Z');
    const live = liveThreadRuns([first], [followUp], 't');
    const foreground = foregroundRun(live);
    expect(foreground?.id).toBe('first');
    expect(queuedBehindForeground(live, foreground).map((item) => item.id)).toEqual(['follow-up']);
  });

  it('promotes the queued follow-up as soon as the first run ends over SSE', () => {
    const first = run('first', 'running', '2026-07-22T10:00:00Z');
    const followUp = run('follow-up', 'waiting_resources', '2026-07-22T10:00:01Z');
    const live = liveThreadRuns([first, followUp], [], 't', new Set(['first']));
    expect(foregroundRun(live)?.id).toBe('follow-up');
  });

  it('lets refreshed snapshot status override the optimistic submitted row', () => {
    const optimistic = run('follow-up', 'queued', '2026-07-22T10:00:01Z');
    const refreshed = { ...optimistic, status: 'running', phase: 'agent' };
    const live = liveThreadRuns([refreshed], [optimistic], 't');
    expect(live[0].status).toBe('running');
  });
});
