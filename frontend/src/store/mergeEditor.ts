// The merge editor's app-wide side: opening it, and the commands that act on the
// active merge tab. The editor itself (CodeMirror) loads lazily with MergeView, which
// registers a controller per tab while it is mounted.
import type { Command } from '../lib/commands'
import type { Resolution } from '../lib/mergeConflicts'
import { activeFilePath } from './gitHistory'
import { useEditor } from './editor'
import { useGit } from './git'

/** What the commands can ask of a mounted merge editor. */
export interface MergeController {
  /** Moves to the next (1) or previous (-1) conflict. */
  go: (dir: 1 | -1) => void
  /** Resolves the conflict at the cursor (or the next one). */
  resolve: (how: Resolution) => void
  askAgent: () => void
  save: () => Promise<void>
  markResolved: () => Promise<void>
}

const controllers = new Map<string, MergeController>()

export function registerMergeController(tabId: string, c: MergeController): () => void {
  controllers.set(tabId, c)
  return () => {
    if (controllers.get(tabId) === c) controllers.delete(tabId)
  }
}

function active(): MergeController | undefined {
  const id = useEditor.getState().activeId
  return id ? controllers.get(id) : undefined
}

/** Opens the merge editor for a conflicted file (absolute path) of the work tree at repoRoot. */
export function openMergeEditor(repoRoot: string, path: string): void {
  useEditor.getState().openMerge(repoRoot, path)
}

/** The conflicted file the active file tab shows, as the Commit panel lists it. */
function activeConflictedFile(): { root: string; path: string } | null {
  const path = activeFilePath()
  const { root, status } = useGit.getState()
  if (!path || !root || !path.startsWith(`${root}/`)) return null
  const rel = path.slice(root.length + 1)
  return status?.files.some((f) => f.kind === 'conflicted' && f.path === rel) ? { root, path } : null
}

export function mergeCommands(): Command[] {
  const has = () => !!active()
  const resolve = (how: Resolution) => () => active()?.resolve(how)
  return [
    {
      id: 'git.resolveConflicts', title: 'Resolve Conflicts in Merge Editor', category: 'Git', keywords: 'merge conflict rebase',
      when: () => !!activeConflictedFile(),
      run: () => {
        const f = activeConflictedFile()
        if (f) openMergeEditor(f.root, f.path)
      },
    },
    { id: 'merge.nextConflict', title: 'Next Conflict', category: 'Merge', keys: 'Mod+Alt+N', when: has, run: () => active()?.go(1) },
    { id: 'merge.previousConflict', title: 'Previous Conflict', category: 'Merge', keys: 'Mod+Alt+P', when: has, run: () => active()?.go(-1) },
    { id: 'merge.acceptCurrent', title: 'Accept Current Change', category: 'Merge', keys: 'Mod+Alt+1', keywords: 'ours', when: has, run: resolve('current') },
    { id: 'merge.acceptIncoming', title: 'Accept Incoming Change', category: 'Merge', keys: 'Mod+Alt+2', keywords: 'theirs', when: has, run: resolve('incoming') },
    { id: 'merge.acceptBoth', title: 'Accept Both Changes (Current First)', category: 'Merge', keys: 'Mod+Alt+3', when: has, run: resolve('both') },
    {
      id: 'merge.acceptBothIncomingFirst', title: 'Accept Both Changes (Incoming First)', category: 'Merge', keys: 'Mod+Alt+4',
      when: has, run: resolve('both-incoming-first'),
    },
    { id: 'merge.askAgent', title: 'Ask Agent about Conflict', category: 'Merge', keywords: 'claude codex resolve', when: has, run: () => active()?.askAgent() },
    { id: 'merge.save', title: 'Save Merge Result', category: 'Merge', hintKeys: 'Mod+S', when: has, run: () => active()?.save() },
    { id: 'merge.markResolved', title: 'Mark as Resolved', category: 'Merge', keys: 'Mod+Shift+Enter', keywords: 'stage add', when: has, run: () => active()?.markResolved() },
  ]
}
