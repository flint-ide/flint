import { createStore } from '../lib/store'
import { joinRoot, type BlameCommit } from '../lib/blame'
import { useEditor } from './editor'
import { showToast } from './dialog'

// Blame (Annotate) and file history. The actions are plain functions so they can be
// bound to buttons, menus and commands alike; without a path they act on the
// active file tab.

interface GitHistoryState {
  /** Files whose editor shows the blame column, by absolute path. */
  blame: Record<string, boolean>
}

export const useGitHistory = createStore<GitHistoryState>(() => ({ blame: {} }))

// Annotations belong to an editor: forget them when the file's last tab closes, so
// reopening it starts without blame (and closed files don't pile up here).
useEditor.subscribe((s, prev) => {
  if (s.tabs === prev.tabs) return
  const annotated = Object.keys(useGitHistory.getState().blame)
  if (annotated.length === 0) return
  const open = new Set(s.tabs.flatMap((t) => (t.kind === 'file' ? [t.path] : [])))
  if (annotated.every((p) => open.has(p))) return
  useGitHistory.setState({ blame: Object.fromEntries(annotated.filter((p) => open.has(p)).map((p) => [p, true])) })
})

/** The active file tab's path, or null when the active tab isn't a file. */
export function activeFilePath(): string | null {
  const { tabs, activeId } = useEditor.getState()
  const tab = tabs.find((t) => t.id === activeId)
  return tab?.kind === 'file' ? tab.path : null
}

/** Shows or hides the blame column for a file (default: the active one). */
export function toggleBlame(path = activeFilePath()): void {
  if (!path) return showToast('Open a file to annotate it')
  setBlameShown(path, !useGitHistory.getState().blame[path])
}

export function setBlameShown(path: string, shown: boolean): void {
  useGitHistory.setState((s) => {
    const rest = Object.fromEntries(Object.entries(s.blame).filter(([p]) => p !== path))
    return { blame: shown ? { ...rest, [path]: true } : rest }
  })
}

/** Opens the history of a file (default: the active one) in a tab. */
export function showFileHistory(path = activeFilePath()): void {
  if (!path) return showToast('Open a file to see its history')
  useEditor.getState().openHistory(path)
}

/** A commit as far as one file is concerned: its name after and before the commit. */
export interface FileRevision {
  hash: string
  /** Repo-relative path in that commit. */
  path: string
  /** Repo-relative path before the commit (differs for renames). */
  basePath: string
  /** Revision the left side comes from; defaults to the first parent. */
  baseRev?: string
}

/** Opens what a commit changed in a file, in a diff tab. */
export function openCommitDiff(root: string, rev: FileRevision): void {
  const short = rev.hash.slice(0, 7)
  useEditor.getState().openRevisionDiff({
    repoRoot: root,
    path: joinRoot(root, rev.path),
    basePath: joinRoot(root, rev.basePath),
    baseRev: rev.baseRev || `${rev.hash}^`,
    rev: rev.hash,
    baseLabel: `${short}^`,
    label: short,
  })
}

/** Compares a file at a commit with its working-tree version (currentPath, absolute). */
export function compareWithCurrent(root: string, hash: string, pathAtCommit: string, currentPath: string): void {
  useEditor.getState().openRevisionDiff({
    repoRoot: root,
    path: currentPath,
    basePath: joinRoot(root, pathAtCommit),
    baseRev: hash,
    baseLabel: hash.slice(0, 7),
    label: 'Working tree',
  })
}

/** What clicking a blame entry does: the commit's change, or the local diff when uncommitted. */
export function openBlameCommit(root: string, path: string, c: BlameCommit): void {
  if (c.uncommitted) {
    useEditor.getState().openDiff(root, path)
    return
  }
  openCommitDiff(root, {
    hash: c.hash,
    path: c.path,
    basePath: c.prevPath || c.path,
    baseRev: c.previous || undefined,
  })
}
