import { EventsOn } from '../../wailsjs/runtime/runtime'
import { Workspace, type Entry } from '../api/backend'
import { useWorkspace } from './workspace'

type FileListener = () => void

/** Open editors subscribe here to learn when their file changed on disk. */
const fileListeners = new Map<string, Set<FileListener>>()

export function onFileChanged(path: string, listener: FileListener): () => void {
  const set = fileListeners.get(path) ?? new Set()
  set.add(listener)
  fileListeners.set(path, set)
  return () => {
    set.delete(listener)
    if (set.size === 0) fileListeners.delete(path)
  }
}

const dirname = (p: string) => p.slice(0, Math.max(p.lastIndexOf('/'), 1))

/** Applies a batch of changed paths: refresh loaded tree dirs, open files and quick-open. */
export async function applyChanges(paths: string[]): Promise<void> {
  for (const p of paths) fileListeners.get(p)?.forEach((fn) => fn())

  const { children, root } = useWorkspace.getState()
  if (!root) return
  // A change to `a/b/c.go` may add/remove an entry in `a/b`; a changed dir path may be new.
  const dirs = new Set<string>()
  for (const p of paths) {
    if (children[dirname(p)]) dirs.add(dirname(p))
    if (children[p]) dirs.add(p)
  }
  // Mark the quick-open list stale (any path may be new); it refreshes lazily on next ⌘P.
  // A no-op while already stale, so a burst of edits in unloaded dirs renders nothing.
  if (dirs.size === 0) return useWorkspace.setState({ filesLoadedAt: 0 })
  const listed = await Promise.all(
    [...dirs].map((d) =>
      Workspace.ListDir(d).then(
        (entries) => [d, entries] as const,
        () => [d, null] as const, // directory was deleted
      ),
    ),
  )
  // Content edits don't change a listing: only write the tree when one did.
  const current = useWorkspace.getState().children
  let next: typeof current | null = null
  for (const [dir, entries] of listed) {
    if (entries ? sameEntries(current[dir], entries) : !(dir in current)) continue
    next ??= { ...current }
    if (entries) next[dir] = entries
    else delete next[dir]
  }
  useWorkspace.setState(next ? { children: next, filesLoadedAt: 0 } : { filesLoadedAt: 0 })
}

function sameEntries(a: Entry[] | undefined, b: Entry[]): boolean {
  return !!a && a.length === b.length && a.every((e, i) => e.name === b[i].name && e.path === b[i].path && e.isDir === b[i].isDir)
}

let subscribed = false
export function subscribeFsEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('fs:changed', (paths: string[]) => void applyChanges(paths))
}
