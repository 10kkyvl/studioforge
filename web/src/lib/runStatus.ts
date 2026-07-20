import type { RunEvent } from './types';

// Statuses the scheduler uses to declare a run's live process has ended.
// waiting_decision belongs here too: the run stops executing as soon as an
// agent asks a closed question, even though the thread stays resumable and
// will continue once the question is answered. interrupted belongs here too:
// RecoverInterrupted (internal/database/runs.go) stamps it on every run still
// starting/running/cancelling when the daemon restarts, precisely because no
// process survived to keep running it — without it here, a thread reopened
// after a restart finds that dead run "active" again and shows a live
// elapsed timer ticking up from its createdAt. This function only gates "has
// this run's process finished (for now)", not "is the thread done".
const TERMINAL_STATUSES = new Set([
  'completed',
  'failed',
  'cancelled',
  'waiting_decision',
  'interrupted',
]);
// The scheduler is the only authority on a run's lifecycle: it stamps every
// state change with this raw type (see transition()/fail() in
// internal/scheduler/scheduler.go). Providers stream their own JSON verbatim
// under the same event type, so without this gate a sub-agent's
// `{"subtype":"task_notification","status":"completed"}` reads as the whole
// run finishing and the progress strip vanishes mid-run.
const SCHEDULER_STATE = 'scheduler.state';

export function isRunTerminal(status: string): boolean {
  return TERMINAL_STATUSES.has(status);
}

const MCP_WITHHELD = 'scheduler.mcp';

export function mcpWithheldMessage(event: RunEvent): string | null {
  if (event.type !== 'status' || event.rawType !== MCP_WITHHELD) return null;
  const payload = event.payload;
  if (!payload || typeof payload !== 'object') return null;
  const message = (payload as Record<string, unknown>).message;
  return typeof message === 'string' && message !== '' ? message : null;
}

/** Reports whether this event is the scheduler declaring `runId` finished. */
export function endsRun(event: RunEvent, runId: string): boolean {
  if (!runId || event.runId !== runId) return false;
  if (event.type !== 'status' || event.rawType !== SCHEDULER_STATE) return false;
  const payload = event.payload;
  if (!payload || typeof payload !== 'object') return false;
  const status = (payload as Record<string, unknown>).status;
  return typeof status === 'string' && TERMINAL_STATUSES.has(status);
}
