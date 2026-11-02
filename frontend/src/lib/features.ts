// The feature-module contract, frontend half (Go: internal/feature). A feature
// is one directory, src/features/<id>, whose index.ts default-exports a
// FeatureModule, plus one line in src/features/index.ts. Everything it adds is
// declared here as data; features/install.ts hands it to the registries:
//
//   settings       sections of settings.json it owns (lib/settingsSections.ts)
//   panels         sidebar panels (panels/registry.ts), with their "Show …" commands
//   commands       palette commands and shortcuts (lib/commands.ts)
//   settingsPages  pages in the Settings tab
//   statusItems    status bar items, after Flint's own
//   tabKinds       editor tab views, opened with useEditor().openContributed
//   fileMenu       entries of the file tree's context menu
//   themes         color themes offered in Settings ▸ Appearance
//   activate       runs once after the first render (event subscriptions); may return a cleanup
//
// Components are types only, so React.lazy or lazyPreload components keep their
// code out of the startup bundle. The same shapes are what an external
// extension's manifest is translated into (docs/design/extensions.md), which is
// why entries carry ids and orders instead of positions in a list.
// Framework-free (React only as a type), so it is testable with node:test.
import type { ComponentType } from 'react'
import type { Command } from './commands'
import type { SettingsSection } from './settingsSections'
import type { PanelSpec } from '../panels/registry'
import type { IconName } from '../components/ui/icons'
import type { ResolvedTheme } from './themeMode'

/** A page of the Settings tab. */
export interface SettingsPage {
  /** Unique among pages. */
  id: string
  label: string
  /** Position in the list, ascending; Flint's own pages are 10 (Appearance) to 160 (Import), see SettingsView. */
  order: number
  /** active: the Settings tab is the visible one. */
  component: ComponentType<{ active: boolean }>
}

/** A status bar item; contributed items follow Flint's own, at the right end. */
export interface StatusItem {
  id: string
  /** Position among contributed items, ascending. */
  order: number
  /** Renders nothing (null) while it has nothing to show. */
  component: ComponentType
}

/** An editor tab opened by a feature: data is whatever its view needs (JSON, so a session can keep it). */
export interface ContributedTab {
  id: string
  kind: 'contrib'
  /** The TabKind that renders it. */
  view: string
  title: string
  dirty: boolean // always false
  data: unknown
}

/** A kind of editor tab. */
export interface TabKind {
  /** Unique; ContributedTab.view. */
  id: string
  component: ComponentType<{ tab: ContributedTab; active: boolean }>
  /** Tab strip icon; none shows the title only. */
  icon?: IconName
}

/** An entry of the file tree's context menu, for the right-clicked paths. */
export interface FileMenuItem {
  id: string
  label: string
  /** Offered for these paths; default: always. */
  when?: (paths: readonly string[]) => boolean
  run: (paths: readonly string[]) => void | Promise<unknown>
}

/**
 * A color theme offered in Settings ▸ Appearance. Choosing it copies it into
 * the custom theme (settings.customTheme, source = id), so saving checks it
 * with the custom theme rules in Go, as for an imported VS Code theme.
 */
export interface ThemeContribution {
  /** Unique among themes; becomes customTheme.source. */
  id: string
  label: string
  /** Who offers it, shown next to the label ("Nord", the extension's name). */
  group: string
  base: ResolvedTheme
  /** CSS variable → hex color (THEME_VARIABLES). */
  colors: Readonly<Record<string, string>>
  /** xterm color → hex color (TERMINAL_COLOR_NAMES). */
  terminal: Readonly<Record<string, string>>
}

export interface FeatureModule {
  /** The feature's id, the same as its Go module's (internal/features/<id>). */
  id: string
  /**
   * A built-in extension that can be switched off in Settings ▸ Extensions (Go:
   * feature.Module.Optional); name and description are shown there.
   */
  optional?: boolean
  name?: string
  description?: string
  settings?: readonly SettingsSection[]
  panels?: readonly PanelSpec[]
  commands?: readonly Command[]
  settingsPages?: readonly SettingsPage[]
  statusItems?: readonly StatusItem[]
  tabKinds?: readonly TabKind[]
  fileMenu?: readonly FileMenuItem[]
  themes?: readonly ThemeContribution[]
  /** Runs once after the first render; the returned function undoes it. */
  activate?: () => void | (() => void)
}

/** Types a feature module (an identity function, for inference and checks in one place). */
export function defineFeature<const T extends FeatureModule>(m: T): T {
  return m
}

/**
 * Fails loudly on what would make two features collide: duplicate feature ids,
 * or duplicate ids of the same contribution kind (two panels with one id would
 * share a button; two commands, a shortcut).
 */
export function checkFeatures(mods: readonly FeatureModule[]): void {
  const kinds: [string, (m: FeatureModule) => readonly { id: string }[] | undefined][] = [
    ['feature', (m) => [m]],
    ['panel', (m) => m.panels],
    ['command', (m) => m.commands],
    ['settings page', (m) => m.settingsPages],
    ['status item', (m) => m.statusItems],
    ['tab kind', (m) => m.tabKinds],
    ['file menu item', (m) => m.fileMenu],
    ['theme', (m) => m.themes],
  ]
  for (const [kind, of] of kinds) {
    const seen = new Set<string>()
    for (const m of mods) {
      for (const { id } of of(m) ?? []) {
        if (!id) throw new Error(`${m.id}: ${kind} without an id`)
        if (seen.has(id)) throw new Error(`duplicate ${kind} id: ${id}`)
        seen.add(id)
      }
    }
  }
  const keys = new Set<string>()
  for (const m of mods) {
    for (const s of m.settings ?? []) {
      if (keys.has(s.key)) throw new Error(`duplicate settings section: ${s.key}`)
      keys.add(s.key)
    }
  }
}
