export type Theme = 'light' | 'dark';

const THEME_KEY = 'axyra-theme';

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
}

export function initialTheme(): Theme {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved === 'light' || saved === 'dark') return saved;
  } catch {
    // The theme still works for this session when storage is unavailable.
  }
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export function saveTheme(theme: Theme) {
  applyTheme(theme);
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {
    // Storage can be disabled; keep the selection for this page session.
  }
}
