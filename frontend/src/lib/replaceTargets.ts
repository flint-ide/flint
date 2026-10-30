// What a project-wide replace writes: the ticked lines of every file, minus files
// with unsaved edits (their editor content would be out of step with the disk).
import type { SearchMatch } from '../api/backend'

export interface ReplaceTarget {
  root: string
  path: string
  lines: number[]
}

export const fileKey = (m: Pick<SearchMatch, 'root' | 'path'>) => `${m.root}/${m.path}`
export const matchKey = (m: Pick<SearchMatch, 'root' | 'path' | 'line'>) => `${m.root}/${m.path}:${m.line}`

/** Groups the ticked matches by file; `dirty` holds absolute paths with unsaved edits. */
export function replaceTargets(
  matches: readonly SearchMatch[],
  excluded: ReadonlySet<string>,
  dirty: ReadonlySet<string>,
): { targets: ReplaceTarget[]; skippedDirty: string[]; count: number } {
  const byFile = new Map<string, ReplaceTarget>()
  const skipped = new Set<string>()
  let count = 0
  for (const m of matches) {
    const file = fileKey(m)
    if (excluded.has(file) || excluded.has(matchKey(m)) || !m.replacements?.length) continue
    if (dirty.has(file)) {
      skipped.add(file)
      continue
    }
    const t = byFile.get(file) ?? { root: m.root, path: m.path, lines: [] }
    if (!t.lines.includes(m.line)) t.lines.push(m.line)
    byFile.set(file, t)
    count += m.replacements.length
  }
  return { targets: [...byFile.values()], skippedDirty: [...skipped], count }
}
