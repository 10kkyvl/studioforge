import { afterEach, describe, expect, it, vi } from 'vitest';
import { loadProject, loadView, saveProject, saveView } from './session';

// A tiny in-memory stand-in for localStorage. The failure mode worth covering
// is not "does it round-trip" but what happens when the stored value no longer
// matches reality — a removed view or a deleted project must not strand the
// operator on a screen that cannot render.
function stubStorage(initial: Record<string, string> = {}) {
  const store = new Map(Object.entries(initial));
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, value),
    removeItem: (key: string) => void store.delete(key),
  });
  return store;
}

afterEach(() => vi.unstubAllGlobals());

const VIEWS = ['chat', 'projects', 'settings'] as const;

describe('session restore', () => {
  it('restores a view that still exists', () => {
    stubStorage({ 'studioforge-view': 'chat' });
    expect(loadView(VIEWS)).toBe('chat');
  });

  it('ignores a view this build no longer offers', () => {
    stubStorage({ 'studioforge-view': 'retired-view' });
    expect(loadView(VIEWS)).toBe('');
  });

  it('ignores an empty store', () => {
    stubStorage();
    expect(loadView(VIEWS)).toBe('');
  });

  it('restores a project that still exists', () => {
    stubStorage({ 'studioforge-project': 'p2' });
    expect(loadProject([{ id: 'p1' }, { id: 'p2' }])).toBe('p2');
  });

  it('ignores a project deleted since last session', () => {
    stubStorage({ 'studioforge-project': 'gone' });
    expect(loadProject([{ id: 'p1' }])).toBe('');
  });

  it('round-trips what it saved', () => {
    stubStorage();
    saveView('settings');
    saveProject('p9');
    expect(loadView(VIEWS)).toBe('settings');
    expect(loadProject([{ id: 'p9' }])).toBe('p9');
  });

  it('clears the stored project when the selection is emptied', () => {
    const store = stubStorage({ 'studioforge-project': 'p1' });
    saveProject('');
    expect(store.has('studioforge-project')).toBe(false);
  });

  // Safari private mode and storage-blocked embeddings throw instead of
  // returning null. A remembered view is a convenience, never a hard failure.
  it('degrades to a fresh start when storage throws', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('storage disabled');
      },
      setItem: () => {
        throw new Error('storage disabled');
      },
      removeItem: () => {},
    });
    expect(loadView(VIEWS)).toBe('');
    expect(() => saveView('chat')).not.toThrow();
  });
});
