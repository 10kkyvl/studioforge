import type { RunEvent } from './types';

const FILE_EDIT_TYPE = 'file_edit';
const FILE_EDIT_RAW_TYPE = 'scheduler.file_edit';

export function isFileEditEvent(event: RunEvent): boolean {
  return event.type === FILE_EDIT_TYPE && event.rawType === FILE_EDIT_RAW_TYPE;
}

export function fileEditEventCount(events: RunEvent[], runId: string): number {
  if (!runId) return 0;
  return events.filter((event) => event.runId === runId && isFileEditEvent(event)).length;
}

export type LiveDiffDecision = { kind: 'fetch-now' } | { kind: 'schedule'; fireAt: number };

export class LiveDiffDebounce {
  private fetchedOnce = false;
  private pendingFireAt: number | null = null;

  constructor(private readonly delayMs: number = 1500) {}

  onFileEdit(now: number): LiveDiffDecision {
    if (!this.fetchedOnce) {
      this.fetchedOnce = true;
      this.pendingFireAt = null;
      return { kind: 'fetch-now' };
    }
    const fireAt = now + this.delayMs;
    this.pendingFireAt = fireAt;
    return { kind: 'schedule', fireAt };
  }

  consume(fireAt: number): boolean {
    if (this.pendingFireAt !== fireAt) return false;
    this.pendingFireAt = null;
    return true;
  }

  reset(): void {
    this.fetchedOnce = false;
    this.pendingFireAt = null;
  }
}
