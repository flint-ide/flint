// Grouping and filtering of the TODO list, pure so it is testable.
import type { todo } from '../wails/models'

export type TodoItem = todo.Item

export interface TodoFile {
  key: string // absolute path
  root: string
  path: string
  items: TodoItem[]
}

/** Items matching the tag filter (empty = all) and the text query, grouped by file in scan order. */
export function groupTodos(items: readonly TodoItem[], tags: ReadonlySet<string>, query: string): TodoFile[] {
  const q = query.trim().toLowerCase()
  const files = new Map<string, TodoFile>()
  for (const it of items) {
    if (tags.size > 0 && !tags.has(it.tag)) continue
    if (q && !`${it.text} ${it.owner ?? ''} ${it.path}`.toLowerCase().includes(q)) continue
    const key = `${it.root}/${it.path}`
    const file = files.get(key) ?? { key, root: it.root, path: it.path, items: [] }
    file.items.push(it)
    files.set(key, file)
  }
  return [...files.values()]
}

/** How many items carry each tag. */
export function countTags(items: readonly TodoItem[]): Record<string, number> {
  const counts: Record<string, number> = {}
  for (const it of items) counts[it.tag] = (counts[it.tag] ?? 0) + 1
  return counts
}

/** Changes here never hold notes worth listing. */
const IGNORED = /\/(\.git|node_modules|\.venv|venv|__pycache__|vendor)\//

/** Whether a file change can alter the list: not in .git or a dependency folder the scan skipped. */
export function affectsTodos(path: string, skipped: readonly string[]): boolean {
  return !IGNORED.test(path) && !skipped.some((dir) => path.startsWith(`${dir}/`))
}
