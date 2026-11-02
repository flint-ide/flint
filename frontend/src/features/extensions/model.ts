// An installed extension (host.Info from Go) translated into level-1
// contributions (lib/contributions.ts) and commands (lib/commands.ts), under
// the source extension:<id>. Components come from the caller, so this stays
// framework-free and testable with node:test.
import type { ComponentType } from 'react'
import { canonicalKeys, keyConflicts, listCommands, type Command } from '../../lib/commands'
import type { Contributions } from '../../lib/contributions'
import type { ContributedTab, SettingsPage, StatusItem, TabKind, ThemeContribution } from '../../lib/features'
import { coerceCustomTheme } from '../../lib/settings'
import type { PanelProps, PanelSpec } from '../../panels/registry'
import { isIconName } from '../../components/ui/icons'
import type { SchemaProperty } from './schema'

/** The parts of host.Info this file reads. */
export interface ExtensionInfo {
  id: string
  name: string
  version: string
  enabled: boolean
  state: string
  commands: readonly { id: string; title: string; category?: string; keys?: string }[]
  panels: readonly { id: string; title: string; icon: string; order: number; view: string }[]
  statusItems: readonly { id: string; order: number }[]
  /** Tab views (webviews); missing from an older backend. */
  tabViews?: readonly { id: string; title: string; icon?: string; view: string; restore?: boolean }[]
  settings: { title?: string; properties: readonly SchemaProperty[] } | null
  /** Color themes; Go checked them with the custom theme rules. */
  themes?: readonly { id: string; label: string; base: string; colors: Readonly<Record<string, unknown>>; terminal?: Readonly<Record<string, unknown>> | null }[]
}

/** Contributed entries come after Flint's and the features' own. */
export const EXTENSION_ORDER = 1000

export interface Deps {
  panel: (ext: ExtensionInfo, panel: ExtensionInfo['panels'][number]) => ComponentType<PanelProps>
  /** A "webview" panel's component. */
  webviewPanel: (ext: ExtensionInfo, panel: ExtensionInfo['panels'][number]) => ComponentType<PanelProps>
  /** A tab view's component. */
  webviewTab: (ext: ExtensionInfo, view: string) => ComponentType<{ tab: ContributedTab; active: boolean }>
  statusItem: (ext: ExtensionInfo, item: string) => ComponentType
  settingsPage: (ext: ExtensionInfo) => ComponentType<{ active: boolean }>
  run: (ext: string, command: string) => Promise<unknown>
  /** Opens a panel (its "Show …" command). */
  show: (panel: string) => void
  /** Commands a default shortcut must not collide with (default: all registered). */
  existing?: readonly Command[]
}

export interface Parts {
  contributions: Partial<Contributions>
  commands: Command[]
  /** Default shortcuts left out, and why. */
  conflicts: string[]
}

/** Whether the extension's contributions are shown at all. */
export function contributes(info: ExtensionInfo): boolean {
  return info.enabled && info.state !== 'invalid'
}

/** Changes when what the extension contributes changes (its status texts don't count). */
export function contributionKey(info: ExtensionInfo): string {
  if (!contributes(info)) return ''
  return JSON.stringify([info.version, info.commands, info.panels, info.statusItems.map((s) => [s.id, s.order]), info.settings, info.themes ?? [], info.tabViews ?? []])
}

/**
 * The extension's color themes, for Settings ▸ Appearance. Colors are
 * filtered again like a custom theme read from settings.json (known names,
 * hex values), whatever Go sent.
 */
export function themeContributions(info: ExtensionInfo): ThemeContribution[] {
  return (info.themes ?? []).flatMap((t) => {
    if (!t.id || !t.label) return []
    const c = coerceCustomTheme({ name: t.label, base: t.base, colors: t.colors, terminal: t.terminal ?? {} })
    return [{ id: t.id, label: t.label, group: info.name, base: c.base, colors: c.colors, terminal: c.terminal }]
  })
}

/** The extension's contributions and commands. */
export function extensionParts(info: ExtensionInfo, deps: Deps): Parts {
  const conflicts: string[] = []
  const existing = deps.existing ?? listCommands()
  const commands: Command[] = info.commands.map((c) => {
    let keys = c.keys || undefined
    if (keys) {
      const why = shortcutConflict(keys, c.id, existing)
      if (why) {
        conflicts.push(`${c.title}: ${keys} ${why}, so it has no shortcut (set one in Settings ▸ Keymap)`)
        keys = undefined
      }
    }
    return {
      id: c.id,
      title: c.title,
      category: c.category || info.name,
      keys,
      keywords: `extension ${info.name}`,
      run: () => deps.run(info.id, c.id),
    }
  })
  const panels: PanelSpec[] = info.panels.map((p) => ({
    id: p.id,
    title: p.title,
    icon: isIconName(p.icon) ? p.icon : 'puzzle',
    order: EXTENSION_ORDER + p.order,
    component: p.view === 'webview' ? deps.webviewPanel(info, p) : deps.panel(info, p),
    shortcutCommand: `view.${p.id}`,
    command: false, // its "Show …" command is below, under the extension's name
  }))
  for (const p of panels) {
    commands.push({ id: `view.${p.id}`, title: `Show ${p.title}`, category: info.name, keywords: `extension ${info.name}`, run: () => deps.show(p.id) })
  }
  const statusItems: StatusItem[] = info.statusItems.map((s) => ({ id: s.id, order: EXTENSION_ORDER + s.order, component: deps.statusItem(info, s.id) }))
  const settingsPages: SettingsPage[] = info.settings?.properties.length
    ? [{ id: `extension:${info.id}`, label: info.settings.title || info.name, order: EXTENSION_ORDER, component: deps.settingsPage(info) }]
    : []
  const tabKinds: TabKind[] = (info.tabViews ?? [])
    .filter((t) => t.view === 'webview')
    .map((t) => ({ id: t.id, icon: t.icon && isIconName(t.icon) ? t.icon : undefined, restore: !!t.restore, component: deps.webviewTab(info, t.id) }))
  return { contributions: { panels, statusItems, settingsPages, tabKinds, themes: themeContributions(info) }, commands, conflicts }
}

/** Why an extension's default shortcut can't be used ('' when it can). */
export function shortcutConflict(keys: string, id: string, existing: readonly Command[]): string {
  try {
    canonicalKeys(keys)
  } catch {
    return 'is not a shortcut Flint understands'
  }
  const taken = keyConflicts(keys, id, existing)
  return taken.length ? `is already ${taken[0].title}` : ''
}
