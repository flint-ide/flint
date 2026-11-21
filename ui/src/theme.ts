export type FlintTheme = 'dark' | 'light'

/**
 * Selects the theme on <html> the way Flint does: data-theme picks Flint's
 * variables, the .dark class picks WunderUI's dark tokens. Flint sets both itself;
 * call this from pages it doesn't manage (an extension webview reading
 * flint.theme.dark, a story).
 */
export function applyTheme(theme: FlintTheme, root: HTMLElement = document.documentElement): void {
  root.dataset.theme = theme
  root.classList.toggle('dark', theme === 'dark')
}
