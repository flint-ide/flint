// Hands the feature modules (features/index.ts) to the shell's registries in
// two steps. installFeatures runs in main.tsx before the first render, so the
// activity bar, Settings and the status bar draw complete, and the settings
// store fills in the sections; it is data only (no backend calls, no
// listeners). activateFeatures runs in App's first effect, after Flint's own
// commands, and registers commands (including each panel's "Show …") and runs
// activate hooks.
import { FEATURES } from '.'
import { checkFeatures, type FeatureModule } from '../lib/features'
import { contribute } from '../lib/contributions'
import { registerSections } from '../lib/settingsSections'
import { CORE_KEYS } from '../lib/settings'
import { registerCommands } from '../lib/commands'
import { panelCommands } from '../panels/registry'
import { useWorkspace } from '../store/workspace'

let installed: readonly FeatureModule[] | null = null

/** Registers the modules' data contributions; once per page. */
export function installFeatures(mods: readonly FeatureModule[] = FEATURES): void {
  if (installed) return
  checkFeatures(mods)
  for (const m of mods) {
    registerSections(m.settings ?? [], CORE_KEYS)
    contribute(`feature:${m.id}`, {
      panels: m.panels, settingsPages: m.settingsPages, statusItems: m.statusItems, tabKinds: m.tabKinds, fileMenu: m.fileMenu, themes: m.themes,
    })
  }
  installed = mods
}

/** Registers the modules' commands and runs their activate hooks; the result undoes both. */
export function activateFeatures(): () => void {
  const offs: (() => void)[] = []
  for (const m of installed ?? []) {
    const show = (id: string) => useWorkspace.getState().setSidebar(id)
    offs.push(registerCommands([...panelCommands(m.panels ?? [], show), ...(m.commands ?? [])]))
    const undo = m.activate?.()
    if (undo) offs.push(undo)
  }
  return () => offs.reverse().forEach((off) => off())
}
