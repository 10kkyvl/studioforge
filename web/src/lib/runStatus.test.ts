import { describe, expect, it } from 'vitest';
import type { RunEvent } from './types';
import { endsRun, isRunTerminal, mcpWithheldMessage } from './runStatus';

function event(patch: Partial<RunEvent>): RunEvent {
  return {
    id: 1,
    projectId: 'proj-1',
    runId: 'run-1',
    type: 'status',
    payload: {},
    createdAt: '2026-07-19T00:00:00Z',
    ...patch,
  };
}

describe('isRunTerminal', () => {
  it('treats interrupted as terminal, the same as completed/failed/cancelled/waiting_decision', () => {
    // A run left starting/running/cancelling when the daemon restarts is
    // recovered to 'interrupted' (RecoverInterrupted, internal/database/runs.go).
    // No process survived that restart, so a thread reopened afterward must
    // not treat this run as still active with a live elapsed timer.
    for (const status of [
      'completed',
      'failed',
      'cancelled',
      'waiting_decision',
      'interrupted',
    ]) {
      expect(isRunTerminal(status)).toBe(true);
    }
  });
  it('keeps the scheduler’s non-terminal states alive', () => {
    for (const status of ['queued', 'waiting_resources', 'starting', 'running', 'paused']) {
      expect(isRunTerminal(status)).toBe(false);
    }
  });
});

describe('endsRun', () => {
  it('ends the run on the scheduler’s own terminal states', () => {
    for (const status of ['completed', 'failed', 'cancelled', 'waiting_decision', 'interrupted']) {
      expect(endsRun(event({ rawType: 'scheduler.state', payload: { status } }), 'run-1')).toBe(
        true,
      );
    }
  });
  it('keeps the run alive on the scheduler’s non-terminal states', () => {
    for (const status of ['queued', 'waiting_resources', 'starting', 'running']) {
      expect(endsRun(event({ rawType: 'scheduler.state', payload: { status } }), 'run-1')).toBe(
        false,
      );
    }
  });
  // The provider streams its own JSON verbatim under type "status", and a
  // sub-agent finishing reports `status: "completed"` for the sub-agent, not
  // for the run that spawned it. Trusting it retired the progress strip while
  // the orchestrator still had ~1000 events of work left to stream.
  it('ignores a sub-agent task notification that completed', () => {
    const notification = event({
      rawType: 'system',
      payload: {
        type: 'system',
        subtype: 'task_notification',
        status: 'completed',
        task_id: 'a8544a03d5c50da21',
        summary: 'Read all 15 files.',
      },
    });
    expect(endsRun(notification, 'run-1')).toBe(false);
  });
  it('ignores every other provider status chatter', () => {
    const chatter = [
      { subtype: 'status', status: 'requesting' },
      { subtype: 'init', mcp_servers: [{ name: 'studio', status: 'connected' }] },
      { subtype: 'task_started' },
      { subtype: 'hook_response', status: 'completed' },
    ];
    for (const payload of chatter) {
      expect(endsRun(event({ rawType: 'system', payload }), 'run-1')).toBe(false);
    }
  });
  it('ignores a terminal state belonging to a different run', () => {
    const other = event({
      runId: 'run-2',
      rawType: 'scheduler.state',
      payload: { status: 'completed' },
    });
    expect(endsRun(other, 'run-1')).toBe(false);
  });
  it('ignores non-status events and malformed payloads', () => {
    expect(
      endsRun(
        event({ type: 'message', rawType: 'scheduler.state', payload: { status: 'completed' } }),
        'run-1',
      ),
    ).toBe(false);
    for (const payload of [null, undefined, 'completed', { status: 7 }, {}]) {
      expect(endsRun(event({ rawType: 'scheduler.state', payload }), 'run-1')).toBe(false);
    }
  });
});

describe('mcpWithheldMessage', () => {
  it('extracts the notice text from a scheduler.mcp status event', () => {
    const notice = event({
      rawType: 'scheduler.mcp',
      payload: { message: 'Studio access withheld: multiple ambiguous instances open.' },
    });
    expect(mcpWithheldMessage(notice)).toBe(
      'Studio access withheld: multiple ambiguous instances open.',
    );
  });
  it('ignores events of other raw types under type status', () => {
    expect(
      mcpWithheldMessage(event({ rawType: 'scheduler.state', payload: { message: 'hi' } })),
    ).toBeNull();
  });
  it('ignores events of other types even with a matching raw type', () => {
    expect(
      mcpWithheldMessage(
        event({ type: 'message', rawType: 'scheduler.mcp', payload: { message: 'hi' } }),
      ),
    ).toBeNull();
  });
  it('ignores malformed or empty payloads', () => {
    for (const payload of [null, undefined, 'hi', { message: 7 }, { message: '' }, {}]) {
      expect(mcpWithheldMessage(event({ rawType: 'scheduler.mcp', payload }))).toBeNull();
    }
  });
});
