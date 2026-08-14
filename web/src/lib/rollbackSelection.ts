import type { RollbackSelection } from './types';

export type RollbackSelectionState = {
  files: Set<string>;
  hunks: Map<string, Set<number>>;
};

export function emptySelection(): RollbackSelectionState {
  return { files: new Set(), hunks: new Map() };
}

export function toggleFile(sel: RollbackSelectionState, path: string): RollbackSelectionState {
  const files = new Set(sel.files);
  const hunks = new Map(sel.hunks);
  if (files.has(path)) {
    files.delete(path);
  } else {
    files.add(path);
    hunks.delete(path);
  }
  return { files, hunks };
}

export function toggleHunk(
  sel: RollbackSelectionState,
  path: string,
  index: number,
): RollbackSelectionState {
  const files = new Set(sel.files);
  files.delete(path);
  const hunks = new Map(sel.hunks);
  const existing = new Set(hunks.get(path) ?? []);
  if (existing.has(index)) {
    existing.delete(index);
  } else {
    existing.add(index);
  }
  if (existing.size === 0) {
    hunks.delete(path);
  } else {
    hunks.set(path, existing);
  }
  return { files, hunks };
}

export function selectionCount(sel: RollbackSelectionState): { files: number; hunks: number } {
  let hunks = 0;
  for (const indices of sel.hunks.values()) hunks += indices.size;
  return { files: sel.files.size, hunks };
}

export function toApiSelection(sel: RollbackSelectionState): RollbackSelection {
  const hunks: { path: string; index: number }[] = [];
  for (const [path, indices] of sel.hunks) {
    for (const index of indices) hunks.push({ path, index });
  }
  return { files: [...sel.files], hunks };
}
