import { useEffect, useMemo, useState } from 'react'
import { Workspace, errorMessage } from '../api/backend'
import { Git } from '../store/git'
import { useWorkspace } from '../store/workspace'
import { showToast } from '../store/dialog'
import { Modal } from './layout/Modal'
import { FileIcon } from './icons/FileIcon'
import { formatKeys } from '../lib/commands'
import { useListCursor } from '../lib/useListCursor'

const tildify = (p: string) => p.replace(/^\/Users\/[^/]+/, '~')

/** Row of the "Add Project to Workspace…" action (paths are absolute, so it can't clash). */
const ADD = '\0add'

/**
 * ⌘O: IntelliJ-style Recent Projects. Enter opens here, ⌘Enter in a new window,
 * ⌥Enter attaches the project to this workspace; "Browse…" falls back to the
 * native folder picker.
 */
export function RecentProjects() {
  const root = useWorkspace((s) => s.root)
  const roots = useWorkspace((s) => s.roots)
  const openFolder = useWorkspace((s) => s.openFolder)
  const close = () => useWorkspace.getState().setRecentOpen(false)
  const [recent, setRecent] = useState<string[]>([])
  const [query, setQuery] = useState('')
  const { listRef, cursor, setCursor, hover } = useListCursor()

  useEffect(() => {
    void Workspace.RecentFolders().then((r) => setRecent(r ?? []))
  }, [])

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase()
    const matches = recent.filter((p) => !q || p.toLowerCase().includes(q))
    // null = "Browse…"; ADD = "Add Project to Workspace…" (only with a folder open).
    return [null, ...(root ? [ADD] : []), ...matches]
  }, [recent, query, root])

  useEffect(() => setCursor(query ? Math.max(0, rows.findIndex((r) => r !== null && r !== ADD)) : 0), [query, rows, setCursor])

  const browse = async () => {
    close()
    const path = await Workspace.PickFolder()
    if (path) await openFolder(path)
  }

  const addProject = async (path?: string) => {
    close()
    const target = path ?? (await Workspace.PickFolder())
    if (target) await useWorkspace.getState().addRoot(target)
  }

  const choose = async (path: string | null, mods: { metaKey: boolean; altKey: boolean }) => {
    if (path === null) return void browse()
    if (path === ADD) return void addProject()
    if (mods.altKey && root && !roots.includes(path)) return void addProject(path)
    close()
    if (mods.metaKey) {
      await Git.OpenWindow(path).catch((err) => showToast(errorMessage(err), 'error'))
      return
    }
    if (path !== root) await openFolder(path)
  }

  const forget = async (path: string) => {
    await Workspace.ForgetRecent(path).catch(() => undefined)
    setRecent((r) => r.filter((p) => p !== path))
  }

  return (
    <Modal onClose={close} className="quick-open recent-projects">
      <input
        autoFocus
        className="quick-input"
        placeholder={`Open recent project…  (${formatKeys('Mod+Enter')} new window${root ? ` · ${formatKeys('Alt+Enter')} add to workspace` : ''})`}
        value={query}
        spellCheck={false}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') setCursor((c) => Math.min(c + 1, rows.length - 1))
          else if (e.key === 'ArrowUp') setCursor((c) => Math.max(c - 1, 0))
          else if (e.key === 'Enter') void choose(rows[cursor], e)
          else return
          e.preventDefault()
        }}
      />
      <div className="quick-list" ref={listRef}>
        {rows.map((path, i) =>
          path === ADD ? (
            <div key="add" className={`quick-item${i === cursor ? ' selected' : ''}`} onMouseMove={() => hover(i)} onClick={() => void addProject()}>
              <FileIcon name="folder" isDir />
              <span className="file-name">Add Project to Workspace…</span>
              <span className="muted path-hint">open it next to {root?.split('/').pop()}</span>
              <kbd>{formatKeys('Mod+Shift+O')}</kbd>
            </div>
          ) : path === null ? (
            <div key="browse" className={`quick-item${i === cursor ? ' selected' : ''}`} onMouseMove={() => hover(i)} onClick={() => void browse()}>
              <FileIcon name="folder" isDir open />
              <span className="file-name">Browse…</span>
              <span className="muted path-hint">choose a folder</span>
              <kbd>{formatKeys('Mod+O')}</kbd>
            </div>
          ) : (
            <div
              key={path}
              className={`quick-item${i === cursor ? ' selected' : ''}${roots.includes(path) ? ' current' : ''}`}
              onMouseMove={() => hover(i)}
              onClick={(e) => void choose(path, e)}
              title={path}
            >
              <FileIcon name={path.split('/').pop() ?? path} isDir />
              <span className="file-name">{path.split('/').pop()}</span>
              <span className="muted path-hint">{tildify(path)}</span>
              {path === root && <span className="badge">open</span>}
              {path !== root && roots.includes(path) && <span className="badge">in workspace</span>}
              {root && !roots.includes(path) && (
                <button
                  className="icon recent-forget"
                  title={`Add to workspace (${formatKeys('Alt+Enter')})`}
                  onClick={(e) => {
                    e.stopPropagation()
                    void addProject(path)
                  }}
                >
                  ＋
                </button>
              )}
              <button
                className="icon recent-forget"
                title="Remove from list"
                onClick={(e) => {
                  e.stopPropagation()
                  void forget(path)
                }}
              >
                ✕
              </button>
            </div>
          ),
        )}
        {rows.length === 1 && <div className="placeholder muted">{recent.length ? 'No matching projects' : 'No recent projects yet'}</div>}
      </div>
    </Modal>
  )
}
