// Pure part of the signed extension index view (Go: internal/extensions/index,
// internal/features/extensions/index.go): how the index's entries relate to
// what is installed, and which installed extensions it has a newer version
// of. No React, no bindings: tested with node:test.
import { catalogRows, compareVersions, type CatalogRow, type InstalledLike } from './catalogModel'

export interface IndexEntryLike {
  readonly id: string
  readonly version: string
  /** Why it can't be installed on this machine ("" when it can). */
  readonly error: string
}

/** Each entry with its status: available, installed, update, newer (than the index) or broken. */
export function indexRows<E extends IndexEntryLike>(entries: readonly E[], installed: readonly InstalledLike[]): readonly CatalogRow<E>[] {
  return catalogRows(entries, installed)
}

/**
 * The installed extensions the index has a newer, installable version of:
 * id → that version. An extension the index doesn't list (installed from a
 * folder), one at the same or a newer version, and an entry that can't be
 * installed here are left out.
 */
export function availableUpdates(entries: readonly IndexEntryLike[], installed: readonly InstalledLike[]): ReadonlyMap<string, string> {
  const byId = new Map(entries.map((e) => [e.id, e]))
  const out = new Map<string, string>()
  for (const have of installed) {
    const entry = byId.get(have.id)
    if (entry && !entry.error && have.version && compareVersions(entry.version, have.version) > 0) out.set(have.id, entry.version)
  }
  return out
}

/** "Program", "Color themes", "Languages" or "Data only" for the index's kind. */
export function kindLabel(kind: string): string {
  switch (kind) {
    case 'program':
      return 'Program'
    case 'themes':
      return 'Color themes'
    case 'languages':
      return 'Languages'
  }
  return 'Data only'
}

/** A download size: "812 B", "14 KB", "3.2 MB". */
export function formatSize(bytes: number): string {
  if (!(bytes > 0)) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** What a fetch found, for the toast of "Check for Extension Updates". */
export function updatesSummary(updates: ReadonlyMap<string, string>, names: (id: string) => string): string {
  if (updates.size === 0) return 'Every installed extension is up to date with the extension index.'
  const list = [...updates].map(([id, version]) => `${names(id)} ${version}`)
  return `${updates.size === 1 ? 'An update is' : 'Updates are'} available in Settings ▸ Extensions: ${list.join(', ')}.`
}
