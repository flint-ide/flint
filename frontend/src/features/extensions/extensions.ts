// What ExtensionsAPI.List answers, read as untrusted JSON, and the
// contributions an installed extension makes without a program (color
// themes). Framework-free, so it is testable with node:test.
import type { ThemeContribution } from '../../lib/features'
import { coerceCustomTheme } from '../../lib/settings'

export interface ExtensionTheme {
  id: string
  label: string
  base: 'dark' | 'light'
  colors: Record<string, string>
  terminal: Record<string, string>
}

/** An installed extension (Go: extensions.Extension). */
export interface InstalledExtension {
  id: string
  name: string
  version: string
  publisher: string
  description: string
  dir: string
  /** Why it can't be used; '' when it can. */
  error: string
  permissions: string[]
  /** It has a program, which this version of Flint does not start yet. */
  program: boolean
  /** "2 color themes", "1 command", … */
  contributes: string[]
  themes: ExtensionTheme[]
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const str = (v: unknown, max = 2000): string => (typeof v === 'string' ? v.slice(0, max) : '')
const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').map((x) => x.slice(0, 300)) : [])

/** The list from Go; malformed entries are dropped, unknown colors filtered out. */
export function coerceExtensions(v: unknown): InstalledExtension[] {
  if (!Array.isArray(v)) return []
  return v.flatMap((e) => (isObject(e) && str(e.id) ? [coerceExtension(e)] : []))
}

function coerceExtension(e: Record<string, unknown>): InstalledExtension {
  return {
    id: str(e.id, 200),
    name: str(e.name, 200) || str(e.id, 200),
    version: str(e.version, 100),
    publisher: str(e.publisher, 200),
    description: str(e.description),
    dir: str(e.dir, 4096),
    error: str(e.error),
    permissions: strings(e.permissions),
    program: e.program === true,
    contributes: strings(e.contributes),
    themes: Array.isArray(e.themes) ? e.themes.flatMap(coerceTheme) : [],
  }
}

function coerceTheme(t: unknown): ExtensionTheme[] {
  if (!isObject(t) || !str(t.id) || !str(t.label)) return []
  // The same filter as a custom theme loaded from settings.json.
  const c = coerceCustomTheme({ name: t.label, base: t.base, colors: t.colors, terminal: t.terminal })
  return [{ id: str(t.id, 200), label: str(t.label, 100), base: c.base, colors: c.colors, terminal: c.terminal }]
}

/** The color themes an extension offers in Settings ▸ Appearance. */
export function themeContributions(e: InstalledExtension): ThemeContribution[] {
  if (e.error) return []
  return e.themes.map((t) => ({ id: t.id, label: t.label, group: e.name, base: t.base, colors: t.colors, terminal: t.terminal }))
}
