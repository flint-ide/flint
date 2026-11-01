// An installed extension (host.Info from Go) translated into level-1
// contributions (lib/contributions.ts) and commands (lib/commands.ts), under
// the source extension:<id>. Components come from the caller, so this stays
// framework-free and testable with node:test.
import type { ComponentType } from 'react'
import { canonicalKeys, keyConflicts, listCommands, type Command } from '../../lib/commands'
import type { Contributions } from '../../lib/contributions'
import type { SettingsPage, StatusItem } from '../../lib/features'
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
  settings: { title?: string; properties: readonly SchemaProperty[] } | null
}

/** Contributed entries come after Flint's and the features' own. */
export const EXTENSION_ORDER = 1000

export interface Deps {
  panel: (ext: ExtensionInfo, panel: ExtensionInfo['panels'][number]) => ComponentType<PanelProps>
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
  return JSON.stringify([info.version, info.commands, info.panels, info.statusItems.map((s) => [s.id, s.order]), info.settings])
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
    component: deps.panel(info, p),
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
  return { contributions: { panels, statusItems, settingsPages }, commands, conflicts }
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
