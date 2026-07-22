import { describe, expect, it } from 'vitest';
import { aggregateOpenRouterMessages } from './openrouterStream';
import type { RunEvent } from './types';

function event(id: number, rawType: string, text: string, turn: number): RunEvent {
  return {
    id,
    projectId: 'p1',
    runId: 'r1',
    type: 'message',
    rawType,
    payload: { text, turn },
    createdAt: '2026-07-22T00:00:00Z',
  };
}

describe('aggregateOpenRouterMessages', () => {
  it('turns several deltas into one live bubble', () => {
    const result = aggregateOpenRouterMessages([
      event(0, 'openrouter.message.partial', 'Hello', 1),
      event(0, 'openrouter.message.partial', ' world', 1),
    ]);
    expect(result).toHaveLength(1);
    expect(result[0].payload).toEqual({ text: 'Hello world', turn: 1 });
  });

  it('replaces partial text with the final message', () => {
    const result = aggregateOpenRouterMessages([
      event(0, 'openrouter.message.partial', 'Hel', 1),
      event(42, 'openrouter.message', 'Hello', 1),
    ]);
    expect(result).toEqual([event(42, 'openrouter.message', 'Hello', 1)]);
  });

  it('keeps assistant turns around a tool call separate', () => {
    const result = aggregateOpenRouterMessages([
      event(0, 'openrouter.message.partial', 'Checking', 1),
      event(10, 'openrouter.message', 'Checking files', 1),
      event(0, 'openrouter.message.partial', 'Finished', 2),
      event(11, 'openrouter.message', 'Finished the change', 2),
    ]);
    expect(result.map((item) => (item.payload as { text: string }).text)).toEqual([
      'Checking files',
      'Finished the change',
    ]);
  });

  it('aggregates NVIDIA-compatible streaming events', () => {
    const result = aggregateOpenRouterMessages([
      event(0, 'nvidia.message.partial', 'Hello', 1),
      event(0, 'nvidia.message.partial', ' from NVIDIA', 1),
      event(12, 'nvidia.message', 'Hello from NVIDIA', 1),
    ]);
    expect(result).toEqual([event(12, 'nvidia.message', 'Hello from NVIDIA', 1)]);
  });
});
