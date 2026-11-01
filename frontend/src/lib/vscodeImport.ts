// The pure parts of "Import from VS Code…": grouping the preview Go returns
// (internal/vscodeimport) and naming the Flint commands and shortcut clashes a
// keybinding brings. Framework-free, so it is testable with node:test.
import { formatKeys, keyConflicts, type Command } from './commands'

export interface ImportItem {
  kind: string
  /** VS Code setting, command or theme. */
  name: string
  /** What Flint gets. */
  value: string
  /** Flint setting or command id. */
  target: string
}

export interface ImportSkipped {
  kind: string
  name: string
  reason: string
}

export interface ImportGroup<T> {
  kind: string
  title: string
  entries: T[]
}

const GROUPS: { kind: string; title: string }[] = [
  { kind: 'setting', title: 'Settings' },
  { kind: 'theme', title: 'Theme' },
  { kind: 'keybinding', title: 'Keyboard shortcuts' },
  { kind: 'file', title: 'Files' },
]

/** Entries by kind, in the dialog's order; empty groups are left out. */
export function groupByKind<T extends { kind: string }>(entries: readonly T[]): ImportGroup<T>[] {
  const known = GROUPS.map((g) => g.kind)
  const groups = GROUPS.map((g) => ({ ...g, entries: entries.filter((e) => e.kind === g.kind) }))
  const other = entries.filter((e) => !known.includes(e.kind))
  if (other.length) groups.push({ kind: 'other', title: 'Other', entries: other })
  return groups.filter((g) => g.entries.length > 0)
}

/** The keymap after the import: the imported shortcuts over the user's own. */
export function importedKeymap(items: readonly ImportItem[], current: Readonly<Record<string, string>>): Record<string, string> {
  const next = { ...current }
  for (const it of items) if (it.kind === 'keybinding') next[it.target] = it.value
  return next
}

/** "Go to File" for a command id, the id itself when Flint doesn't know it. */
export function commandTitle(id: string, list: readonly Command[]): string {
  return list.find((c) => c.id === id)?.title ?? id
}

/** The shortcut as shown in menus plus the commands that would share it, if any. */
export function keybindingNote(item: ImportItem, list: readonly Command[], keymap: Readonly<Record<string, string>>): string {
  let keys = item.value
  try {
    keys = formatKeys(item.value)
  } catch {
    // keep Go's spelling
  }
  const clashes = keyConflicts(item.value, item.target, list, keymap).map((c) => c.title)
  return clashes.length ? `${keys} — also ${clashes.join(', ')}` : keys
}
