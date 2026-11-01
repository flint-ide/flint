// Theme modes and how they resolve to a palette. A module of its own so the
// boot script (src/boot.ts) can use it without the rest of the settings code.

export type ThemeMode = 'dark' | 'light' | 'system' | 'custom'
export type ResolvedTheme = 'dark' | 'light'

/** The palette to use: System follows macOS (prefers-color-scheme), Custom its base palette. */
export function resolveTheme(mode: ThemeMode, systemDark: boolean, customBase: ResolvedTheme = 'dark'): ResolvedTheme {
  if (mode === 'system') return systemDark ? 'dark' : 'light'
  if (mode === 'custom') return customBase
  return mode
}
