import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  applyStoredTheme,
  normalizeFontSize,
  resolveTheme,
  setThemeColorMeta,
  themeColorFor,
} from './theme';

function stubMatchMedia(prefersLight: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('light') ? prefersLight : !prefersLight,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
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

afterEach(() => {
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-font-size');
  document.querySelectorAll('meta[name="theme-color"]').forEach((el) => el.remove());
});

describe('resolveTheme', () => {
  it('honors an explicit dark choice regardless of the OS preference', () => {
    stubMatchMedia(true);
    expect(resolveTheme('dark')).toBe('dark');
  });

  it('honors an explicit light choice regardless of the OS preference', () => {
    stubMatchMedia(false);
    expect(resolveTheme('light')).toBe('light');
  });

  it('falls back to the OS preference for "system"', () => {
    stubMatchMedia(true);
    expect(resolveTheme('system')).toBe('light');
    stubMatchMedia(false);
    expect(resolveTheme('system')).toBe('dark');
  });
});

describe('themeColorFor', () => {
  it('picks the dark meta color for dark themes', () => {
    expect(themeColorFor('dark')).toBe('#0a0e14');
  });

  it('picks the light meta color for light themes', () => {
    expect(themeColorFor('light')).toBe('#f4f6f9');
  });
});

describe('normalizeFontSize', () => {
  it('accepts a known size', () => {
    expect(normalizeFontSize('large')).toBe('large');
  });

  it('falls back to comfortable for anything unrecognized', () => {
    expect(normalizeFontSize('huge')).toBe('comfortable');
  });
});

describe('setThemeColorMeta', () => {
  it('replaces any existing theme-color meta tags with a single one', () => {
    const stale = document.createElement('meta');
    stale.setAttribute('name', 'theme-color');
    stale.setAttribute('content', '#000000');
    document.head.appendChild(stale);

    setThemeColorMeta('#f4f6f9');

    const tags = document.querySelectorAll('meta[name="theme-color"]');
    expect(tags.length).toBe(1);
    expect(tags[0].getAttribute('content')).toBe('#f4f6f9');
  });
});

describe('applyStoredTheme', () => {
  it('applies the stored theme and font size before any component mounts', () => {
    stubStorage({ 'studioforge-theme': 'light', 'studioforge-font-size': 'large' });
    applyStoredTheme();
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.dataset.fontSize).toBe('large');
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe(
      '#f4f6f9',
    );
  });

  it('degrades to the system default when storage is empty or throws', () => {
    stubStorage();
    applyStoredTheme();
    expect(document.documentElement.dataset.theme).toBe('system');
    expect(document.documentElement.dataset.fontSize).toBe('comfortable');
  });
});
