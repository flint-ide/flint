// What the boot script (src/boot.ts) needs before the first paint: the theme of
// the cached settings, without loading the rest of the settings code.

import { resolveTheme, type ResolvedTheme, type ThemeMode } from './themeMode'

const MODES: readonly string[] = ['dark', 'light', 'system', 'custom']

/** The theme to paint first, from the settings cached in localStorage (any JSON, or null). */
export function bootTheme(cached: string | null, systemDark: boolean): ResolvedTheme {
  try {
    const s: unknown = cached ? JSON.parse(cached) : null
    if (!s || typeof s !== 'object') return 'dark'
    const { theme, customTheme } = s as { theme?: unknown; customTheme?: { base?: unknown } }
    const mode = (typeof theme === 'string' && MODES.includes(theme) ? theme : 'dark') as ThemeMode
    const base: ResolvedTheme = customTheme?.base === 'light' ? 'light' : 'dark'
    return resolveTheme(mode, systemDark, base)
  } catch {
    return 'dark'
  }
}

/** The cached sidebar width in pixels, or null when there is none worth using. */
export function bootSidebarWidth(cached: string | null): number | null {
  const n = Number(cached)
  return Number.isFinite(n) && n >= 120 && n <= 1200 ? Math.round(n) : null
}
