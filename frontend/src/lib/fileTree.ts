// Pure helpers behind the file tree: flattening, selection and path bookkeeping
// for rename / delete. Kept free of React and Wails so node:test can cover them.

export interface TreeEntry {
  name: string
  path: string
  isDir: boolean
}

export interface Row {
  entry: TreeEntry
  depth: number
  expanded: boolean
  /** A workspace folder's header (only shown when several folders are open). */
  isRoot?: boolean
}

export const dirname = (p: string) => p.slice(0, Math.max(p.lastIndexOf('/'), 1))
export const basename = (p: string) => p.slice(p.lastIndexOf('/') + 1)

/** True when path is base itself or lies under it. */
export function isWithin(base: string, path: string): boolean {
  return path === base || path.startsWith(base.endsWith('/') ? base : `${base}/`)
}

/** Flattens the expanded part of the tree into rows for virtualized rendering. */
export function visibleRows(root: string, children: Record<string, TreeEntry[]>, expanded: Record<string, boolean>, depth = 0): Row[] {
  const rows: Row[] = []
  const walk = (dir: string, level: number) => {
    for (const entry of children[dir] ?? []) {
      const isOpen = entry.isDir && !!expanded[entry.path]
      rows.push({ entry, depth: level, expanded: isOpen })
      if (isOpen) walk(entry.path, level + 1)
    }
  }
  walk(root, depth)
  return rows
}

/**
 * Rows of the whole workspace: one folder is shown as before (its entries at the
 * top level); several get a collapsible header each, labelled like ⌘P results.
 */
export function workspaceRows(
  roots: string[],
  labels: string[],
  children: Record<string, TreeEntry[]>,
  expanded: Record<string, boolean>,
): Row[] {
  if (roots.length === 1) return visibleRows(roots[0], children, expanded)
  return roots.flatMap((root, i) => {
    const open = !!expanded[root]
    const header: Row = { entry: { name: labels[i], path: root, isDir: true }, depth: 0, expanded: open, isRoot: true }
    return open ? [header, ...visibleRows(root, children, expanded, 1)] : [header]
  })
}

/** Paths of the rows between anchor and target (inclusive), in tree order (⇧-click). */
export function rangeBetween(rows: Row[], anchor: string, target: string): string[] {
  const a = rows.findIndex((r) => r.entry.path === anchor)
  const b = rows.findIndex((r) => r.entry.path === target)
  if (a < 0 || b < 0) return [target]
  const [from, to] = a <= b ? [a, b] : [b, a]
  return rows.slice(from, to + 1).map((r) => r.entry.path)
}

/** Adds or removes path from the selection (⌘-click). */
export function toggleIn(selection: string[], path: string): string[] {
  return selection.includes(path) ? selection.filter((p) => p !== path) : [...selection, path]
}

/** Drops paths nested under another selected path (a folder takes its contents along). */
export function topLevel(paths: string[]): string[] {
  const unique = [...new Set(paths)]
  return unique.filter((p) => !unique.some((other) => other !== p && isWithin(other, p)))
}

/** Maps path through a rename of `from` to `to`; unrelated paths are returned unchanged. */
export function remapPath(path: string, from: string, to: string): string {
  if (path === from) return to
  return isWithin(from, path) ? to + path.slice(from.length) : path
}

/** Re-keys a path-keyed record after a rename (expanded folders, listed children). */
export function remapKeys<T>(record: Record<string, T>, from: string, to: string): Record<string, T> {
  const out: Record<string, T> = {}
  for (const [key, value] of Object.entries(record)) out[remapPath(key, from, to)] = value
  return out
}

/** Path relative to root ("" for the root itself). */
export function relativePath(root: string, path: string): string {
  if (path === root) return ''
  return isWithin(root, path) ? path.slice(root.replace(/\/$/, '').length + 1) : path
}

/** Search include glob that scopes a search to folder ("" means the whole project). */
export function folderGlob(root: string, folder: string): string {
  const rel = relativePath(root, folder)
  return rel ? `${rel}/**` : ''
}

/** Selection range for renaming: the name without its extension, like Finder. */
export function stemRange(name: string, isDir: boolean): [number, number] {
  const dot = name.lastIndexOf('.')
  return isDir || dot <= 0 ? [0, name.length] : [0, dot]
}

/** Client-side check before asking the backend, so typos are reported inline. */
export function nameError(name: string, siblings: string[], current?: string): string | null {
  const n = name.trim()
  if (!n) return 'A name is required'
  if (n === '.' || n === '..') return `"${n}" is not a valid name`
  if (n.includes('/')) return 'Names cannot contain "/"'
  const lower = n.toLowerCase()
  // A case-only rename of the entry itself is fine.
  if (siblings.some((s) => s.toLowerCase() === lower && s !== current && current?.toLowerCase() !== lower)) {
    return `"${n}" already exists here`
  }
  return null
}

/** A row of the rendered tree: an entry, or the inline input of a new file / folder. */
export type DisplayRow = { kind: 'entry'; row: Row } | { kind: 'draft'; dir: string; depth: number; isDir: boolean }

/** Inserts the "new file / folder" input as the first child of dir (dir must be expanded). */
export function withDraft(rows: Row[], root: string, draft: { dir: string; isDir: boolean } | null): DisplayRow[] {
  const out: DisplayRow[] = rows.map((row) => ({ kind: 'entry', row }))
  if (!draft) return out
  const at = draft.dir === root ? -1 : rows.findIndex((r) => r.entry.path === draft.dir)
  if (at < 0 && draft.dir !== root) return out
  const depth = at < 0 ? 0 : rows[at].depth + 1
  return [...out.slice(0, at + 1), { kind: 'draft', dir: draft.dir, depth, isDir: draft.isDir }, ...out.slice(at + 1)]
}
