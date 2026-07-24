import type { RunEvent } from './types';

function turnOf(event: RunEvent): number | null {
  if (!event.payload || typeof event.payload !== 'object') return null;
  const turn = (event.payload as Record<string, unknown>).turn;
  return typeof turn === 'number' && Number.isInteger(turn) && turn > 0 ? turn : null;
}

function textOf(event: RunEvent): string {
  if (!event.payload || typeof event.payload !== 'object') return '';
  const text = (event.payload as Record<string, unknown>).text;
  return typeof text === 'string' ? text : '';
}

// A retried request re-streams the whole answer from the beginning. The
// provider marks that restart so the text accumulated from the failed attempt
// is dropped instead of being prefixed onto the new one.
function isReset(event: RunEvent): boolean {
  if (!event.payload || typeof event.payload !== 'object') return false;
  return (event.payload as Record<string, unknown>).reset === true;
}

function withText(event: RunEvent, text: string): RunEvent {
  return { ...event, payload: { ...(event.payload as Record<string, unknown>), text } };
}

export function aggregateOpenRouterMessages(events: RunEvent[]): RunEvent[] {
  const result: RunEvent[] = [];
  const positions = new Map<string, number>();
  for (const event of events) {
    const rawType = event.rawType ?? '';
    const streaming = rawType.endsWith('.message.partial') || rawType.endsWith('.message');
    const turn = streaming ? turnOf(event) : null;
    if (turn === null) {
      result.push(event);
      continue;
    }
    const key = `${event.runId}:${turn}`;
    const position = positions.get(key);
    if (position === undefined) {
      positions.set(key, result.length);
      result.push(isReset(event) ? withText(event, '') : event);
      continue;
    }
    if (rawType.endsWith('.message')) {
      result[position] = event;
    } else if (isReset(event)) {
      result[position] = withText(result[position], '');
    } else {
      result[position] = withText(result[position], textOf(result[position]) + textOf(event));
    }
  }
  return result;
}
