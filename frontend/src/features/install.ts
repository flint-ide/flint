// Hands the feature modules (features/index.ts) to the shell's registries in
// two steps. installFeatures runs in main.tsx before the first render, so the
// activity bar, Settings and the status bar draw complete, and the settings
// store fills in the sections; it is data only (no backend calls, no
// listeners). activateFeatures runs in App's first effect, after Flint's own
// commands, and registers commands (including each panel's "Show …") and runs
// activate hooks.
//
// Built-in extensions (optional modules) run only when Go built them: a switched-off
// one has no backend, so its panels and commands are left out too. Before Go answers,
// the modules it ran last time are installed (cached), so nothing flickers; then
// syncActiveFeatures makes the installed set what Go reports.
import { FEATURES } from '.'
import * as AppAPI from '../../bindings/flint/appapi'
import { checkFeatures, type FeatureModule } from '../lib/features'
import { contribute } from '../lib/contributions'
import { registerSections } from '../lib/settingsSections'
import { CORE_KEYS } from '../lib/settings'
import { registerCommands } from '../lib/commands'
import { coerceToggles, initialActive } from '../lib/featureToggles'
import { useActiveFeatures } from '../lib/activeFeatures'
import { panelCommands } from '../panels/registry'
import { useSettings } from '../store/settings'
import { useWorkspace } from '../store/workspace'

const ACTIVE_CACHE_KEY = 'flint.features.active'

interface Installed {
  withdraw: () => void
  /** Undoes commands and the activate hook; null until activated. */
  deactivate: (() => void) | null
}

let modules: readonly FeatureModule[] = []
const installed = new Map<string, Installed>()
let activated = false

/** Registers the modules' data contributions; once per page. */
export function installFeatures(mods: readonly FeatureModule[] = FEATURES): void {
  if (modules.length) return
  checkFeatures(mods)
  modules = mods
  // Every module's settings are registered, also of switched-off ones: saving keeps them.
  for (const m of mods) registerSections(m.settings ?? [], CORE_KEYS)
  const active = initialActive(mods, readActiveCache(), coerceToggles(useSettings.getState().settings.features))
  for (const m of mods) if (active.has(m.id)) install(m)
  publish()
}

/** Registers the modules' commands and runs their activate hooks; the result undoes both. */
export function activateFeatures(): () => void {
  activated = true
  for (const m of modules) {
    const it = installed.get(m.id)
    if (it && !it.deactivate) it.deactivate = activate(m)
  }
  return () => {
    activated = false
    for (const m of [...modules].reverse()) {
      const it = installed.get(m.id)
      it?.deactivate?.()
      if (it) it.deactivate = null
    }
  }
}

/** Asks Go which features run and installs exactly those (once, at startup). */
export async function syncActiveFeatures(): Promise<void> {
  try {
    setActiveFeatures((await AppAPI.Features()) ?? [])
  } catch {
    // An older backend or a failed call: keep what the cache and settings said.
  }
}

/** Makes the installed modules the given ones (and remembers them for the next start). */
export function setActiveFeatures(ids: readonly string[]): void {
  const want = new Set(ids)
  for (const m of modules) {
    if (want.has(m.id) && !installed.has(m.id)) install(m)
    else if (!want.has(m.id) && installed.has(m.id)) uninstall(m.id)
  }
  writeActiveCache(ids)
  publish()
}

function install(m: FeatureModule): void {
  const withdraw = contribute(`feature:${m.id}`, {
    panels: m.panels, settingsPages: m.settingsPages, statusItems: m.statusItems, tabKinds: m.tabKinds, fileMenu: m.fileMenu, themes: m.themes, decorationSets: m.decorationSets,
  })
  installed.set(m.id, { withdraw, deactivate: activated ? activate(m) : null })
}

function uninstall(id: string): void {
  const it = installed.get(id)
  it?.deactivate?.()
  it?.withdraw()
  installed.delete(id)
}

function activate(m: FeatureModule): () => void {
  const show = (id: string) => useWorkspace.getState().setSidebar(id)
  const offCommands = registerCommands([...panelCommands(m.panels ?? [], show), ...(m.commands ?? [])])
  const undo = m.activate?.()
  return () => {
    if (undo) undo()
    offCommands()
  }
}

function publish(): void {
  useActiveFeatures.setState({ ids: new Set(installed.keys()) })
}

function readActiveCache(): string[] | null {
  try {
    const raw = localStorage.getItem(ACTIVE_CACHE_KEY)
    const ids: unknown = raw ? JSON.parse(raw) : null
    return Array.isArray(ids) && ids.every((id) => typeof id === 'string') ? ids : null
  } catch {
    return null
  }
}

function writeActiveCache(ids: readonly string[]): void {
  try {
    localStorage.setItem(ACTIVE_CACHE_KEY, JSON.stringify(ids))
  } catch {
    // Storage blocked: the next start installs what the settings say.
  }
}
