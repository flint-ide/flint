// Contributed color themes (lib/features.ts ThemeContribution) and the custom
// theme setting: choosing one copies it into settings.customTheme, which Go
// checks on save like any custom theme. Framework-free.
import type { ThemeContribution } from './features'
import type { CoreSettings, CustomTheme } from './settings'

/** The custom theme setting for a contributed theme. */
export function customThemeFrom(t: ThemeContribution): CustomTheme {
  return { name: t.label, base: t.base, colors: { ...t.colors }, terminal: { ...t.terminal }, source: t.id }
}

/** The contributed theme in use, if Theme is Custom and was taken from one of themes. */
export function activeThemeId(s: Pick<CoreSettings, 'theme' | 'customTheme'>, themes: readonly ThemeContribution[]): string {
  if (s.theme !== 'custom' || !s.customTheme.source) return ''
  return themes.some((t) => t.id === s.customTheme.source) ? s.customTheme.source : ''
}

/** Themes grouped by who offers them, groups and themes in the order given. */
export function groupThemes(themes: readonly ThemeContribution[]): { group: string; themes: ThemeContribution[] }[] {
  const out: { group: string; themes: ThemeContribution[] }[] = []
  for (const t of themes) {
    const g = out.find((x) => x.group === t.group)
    if (g) g.themes.push(t)
    else out.push({ group: t.group, themes: [t] })
  }
  return out
}
