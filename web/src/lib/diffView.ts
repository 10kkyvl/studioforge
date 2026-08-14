import type { DiffLine } from './types';

export type DiffRow<L extends DiffLine = DiffLine> =
  | { kind: 'line'; line: L }
  | { kind: 'collapsed'; count: number; lines: L[] };

export function collapseContext<L extends DiffLine = DiffLine>(lines: L[], keep = 3): DiffRow<L>[] {
  const rows: DiffRow<L>[] = [];
  let i = 0;
  while (i < lines.length) {
    if (lines[i].type !== 'context') {
      rows.push({ kind: 'line', line: lines[i] });
      i++;
      continue;
    }
    let j = i;
    while (j < lines.length && lines[j].type === 'context') j++;
    const run = lines.slice(i, j);
    const atStart = i === 0;
    const atEnd = j === lines.length;
    const keepBefore = atStart && !atEnd ? 0 : keep;
    const keepAfter = atEnd && !atStart ? 0 : keep;
    const hiddenCount = run.length - keepBefore - keepAfter;
    if (hiddenCount >= 2) {
      for (const line of run.slice(0, keepBefore)) rows.push({ kind: 'line', line });
      const hidden = run.slice(keepBefore, run.length - keepAfter);
      rows.push({ kind: 'collapsed', count: hidden.length, lines: hidden });
      for (const line of run.slice(run.length - keepAfter)) rows.push({ kind: 'line', line });
    } else {
      for (const line of run) rows.push({ kind: 'line', line });
    }
    i = j;
  }
  return rows;
}

export type SideRow<L extends DiffLine = DiffLine> = { left: L | null; right: L | null };

export function toSideBySide<L extends DiffLine = DiffLine>(lines: L[]): SideRow<L>[] {
  const rows: SideRow<L>[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.type === 'context') {
      rows.push({ left: line, right: line });
      i++;
      continue;
    }
    const deletes: L[] = [];
    while (i < lines.length && lines[i].type === 'delete') {
      deletes.push(lines[i]);
      i++;
    }
    const adds: L[] = [];
    while (i < lines.length && lines[i].type === 'add') {
      adds.push(lines[i]);
      i++;
    }
    const max = Math.max(deletes.length, adds.length);
    for (let k = 0; k < max; k++) {
      rows.push({ left: deletes[k] ?? null, right: adds[k] ?? null });
    }
  }
  return rows;
}

const VIEW_MODE_KEY = 'studioforge-diff-view';

export type DiffViewMode = 'unified' | 'split';

export function loadDiffViewMode(): DiffViewMode {
  try {
    return localStorage.getItem(VIEW_MODE_KEY) === 'split' ? 'split' : 'unified';
  } catch {
    return 'unified';
  }
}

export function saveDiffViewMode(mode: DiffViewMode): void {
  try {
    localStorage.setItem(VIEW_MODE_KEY, mode);
  } catch {}
}

export function resolveViewMode(width: number, mode: DiffViewMode): DiffViewMode {
  return width > 0 && width < 700 ? 'unified' : mode;
}
