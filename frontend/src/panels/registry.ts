// Sidebar panel registry: what a panel needs to appear on the activity bar, in the
// sidebar and in the palette. The panels themselves are listed in ./index.ts.
// Framework-free on purpose (React only as a type), so it is testable with node:test.
import type { ComponentType } from 'react'
import { formatKeys, type Command } from '../lib/commands'

/** Props every panel component receives (panels without an input ignore them). */
export interface PanelProps {
  /** Bumped by focusPanel (⇧⌘F, ⌘K): the panel focuses its main input when it changes. */
  focusToken: number
}

/** The palette entry generated for a panel; every field defaults from the panel. */
export interface PanelCommand {
  /** Default `view.<panel id>`. Keep existing ids: the keymap stores overrides by id. */
  id?: string
  /** Default `Show <title>`. */
  title?: string
  /** Default 'View'. */
  category?: string
  keywords?: string
  keys?: string
}

export interface PanelSpec<Id extends string = string> {
  /** Stable id, stored as the open sidebar view. */
  id: Id
  /** Name shown in the tooltip and, by default, in the "Show …" command. */
  title: string
  /** Activity bar glyph. */
  icon: string
  /** Position on the activity bar, ascending; ties keep listing order. */
  order: number
  /** Wrap heavy panels in React.lazy so their code is fetched on first open, not at startup. */
  component: ComponentType<PanelProps>
  /** Command whose shortcut the tooltip shows, e.g. "Search (⇧⌘F)"; read on hover so rebinding shows up. */
  shortcutCommand?: string
  /** Palette entry overrides; false for none. */
  command?: PanelCommand | false
  /** Availability: false hides the activity bar button and the command. Read stores with getState(). */
  when?: () => boolean
}

/** The panel shown when the stored id is unknown (a removed or renamed panel). */
export const FALLBACK_PANEL = 'files'

/**
 * Validates and orders the panel list once, at startup. Duplicate ids would make
 * two buttons select the same view, so they fail loudly instead.
 */
export function definePanels<const T extends readonly PanelSpec[]>(specs: T): readonly PanelSpec<T[number]['id']>[] {
  const seen = new Set<string>()
  for (const spec of specs) {
    if (seen.has(spec.id)) throw new Error(`duplicate panel id: ${spec.id}`)
    seen.add(spec.id)
  }
  // Stable sort: equal orders keep the order they were listed in.
  return [...specs].sort((a, b) => a.order - b.order)
}

/** The panel for a stored id, else the fallback (else the first panel). */
export function resolvePanel<S extends PanelSpec>(panels: readonly S[], id: string): S | undefined {
  return panels.find((p) => p.id === id) ?? panels.find((p) => p.id === FALLBACK_PANEL) ?? panels[0]
}

export function isPanelAvailable(spec: Pick<PanelSpec, 'when'>): boolean {
  try {
    return !spec.when || spec.when()
  } catch {
    return false
  }
}

/** Tooltip text: the title plus the shortcut of its command, when it has one. */
export function panelTooltip(spec: Pick<PanelSpec, 'title'>, keys: string | undefined): string {
  if (!keys) return spec.title
  try {
    return `${spec.title} (${formatKeys(keys)})`
  } catch {
    return spec.title // an unparsable override must not break the activity bar
  }
}

/** The palette commands of the panels; `show(id)` opens a panel. */
export function panelCommands<Id extends string>(panels: readonly PanelSpec<Id>[], show: (id: Id) => void): Command[] {
  return panels.flatMap((spec): Command[] => {
    if (spec.command === false) return []
    const c = spec.command ?? {}
    return [
      {
        id: c.id ?? `view.${spec.id}`,
        title: c.title ?? `Show ${spec.title}`,
        category: c.category ?? 'View',
        keywords: c.keywords,
        keys: c.keys,
        when: spec.when,
        run: () => show(spec.id),
      },
    ]
  })
}
