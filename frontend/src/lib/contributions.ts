// What feature modules and external extensions add to the shell's UI:
// panels, Settings pages, status items, tab kinds, file menu entries, color
// themes and editor decoration sets, by the source that contributed them. The shell renders Flint's own entries
// plus these; a source can be withdrawn as a whole (an extension stopped or
// uninstalled). Commands and settings sections have registries of their own
// (lib/commands.ts, lib/settingsSections.ts).
import { createStore } from './store'
import type { DecorationSetContribution, FileMenuItem, SettingsPage, StatusItem, TabKind, ThemeContribution } from './features'
import type { PanelSpec } from '../panels/registry'

export interface Contributions {
  panels: readonly PanelSpec[]
  settingsPages: readonly SettingsPage[]
  statusItems: readonly StatusItem[]
  tabKinds: readonly TabKind[]
  fileMenu: readonly FileMenuItem[]
  themes: readonly ThemeContribution[]
  decorationSets: readonly DecorationSetContribution[]
}

const EMPTY: Contributions = Object.freeze({ panels: [], settingsPages: [], statusItems: [], tabKinds: [], fileMenu: [], themes: [], decorationSets: [] })

/** The contributions of every source, flattened in contribution order; ordered lists are sorted by order. */
export const useContributions = createStore<Contributions>(() => EMPTY)

let bySource = new Map<string, Partial<Contributions>>()

/**
 * Adds a source's contributions ("feature:docker", later "extension:<id>"); the
 * returned function withdraws them. A source contributes once; ids must be
 * unique per kind across sources.
 */
export function contribute(source: string, parts: Partial<Contributions>): () => void {
  if (bySource.has(source)) throw new Error(`${source} already contributed`)
  const next = new Map(bySource).set(source, parts)
  const flat = flatten(next) // throws on a duplicate id: nothing is registered then
  bySource = next
  useContributions.setState(flat, true)
  return () => {
    if (bySource.get(source) !== parts) return
    const rest = new Map(bySource)
    rest.delete(source)
    bySource = rest
    useContributions.setState(flatten(rest), true)
  }
}

function flatten(sources: ReadonlyMap<string, Partial<Contributions>>): Contributions {
  const all = [...sources.values()]
  const of = <K extends keyof Contributions>(k: K) => all.flatMap((p) => (p[k] ?? []) as Contributions[K][number][])
  const out: Contributions = {
    panels: of('panels'),
    settingsPages: byOrder(of('settingsPages')),
    statusItems: byOrder(of('statusItems')),
    tabKinds: of('tabKinds'),
    fileMenu: of('fileMenu'),
    themes: of('themes'),
    decorationSets: of('decorationSets'),
  }
  for (const [kind, list] of Object.entries(out) as [string, readonly { id: string }[]][]) {
    const seen = new Set<string>()
    for (const { id } of list) {
      if (seen.has(id)) throw new Error(`duplicate ${kind} id: ${id}`)
      seen.add(id)
    }
  }
  return out
}

/** Stable sort by order: equal orders keep contribution order. */
export function byOrder<T extends { order: number }>(list: readonly T[]): T[] {
  return [...list].sort((a, b) => a.order - b.order)
}

/** The file menu entries offered for paths, as label + action; a failing action goes to onError. */
export function fileMenuFor(items: readonly FileMenuItem[], paths: readonly string[], onError: (err: unknown) => void): { label: string; onSelect: () => void }[] {
  const offered = items.filter((m) => {
    try {
      return !m.when || m.when(paths)
    } catch {
      return false
    }
  })
  return offered.map((m) => ({
    label: m.label,
    onSelect: () => {
      try {
        void Promise.resolve(m.run(paths)).catch(onError)
      } catch (err) {
        onError(err)
      }
    },
  }))
}

export function tabKind(id: string): TabKind | undefined {
  return useContributions.getState().tabKinds.find((k) => k.id === id)
}
