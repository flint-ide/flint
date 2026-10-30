import { createStore } from '../lib/store'
import { joinRoot } from '../lib/blame'
import type { git } from '../../wailsjs/go/models'
import { errorMessage } from '../api/backend'
import { Git, useGit, type FileChange } from './git'
import { confirmAction, showToast } from './dialog'
import { useEditor } from './editor'
import { useWorkspace } from './workspace'

// Stashes of the repository open in the Commit panel. Stashes are addressed by hash:
// the stash list is shared by all worktrees, so stash@{n} may shift under us.

export type Stash = git.Stash

interface StashState {
  stashes: Stash[]
  /** Files of stashes expanded in the panel, by stash hash. */
  files: Record<string, FileChange[]>
  busy: boolean
  /** Whether the "Stash Changes" dialog is open. */
  dialogOpen: boolean
}

export const useStash = createStore<StashState>(() => ({ stashes: [], files: {}, busy: false, dialogOpen: false }))

const repoRoot = () => useGit.getState().root

/** Reloads the stash list (cheap: one `git stash list`). */
export async function loadStashes(): Promise<void> {
  const root = repoRoot()
  if (!root) return useStash.setState({ stashes: [], files: {} })
  try {
    const stashes = (await Git.Stashes(root)) ?? []
    if (repoRoot() !== root) return
    // Keep file lists only for stashes that still exist.
    useStash.setState((s) => ({
      stashes,
      files: Object.fromEntries(Object.entries(s.files).filter(([hash]) => stashes.some((x) => x.hash === hash))),
    }))
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Opens the "Stash Changes" dialog (message, include untracked) in the Commit panel. */
export function openStashDialog(): void {
  useWorkspace.getState().setSidebar('git')
  useStash.setState({ dialogOpen: true })
}

export function closeStashDialog(): void {
  useStash.setState({ dialogOpen: false })
}

/** Runs a stash command, then refreshes the stash list and the Commit panel. */
async function run(op: (root: string) => Promise<void>, done: string): Promise<boolean> {
  const root = repoRoot()
  if (!root || useStash.getState().busy) return false
  useStash.setState({ busy: true })
  try {
    await op(root)
    showToast(done)
    return true
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return false
  } finally {
    useStash.setState({ busy: false })
    void loadStashes()
    void useGit.getState().refresh()
  }
}

/** Stashes local changes (git stash push), optionally with untracked files. */
export async function stashChanges(message: string, includeUntracked: boolean): Promise<boolean> {
  const ok = await run((root) => Git.StashPush(root, message.trim(), includeUntracked), 'Changes stashed')
  if (ok) closeStashDialog()
  return ok
}

/** Applies a stash and keeps it. */
export function applyStash(hash: string): Promise<boolean> {
  return run((root) => Git.StashApply(root, hash), 'Stash applied')
}

/** Applies a stash and drops it (git keeps it when there are conflicts). */
export function popStash(hash: string): Promise<boolean> {
  return run((root) => Git.StashPop(root, hash), 'Stash popped')
}

/** Deletes a stash after confirmation. */
export async function dropStash(hash: string): Promise<boolean> {
  const stash = useStash.getState().stashes.find((s) => s.hash === hash)
  const what = stash ? `“${stash.message}”` : hash.slice(0, 7)
  if (!(await confirmAction(`Drop stash ${what}? Its changes will be lost.`, 'Drop'))) return false
  return run((root) => Git.StashDrop(root, hash), 'Stash dropped')
}

/** Loads (or hides, when shown) the files a stash changed. */
export async function toggleStashFiles(hash: string): Promise<void> {
  const root = repoRoot()
  if (!root) return
  if (useStash.getState().files[hash]) {
    useStash.setState((s) => ({ files: Object.fromEntries(Object.entries(s.files).filter(([h]) => h !== hash)) }))
    return
  }
  try {
    const files = (await Git.StashFiles(root, hash)) ?? []
    useStash.setState((s) => ({ files: { ...s.files, [hash]: files } }))
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Shows what a stash changed in one file: the stash's base commit vs the stashed version. */
export function openStashFileDiff(hash: string, f: FileChange): void {
  const root = repoRoot()
  if (!root) return
  const short = hash.slice(0, 7)
  useEditor.getState().openRevisionDiff({
    repoRoot: root,
    path: joinRoot(root, f.path),
    basePath: joinRoot(root, f.origPath || f.path),
    baseRev: `${hash}^1`,
    // Untracked files live in the stash's third parent.
    rev: f.kind === 'untracked' ? `${hash}^3` : hash,
    baseLabel: 'Base',
    label: `stash ${short}`,
  })
}
