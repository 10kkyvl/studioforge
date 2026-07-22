import type { Run } from './types';
import { isRunTerminal } from './runStatus';

const EXECUTING = new Set(['starting', 'running', 'cancelling']);

function createdAt(run: Run): number {
  const value = Date.parse(run.createdAt);
  return Number.isFinite(value) ? value : 0;
}

/**
 * Combines immediately returned runs with the latest snapshot. Snapshot rows
 * win because they carry scheduler status changes that happened after POST.
 */
export function liveThreadRuns(
  snapshotRuns: Run[],
  submittedRuns: Run[],
  threadId: string,
  endedRunIds: ReadonlySet<string> = new Set(),
): Run[] {
  if (!threadId) return [];
  const byId = new Map<string, Run>();
  for (const run of submittedRuns) byId.set(run.id, run);
  for (const run of snapshotRuns) byId.set(run.id, run);
  return [...byId.values()]
    .filter(
      (run) => run.threadId === threadId && !isRunTerminal(run.status) && !endedRunIds.has(run.id),
    )
    .sort((a, b) => createdAt(a) - createdAt(b) || a.id.localeCompare(b.id));
}

/** Keeps the progress strip on the run that is really executing. */
export function foregroundRun(runs: Run[]): Run | undefined {
  return runs.find((run) => EXECUTING.has(run.status)) ?? runs[0];
}

export function queuedBehindForeground(runs: Run[], foreground: Run | undefined): Run[] {
  return foreground ? runs.filter((run) => run.id !== foreground.id) : [];
}
