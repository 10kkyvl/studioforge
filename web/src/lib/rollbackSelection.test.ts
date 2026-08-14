import { describe, expect, it } from 'vitest';
import {
  emptySelection,
  selectionCount,
  toApiSelection,
  toggleFile,
  toggleHunk,
  type RollbackSelectionState,
} from './rollbackSelection';

describe('toggleFile', () => {
  it('checks an unselected file', () => {
    const sel = toggleFile(emptySelection(), 'a.lua');
    expect(sel.files.has('a.lua')).toBe(true);
  });

  it('unchecks a selected file', () => {
    let sel = toggleFile(emptySelection(), 'a.lua');
    sel = toggleFile(sel, 'a.lua');
    expect(sel.files.has('a.lua')).toBe(false);
  });

  it('clears that path hunk selections when the file is checked', () => {
    let sel = emptySelection();
    sel = toggleHunk(sel, 'a.lua', 0);
    sel = toggleHunk(sel, 'a.lua', 1);
    sel = toggleFile(sel, 'a.lua');
    expect(sel.files.has('a.lua')).toBe(true);
    expect(sel.hunks.has('a.lua')).toBe(false);
  });

  it('leaves other paths untouched', () => {
    let sel = emptySelection();
    sel = toggleHunk(sel, 'b.lua', 2);
    sel = toggleFile(sel, 'a.lua');
    expect(sel.hunks.get('b.lua')).toEqual(new Set([2]));
  });
});

describe('toggleHunk', () => {
  it('checks an unselected hunk', () => {
    const sel = toggleHunk(emptySelection(), 'a.lua', 0);
    expect(sel.hunks.get('a.lua')).toEqual(new Set([0]));
  });

  it('unchecks a selected hunk', () => {
    let sel = toggleHunk(emptySelection(), 'a.lua', 0);
    sel = toggleHunk(sel, 'a.lua', 0);
    expect(sel.hunks.has('a.lua')).toBe(false);
  });

  it('keeps sibling hunks for the same path when one is unchecked', () => {
    let sel = emptySelection();
    sel = toggleHunk(sel, 'a.lua', 0);
    sel = toggleHunk(sel, 'a.lua', 1);
    sel = toggleHunk(sel, 'a.lua', 0);
    expect(sel.hunks.get('a.lua')).toEqual(new Set([1]));
  });

  it('clears that path file selection when a hunk is checked', () => {
    let sel = toggleFile(emptySelection(), 'a.lua');
    sel = toggleHunk(sel, 'a.lua', 0);
    expect(sel.files.has('a.lua')).toBe(false);
    expect(sel.hunks.get('a.lua')).toEqual(new Set([0]));
  });

  it('does not affect other paths file selections', () => {
    let sel = toggleFile(emptySelection(), 'b.lua');
    sel = toggleHunk(sel, 'a.lua', 0);
    expect(sel.files.has('b.lua')).toBe(true);
  });
});

describe('selectionCount', () => {
  it('counts files and hunks across multiple paths', () => {
    let sel = emptySelection();
    sel = toggleFile(sel, 'a.lua');
    sel = toggleFile(sel, 'b.lua');
    sel = toggleHunk(sel, 'c.lua', 0);
    sel = toggleHunk(sel, 'c.lua', 1);
    sel = toggleHunk(sel, 'd.lua', 0);
    expect(selectionCount(sel)).toEqual({ files: 2, hunks: 3 });
  });

  it('is zero for an empty selection', () => {
    expect(selectionCount(emptySelection())).toEqual({ files: 0, hunks: 0 });
  });
});

describe('toApiSelection', () => {
  it('flattens files and hunks into the API shape', () => {
    let sel: RollbackSelectionState = emptySelection();
    sel = toggleFile(sel, 'a.lua');
    sel = toggleHunk(sel, 'b.lua', 0);
    sel = toggleHunk(sel, 'b.lua', 2);
    const api = toApiSelection(sel);
    expect(api.files).toEqual(['a.lua']);
    expect(api.hunks).toEqual(
      expect.arrayContaining([
        { path: 'b.lua', index: 0 },
        { path: 'b.lua', index: 2 },
      ]),
    );
    expect(api.hunks).toHaveLength(2);
  });

  it('returns empty arrays for an empty selection', () => {
    expect(toApiSelection(emptySelection())).toEqual({ files: [], hunks: [] });
  });
});
