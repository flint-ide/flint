// The workspace model: a primary project folder plus folders attached to it (like
// IntelliJ's "attach project"). Pure helpers so node:test can cover them.
import { isWithin } from './fileTree'

/** The workspace folder containing path, or null. Roots never nest. */
export function rootOf(roots: string[], path: string): string | null {
  return roots.find((r) => isWithin(r, path)) ?? null
}

/** Why folder can't be attached (already there, nested in or around a root), or null. */
export function addRootError(roots: string[], folder: string): string | null {
  const name = (p: string) => p.slice(p.lastIndexOf('/') + 1)
  for (const r of roots) {
    if (r === folder) return `${name(folder)} is already in the workspace`
    if (isWithin(r, folder)) return `${name(folder)} is inside ${name(r)}, which is already in the workspace`
    if (isWithin(folder, r)) return `${name(folder)} contains ${name(r)}, which is already in the workspace`
  }
  return null
}

/**
 * Display names: the folder name, plus the parent's name when two roots share a
 * name ("app (api)"), and a number if that still clashes. Mirrors workspace.Labels
 * in Go, which prefixes ⌘P results with the same names.
 */
export function rootLabels(roots: string[]): string[] {
  const base = roots.map((r) => r.slice(r.lastIndexOf('/') + 1))
  const count = new Map<string, number>()
  for (const b of base) count.set(b, (count.get(b) ?? 0) + 1)
  const used = new Set<string>()
  return roots.map((r, i) => {
    let label = base[i]
    if ((count.get(label) ?? 0) > 1) {
      const parent = r.slice(0, r.lastIndexOf('/'))
      label = `${label} (${parent.slice(parent.lastIndexOf('/') + 1)})`
    }
    for (let n = 2; used.has(label); n++) label = `${base[i]} ${n}`
    used.add(label)
    return label
  })
}

/** The label of root, or its folder name when it isn't in the workspace. */
export function labelOf(roots: string[], root: string): string {
  const i = roots.indexOf(root)
  return i >= 0 ? rootLabels(roots)[i] : root.slice(root.lastIndexOf('/') + 1)
}

/**
 * Where new terminals and chats start: the root of what the active tab shows (a
 * file, or a terminal's / chat's folder), else the primary folder.
 */
export function pickRoot(roots: string[], context: string | null | undefined): string | null {
  return (context && rootOf(roots, context)) || roots[0] || null
}

/** Drops path-keyed entries of a removed root (tree listings, expanded folders). */
export function withoutRoot<T>(record: Record<string, T>, root: string): Record<string, T> {
  return Object.fromEntries(Object.entries(record).filter(([path]) => !isWithin(root, path)))
}

/** Path shown in lists: relative to its root, prefixed with the root's label when there are several. */
export function displayPath(roots: string[], path: string): string {
  const root = rootOf(roots, path)
  if (!root) return path
  const rel = path === root ? '' : path.slice(root.length + 1)
  if (roots.length < 2) return rel || labelOf(roots, root)
  return rel ? `${labelOf(roots, root)}/${rel}` : labelOf(roots, root)
}
