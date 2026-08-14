import { describe, expect, it, vi } from 'vitest';
import type { DiffLine } from './types';
import {
  collapseContext,
  loadDiffViewMode,
  resolveViewMode,
  saveDiffViewMode,
  toSideBySide,
} from './diffView';

function context(n: number): DiffLine {
  return { type: 'context', oldNo: n, newNo: n, text: `ctx ${n}` };
}
function add(n: number): DiffLine {
  return { type: 'add', oldNo: null, newNo: n, text: `add ${n}` };
}
function del(n: number): DiffLine {
  return { type: 'delete', oldNo: n, newNo: null, text: `del ${n}` };
}

function stubStorage(initial: Record<string, string> = {}) {
  const store = new Map(Object.entries(initial));
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, value),
    removeItem: (key: string) => void store.delete(key),
  });
  return store;
}

describe('collapseContext', () => {
  it('collapses a leading run down to the last `keep` lines', () => {
    const lines = [context(1), context(2), context(3), context(4), context(5), add(6)];
    const rows = collapseContext(lines, 3);
    expect(rows[0]).toEqual({ kind: 'collapsed', count: 2, lines: [context(1), context(2)] });
    expect(rows.slice(1, 4)).toEqual([
      { kind: 'line', line: context(3) },
      { kind: 'line', line: context(4) },
      { kind: 'line', line: context(5) },
    ]);
    expect(rows[4]).toEqual({ kind: 'line', line: add(6) });
  });

  it('collapses a trailing run down to the first `keep` lines', () => {
    const lines = [del(1), context(2), context(3), context(4), context(5), context(6)];
    const rows = collapseContext(lines, 3);
    expect(rows[0]).toEqual({ kind: 'line', line: del(1) });
    expect(rows.slice(1, 4)).toEqual([
      { kind: 'line', line: context(2) },
      { kind: 'line', line: context(3) },
      { kind: 'line', line: context(4) },
    ]);
    expect(rows[4]).toEqual({ kind: 'collapsed', count: 2, lines: [context(5), context(6)] });
  });

  it('collapses an interior run keeping `keep` lines on each side', () => {
    const lines = [
      add(1),
      context(2),
      context(3),
      context(4),
      context(5),
      context(6),
      context(7),
      context(8),
      context(9),
      del(10),
    ];
    const rows = collapseContext(lines, 3);
    expect(rows).toHaveLength(9);
    expect(rows[0]).toEqual({ kind: 'line', line: add(1) });
    expect(rows.slice(1, 4)).toEqual([
      { kind: 'line', line: context(2) },
      { kind: 'line', line: context(3) },
      { kind: 'line', line: context(4) },
    ]);
    expect(rows[4]).toEqual({ kind: 'collapsed', count: 2, lines: [context(5), context(6)] });
    expect(rows.slice(5, 8)).toEqual([
      { kind: 'line', line: context(7) },
      { kind: 'line', line: context(8) },
      { kind: 'line', line: context(9) },
    ]);
    expect(rows[8]).toEqual({ kind: 'line', line: del(10) });
  });

  it('does not collapse a short run that would hide fewer than 2 lines', () => {
    const lines = [add(1), context(2), context(3), context(4), del(5)];
    const rows = collapseContext(lines, 3);
    expect(rows.every((row) => row.kind === 'line')).toBe(true);
    expect(rows).toHaveLength(5);
  });

  it('does not collapse an interior run that would hide exactly 1 line', () => {
    const lines = [
      add(1),
      context(2),
      context(3),
      context(4),
      context(5),
      context(6),
      context(7),
      context(8),
      del(9),
    ];
    const rows = collapseContext(lines, 3);
    expect(rows.some((row) => row.kind === 'collapsed')).toBe(false);
    expect(rows).toHaveLength(9);
  });

  it('collapses an interior run that hides exactly 2 lines', () => {
    const lines = [
      add(1),
      context(2),
      context(3),
      context(4),
      context(5),
      context(6),
      context(7),
      context(8),
      context(9),
      del(10),
    ];
    const rows = collapseContext(lines, 3);
    expect(rows.some((row) => row.kind === 'collapsed' && row.count === 2)).toBe(true);
  });

  it('renders an all-context hunk with `keep` kept on each end', () => {
    const lines = [
      context(1),
      context(2),
      context(3),
      context(4),
      context(5),
      context(6),
      context(7),
      context(8),
    ];
    const rows = collapseContext(lines, 3);
    expect(rows[0]).toEqual({ kind: 'line', line: context(1) });
    expect(rows[3]).toEqual({ kind: 'collapsed', count: 2, lines: [context(4), context(5)] });
  });
});

describe('toSideBySide', () => {
  it('mirrors context lines on both sides', () => {
    const rows = toSideBySide([context(1), context(2)]);
    expect(rows).toEqual([
      { left: context(1), right: context(1) },
      { left: context(2), right: context(2) },
    ]);
  });

  it('aligns a pure-add block with a null left side', () => {
    const rows = toSideBySide([add(1), add(2)]);
    expect(rows).toEqual([
      { left: null, right: add(1) },
      { left: null, right: add(2) },
    ]);
  });

  it('aligns a pure-delete block with a null right side', () => {
    const rows = toSideBySide([del(1), del(2)]);
    expect(rows).toEqual([
      { left: del(1), right: null },
      { left: del(2), right: null },
    ]);
  });

  it('pairs a replace block index-by-index with leftovers against null', () => {
    const rows = toSideBySide([del(1), del(2), del(3), add(1), add(2)]);
    expect(rows).toEqual([
      { left: del(1), right: add(1) },
      { left: del(2), right: add(2) },
      { left: del(3), right: null },
    ]);
  });
});

describe('diff view mode storage', () => {
  it('defaults to unified when storage is empty', () => {
    stubStorage();
    expect(loadDiffViewMode()).toBe('unified');
    vi.unstubAllGlobals();
  });

  it('round-trips a saved mode', () => {
    stubStorage();
    saveDiffViewMode('split');
    expect(loadDiffViewMode()).toBe('split');
    vi.unstubAllGlobals();
  });

  it('degrades to unified when storage throws', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    });
    expect(loadDiffViewMode()).toBe('unified');
    expect(() => saveDiffViewMode('split')).not.toThrow();
    vi.unstubAllGlobals();
  });
});

describe('resolveViewMode', () => {
  it('keeps the requested mode when the width is unknown', () => {
    expect(resolveViewMode(0, 'split')).toBe('split');
    expect(resolveViewMode(0, 'unified')).toBe('unified');
  });

  it('falls back to unified below the narrow-viewport threshold', () => {
    expect(resolveViewMode(520, 'split')).toBe('unified');
  });

  it('keeps split above the narrow-viewport threshold', () => {
    expect(resolveViewMode(900, 'split')).toBe('split');
  });

  it('keeps unified regardless of width', () => {
    expect(resolveViewMode(520, 'unified')).toBe('unified');
    expect(resolveViewMode(900, 'unified')).toBe('unified');
  });

  it('keeps the requested mode exactly at the threshold', () => {
    expect(resolveViewMode(700, 'split')).toBe('split');
    expect(resolveViewMode(700, 'unified')).toBe('unified');
  });
});
