// File operations started from the file tree. Go does the work (and checks the paths
// stay inside the project); this keeps tabs, the tree and the search scope in step.
import { ClipboardSetText } from '../wails/runtime'
import { createStore } from '../lib/store'
import { Workspace, errorMessage } from '../api/backend'
import { basename, folderGlob, isWithin, relativePath, remapKeys, topLevel } from '../lib/fileTree'
import { rootOf } from '../lib/workspaceRoots'
import { confirmAction, showToast } from './dialog'
import { saveTab, useEditor } from './editor'
import { setAllBreakpoints, useDebug } from './debug'
import { applyChanges } from './fsEvents'
import { useSearch } from './search'
import { useWorkspace } from './workspace'

export type NewKind = 'file' | 'folder'

/** Creates a file (opened right away) or folder in dir; returns its path, or null on failure. */
export async function createEntry(dir: string, name: string, kind: NewKind): Promise<string | null> {
  try {
    const path = kind === 'file' ? await Workspace.CreateFile(dir, name.trim()) : await Workspace.CreateFolder(dir, name.trim())
    await applyChanges([path])
    if (kind === 'file') useEditor.getState().openFile(path)
    return path
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return null
  }
}

/**
 * Renames a file or folder. Unsaved edits in affected tabs are saved first (like
 * IntelliJ), then their tabs, breakpoints and the tree's expanded folders follow the new path.
 */
export async function renameEntry(path: string, newName: string): Promise<string | null> {
  try {
    for (const t of useEditor.getState().tabs) {
      if (t.kind === 'file' && t.dirty && isWithin(path, t.path)) await saveTab(t.id)
    }
    const to = await Workspace.RenamePath(path, newName.trim())
    if (to === path) return to
    useEditor.getState().renamePath(path, to)
    setAllBreakpoints(remapKeys(useDebug.getState().breakpoints, path, to))
    useWorkspace.setState((s) => ({ expanded: remapKeys(s.expanded, path, to) }))
    await useWorkspace.getState().reloadTree()
    return to
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return null
  }
}

/** Asks, then moves paths to the macOS Trash and closes their tabs. Returns whether it ran. */
export async function trashEntries(paths: string[]): Promise<boolean> {
  const top = topLevel(paths)
  if (top.length === 0) return false
  const dirty = useEditor.getState().tabs.some((t) => t.kind === 'file' && t.dirty && top.some((p) => isWithin(p, t.path)))
  const what = top.length === 1 ? `"${basename(top[0])}"` : `${top.length} items`
  const extra = dirty ? ' Unsaved changes in open tabs will be lost.' : ''
  if (!(await confirmAction(`Move ${what} to the Trash?${extra}`, 'Move to Trash'))) return false
  try {
    const moved = (await Workspace.TrashPaths(top)) ?? []
    useEditor.getState().closePaths(moved)
    const breakpoints = Object.entries(useDebug.getState().breakpoints)
    const kept = breakpoints.filter(([file]) => !moved.some((p) => isWithin(p, file)))
    if (kept.length !== breakpoints.length) setAllBreakpoints(Object.fromEntries(kept))
    await applyChanges(moved)
    showToast(moved.length === 1 ? `Moved ${basename(moved[0])} to the Trash` : `Moved ${moved.length} items to the Trash`)
  } catch (err) {
    showToast(errorMessage(err), 'error')
    // Some items may have moved before the failure.
    await useWorkspace.getState().reloadTree()
  }
  return true
}

/** Copies paths, absolute or relative to their own workspace folder. */
export async function copyPaths(paths: string[], relative: boolean): Promise<void> {
  const { roots } = useWorkspace.getState()
  const rel = (p: string) => relativePath(rootOf(roots, p) ?? '', p) || '.'
  const text = paths.map((p) => (relative ? rel(p) : p)).join('\n')
  if (await ClipboardSetText(text)) showToast(paths.length === 1 ? `Copied ${text}` : `Copied ${paths.length} paths`)
  else showToast('Could not copy to the clipboard', 'error')
}

export async function revealInFinder(path: string): Promise<void> {
  try {
    await Workspace.RevealInFinder(path)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Opens the search panel scoped to folder: its workspace folder plus an include glob. */
export function findInFolder(folder: string): void {
  const { roots } = useWorkspace.getState()
  const root = rootOf(roots, folder) ?? folder
  useSearch.getState().setParams({ include: folderGlob(root, folder), scope: roots.length > 1 ? root : '' })
  useWorkspace.getState().setSidebar('search')
}

/** A request for the Files panel's inline "new file / folder" input (see requestNewEntry). */
export interface NewEntryRequest {
  dir: string
  kind: NewKind
  seq: number
}

export const useNewEntryRequest = createStore<{ request: NewEntryRequest | null }>(() => ({ request: null }))

let newEntrySeq = 0

/** Shows the Files panel with the inline name input for a new entry in dir (the tab bar's ＋ ▸ New File…). */
export function requestNewEntry(dir: string, kind: NewKind = 'file'): void {
  if (!dir) {
    showToast('Open a folder first')
    return
  }
  useWorkspace.getState().focusPanel('files')
  useNewEntryRequest.setState({ request: { dir, kind, seq: ++newEntrySeq } })
}

/** The Files panel took the request. */
export function clearNewEntryRequest(): void {
  if (useNewEntryRequest.getState().request) useNewEntryRequest.setState({ request: null })
}

/** Asks, then detaches an attached workspace folder (its tabs close; nothing is deleted). */
export async function removeFromWorkspace(root: string): Promise<void> {
  const msg = `Remove ${basename(root)} from the workspace? Its open files are closed; nothing is deleted from disk.`
  if (await confirmAction(msg, 'Remove')) await useWorkspace.getState().removeRoot(root)
}
