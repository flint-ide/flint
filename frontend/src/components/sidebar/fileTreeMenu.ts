import type { DropdownItem } from '../ui/Dropdown'
import { dirname, type TreeEntry } from '../../lib/fileTree'
import { rootOf } from '../../lib/workspaceRoots'
import { attachPathsToAgent } from '../../lib/agentContext'
import { copyPaths, findInFolder, removeFromWorkspace, revealInFinder, trashEntries, type NewKind } from '../../store/fileOps'
import { changesUnder, confirmRollback, useGit } from '../../store/git'
import { useEditor } from '../../store/editor'
import { useTerminal } from '../../store/terminal'
import { useWorkspace } from '../../store/workspace'
import { showFileHistory } from '../../store/gitHistory'
import { runTestsInFile } from '../../store/run'
import { profileTestsInFile } from '../../store/profile'
import { debugTestsInFile } from '../../store/debug'
import { testLang } from '../../lib/testFiles'

export interface MenuActions {
  /** Starts inline creation of a file or folder in dir. */
  startCreate: (dir: string, kind: NewKind) => void
  startRename: (path: string) => void
}

const addProject: DropdownItem = { label: 'Add Project to Workspace…', kbd: '⇧⌘O', onSelect: () => void useWorkspace.getState().pickRoot() }

/**
 * Items of the file tree's context menu for the right-clicked targets (the selection
 * when the click landed on it). A click on empty space passes the folder when one is
 * open, and no targets (workspace menu) when several are.
 */
export function fileTreeMenu(roots: string[], targets: TreeEntry[], actions: MenuActions): DropdownItem[] {
  if (targets.length === 0) return [addProject]
  const paths = targets.map((t) => t.path)
  const single = targets.length === 1 ? targets[0] : null
  const isRoot = !!single && roots.includes(single.path)
  const folder = single ? (single.isDir ? single.path : dirname(single.path)) : roots[0]
  const root = rootOf(roots, folder) ?? roots[0]
  const changes = changesUnder(paths)
  const changedFile = single && !single.isDir ? changes.find((f) => `${useGit.getState().root}/${f.path}` === single.path) : undefined

  const items: DropdownItem[] = [
    { label: 'New File…', onSelect: () => actions.startCreate(folder, 'file') },
    { label: 'New Folder…', onSelect: () => actions.startCreate(folder, 'folder') },
    { type: 'separator' },
  ]
  if (!isRoot) {
    items.push(
      { label: 'Rename…', kbd: 'F2', disabled: !single, onSelect: () => single && actions.startRename(single.path) },
      { label: 'Move to Trash', kbd: '⌘⌫', danger: true, onSelect: () => void trashEntries(paths.filter((p) => !roots.includes(p))) },
      { type: 'separator' },
    )
  }
  items.push(
    { label: targets.length > 1 ? `Copy ${targets.length} Paths` : 'Copy Path', onSelect: () => void copyPaths(paths, false) },
    { label: 'Copy Relative Path', onSelect: () => void copyPaths(paths, true) },
    { type: 'separator' },
  )
  if (single) {
    items.push(
      { label: 'Reveal in Finder', onSelect: () => void revealInFinder(single.path) },
      { label: 'Open in Terminal', hint: shortName(folder), onSelect: () => void useTerminal.getState().create(folder, { project: root }) },
      { label: 'Find in Folder…', hint: shortName(folder), onSelect: () => findInFolder(folder) },
      { type: 'separator' },
    )
  }
  items.push({ label: 'Add to Agent Chat', onSelect: () => void attachPathsToAgent(paths) })
  const lang = single && !single.isDir ? testLang(single.path) : null
  if (lang) {
    items.push({ type: 'separator' }, { label: 'Run Tests', hint: single!.name, onSelect: () => void runTestsInFile(single!.path) })
    if (lang === 'go') {
      items.push(
        { label: 'Debug Tests', hint: single!.name, onSelect: () => void debugTestsInFile(single!.path) },
        { label: 'Profile Tests', hint: single!.name, onSelect: () => void profileTestsInFile(single!.path) },
      )
    }
  }
  const history = single && !single.isDir && inRepo(root)
  if (history || changes.length > 0) items.push({ type: 'separator' })
  if (history) items.push({ label: 'Show History', onSelect: () => showFileHistory(single.path) })
  if (changes.length > 0) {
    const gitRoot = useGit.getState().root ?? root
    if (changedFile && changedFile.kind !== 'conflicted') {
      const base = changedFile.origPath ? `${gitRoot}/${changedFile.origPath}` : single!.path
      items.push({ label: 'Show Diff', onSelect: () => useEditor.getState().openDiff(gitRoot, single!.path, base) })
    }
    items.push({
      label: changes.length === 1 ? 'Rollback…' : `Rollback ${changes.length} Changed Files…`,
      danger: true,
      onSelect: () => void confirmRollback(changes),
    })
  }
  if (isRoot) {
    items.push({ type: 'separator' }, addProject)
    if (single.path !== roots[0]) {
      items.push({ label: 'Remove from Workspace', onSelect: () => void removeFromWorkspace(single.path) })
    }
  }
  return items
}

/** Whether a workspace folder is in a git repository (the Commit panel's switcher counts every one). */
function inRepo(root: string): boolean {
  const git = useGit.getState()
  return git.folder === root ? git.isRepo : root in git.counts
}

const shortName = (path: string) => path.slice(path.lastIndexOf('/') + 1)
