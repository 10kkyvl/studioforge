export type FontSize = 'compact' | 'comfortable' | 'large';
export type ResolvedTheme = 'dark' | 'light';

export const THEME_KEY = 'studioforge-theme';
export const FONT_SIZE_KEY = 'studioforge-font-size';
const FONT_SIZES: readonly FontSize[] = ['compact', 'comfortable', 'large'];

const THEME_COLOR_DARK = '#0b0e15';
const THEME_COLOR_LIGHT = '#f2f4f8';

let schemeWatcherBound = false;

export function resolveTheme(value: string): ResolvedTheme {
  if (value === 'light') return 'light';
  if (value === 'dark') return 'dark';
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: light)').matches
    ? 'light'
    : 'dark';
}

export function themeColorFor(value: string): string {
  return resolveTheme(value) === 'light' ? THEME_COLOR_LIGHT : THEME_COLOR_DARK;
}

export function normalizeFontSize(value: string): FontSize {
  return (FONT_SIZES as readonly string[]).includes(value) ? (value as FontSize) : 'comfortable';
}

export function setThemeColorMeta(content: string): void {
  document.querySelectorAll('meta[name="theme-color"]').forEach((el) => el.remove());
  const meta = document.createElement('meta');
  meta.setAttribute('name', 'theme-color');
  meta.setAttribute('content', content);
  document.head.appendChild(meta);
}

export function applyTheme(theme: string): void {
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.scheme = resolveTheme(theme);
  setThemeColorMeta(themeColorFor(theme));
}

export function watchSystemScheme(): void {
  if (schemeWatcherBound || typeof matchMedia !== 'function') return;
  const query = matchMedia('(prefers-color-scheme: light)');
  if (typeof query.addEventListener !== 'function') return;
  query.addEventListener('change', () => {
    const theme = document.documentElement.dataset.theme ?? 'system';
    if (theme === 'system') applyTheme(theme);
  });
  schemeWatcherBound = true;
}

function readStorage(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function applyStoredTheme(): void {
  const theme = readStorage(THEME_KEY) ?? 'system';
  document.documentElement.dataset.fontSize = normalizeFontSize(
    readStorage(FONT_SIZE_KEY) ?? 'comfortable',
  );
  applyTheme(theme);
  watchSystemScheme();
}
