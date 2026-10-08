export type Theme = 'dark' | 'less-dark'

const STORAGE_KEY = 'stevie.theme'

export function getStoredTheme(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'less-dark' || v === 'light') return 'less-dark'
    if (v === 'dark') return 'dark'
  } catch {
    /* ignore */
  }
  return 'dark'
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
  // Both themes are dark-leaning; keep native controls on dark color scheme.
  document.documentElement.style.colorScheme = 'dark'
}

export function setTheme(theme: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    /* ignore */
  }
  applyTheme(theme)
}
