import { describe, expect, it } from 'vitest';
import type { RunEvent } from './types';
import { LiveDiffDebounce, fileEditEventCount, isFileEditEvent } from './liveDiff';

function event(patch: Partial<RunEvent>): RunEvent {
  return {
    id: 1,
    projectId: 'proj-1',
    runId: 'run-1',
    type: 'message',
    payload: {},
    createdAt: '2026-07-19T00:00:00Z',
    ...patch,
  };
}

describe('isFileEditEvent', () => {
  it('recognizes the scheduler transient file_edit event', () => {
    expect(isFileEditEvent(event({ type: 'file_edit', rawType: 'scheduler.file_edit' }))).toBe(
      true,
    );
  });
  it('ignores events of other types even with a matching raw type', () => {
    expect(isFileEditEvent(event({ type: 'message', rawType: 'scheduler.file_edit' }))).toBe(false);
  });
  it('ignores file_edit-typed events with an unexpected raw type', () => {
    expect(isFileEditEvent(event({ type: 'file_edit', rawType: 'something.else' }))).toBe(false);
  });
});

describe('fileEditEventCount', () => {
  it('counts only file_edit events belonging to the given run', () => {
    const events = [
      event({ id: 1, runId: 'run-1', type: 'file_edit', rawType: 'scheduler.file_edit' }),
      event({ id: 2, runId: 'run-1', type: 'message' }),
      event({ id: 3, runId: 'run-2', type: 'file_edit', rawType: 'scheduler.file_edit' }),
      event({ id: 4, runId: 'run-1', type: 'file_edit', rawType: 'scheduler.file_edit' }),
    ];
    expect(fileEditEventCount(events, 'run-1')).toBe(2);
    expect(fileEditEventCount(events, 'run-2')).toBe(1);
  });
  it('returns 0 for an empty runId', () => {
    expect(
      fileEditEventCount([event({ type: 'file_edit', rawType: 'scheduler.file_edit' })], ''),
    ).toBe(0);
  });
});

describe('LiveDiffDebounce', () => {
  it('fires immediately on the first edit', () => {
    const debounce = new LiveDiffDebounce(1500);
    expect(debounce.onFileEdit(1000)).toEqual({ kind: 'fetch-now' });
  });
  it('debounces subsequent edits with a trailing fetch', () => {
    const debounce = new LiveDiffDebounce(1500);
    debounce.onFileEdit(1000);
    expect(debounce.onFileEdit(1100)).toEqual({ kind: 'schedule', fireAt: 2600 });
  });
  it('pushes the scheduled fetch out on each further edit', () => {
    const debounce = new LiveDiffDebounce(1500);
    debounce.onFileEdit(1000);
    debounce.onFileEdit(1100);
    expect(debounce.onFileEdit(1200)).toEqual({ kind: 'schedule', fireAt: 2700 });
  });
  it('consumes the current scheduled fetch exactly once', () => {
    const debounce = new LiveDiffDebounce(1500);
    debounce.onFileEdit(1000);
    const decision = debounce.onFileEdit(1100) as { kind: 'schedule'; fireAt: number };
    expect(debounce.consume(decision.fireAt)).toBe(true);
    expect(debounce.consume(decision.fireAt)).toBe(false);
  });
  it('rejects a stale timer superseded by a later edit', () => {
    const debounce = new LiveDiffDebounce(1500);
    debounce.onFileEdit(1000);
    const first = debounce.onFileEdit(1100) as { kind: 'schedule'; fireAt: number };
    const second = debounce.onFileEdit(1200) as { kind: 'schedule'; fireAt: number };
    expect(debounce.consume(first.fireAt)).toBe(false);
    expect(debounce.consume(second.fireAt)).toBe(true);
  });
  it('fires immediately again after reset', () => {
    const debounce = new LiveDiffDebounce(1500);
    debounce.onFileEdit(1000);
    debounce.reset();
    expect(debounce.onFileEdit(2000)).toEqual({ kind: 'fetch-now' });
  });
});
