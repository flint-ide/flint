import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react'
import { useVirtualList } from '../../lib/virtualList'
import { focusSoon } from '../../lib/focusSoon'
import {
  basename, dirname, nameError, rangeBetween, stemRange, toggleIn, withDraft, workspaceRows, type Row, type TreeEntry,
} from '../../lib/fileTree'
import { rootLabels } from '../../lib/workspaceRoots'
import { useWorkspace } from '../../store/workspace'
import { useEditor } from '../../store/editor'
import { createEntry, renameEntry, trashEntries, type NewKind } from '../../store/fileOps'
import { FileIcon } from '../icons/FileIcon'
import { openContextMenu } from '../../store/contextMenu'
import { fileTreeMenu } from './fileTreeMenu'
import { formatKeys } from '../../lib/commands'
import { modKey } from '../../lib/platform'

const ROW_HEIGHT = 22

type Edit = { kind: 'rename'; path: string } | { kind: 'new'; dir: string; type: NewKind }

export function FileTree() {
  const roots = useWorkspace((s) => s.roots)
  const children = useWorkspace((s) => s.children)
  const expanded = useWorkspace((s) => s.expanded)
  const toggleDir = useWorkspace((s) => s.toggleDir)
  const reloadTree = useWorkspace((s) => s.reloadTree)
  const openFile = useEditor((s) => s.openFile)
  const activeId = useEditor((s) => s.activeId)

  const [selection, setSelection] = useState<string[]>([])
  // ⇧-selection spans anchor → lead; arrows move the lead.
  const [anchor, setAnchor] = useState<string | null>(null)
  const [lead, setLead] = useState<string | null>(null)
  const [edit, setEdit] = useState<Edit | null>(null)
  const treeEl = useRef<HTMLDivElement | null>(null)

  const labels = useMemo(() => rootLabels(roots), [roots])
  const rows = useMemo(() => (roots.length ? workspaceRows(roots, labels, children, expanded) : []), [roots, labels, children, expanded])
  // With one folder its entries sit at the top level; with several each has a header row.
  const implicitRoot = roots.length === 1 ? roots[0] : ''
  const display = useMemo(
    () => withDraft(rows, implicitRoot, edit?.kind === 'new' ? { dir: edit.dir, isDir: edit.type === 'folder' } : null),
    [rows, implicitRoot, edit],
  )
  const { scrollRef, totalSize, items } = useVirtualList({ count: display.length, itemSize: ROW_HEIGHT, overscan: 30 })
  const setTreeRef = useCallback(
    (el: HTMLDivElement | null) => {
      treeEl.current = el
      scrollRef(el)
    },
    [scrollRef],
  )

  // Bring the inline input into view (a new file in a long folder may be off screen).
  const draftIndex = display.findIndex((d) => d.kind === 'draft')
  useEffect(() => {
    if (draftIndex >= 0) scrollToIndex(treeEl.current, draftIndex)
  }, [draftIndex])

  if (roots.length === 0) return <div className="placeholder muted">No folder open.</div>

  const focusTree = () => treeEl.current?.focus({ preventScroll: true })
  const select = (paths: string[], at = paths[paths.length - 1] ?? null) => {
    setSelection(paths)
    setAnchor(at)
    setLead(at)
  }

  const startCreate = async (dir: string, type: NewKind) => {
    if (dir !== implicitRoot && !useWorkspace.getState().expanded[dir]) await toggleDir(dir)
    setEdit({ kind: 'new', dir, type })
  }
  const startRename = (path: string) => {
    if (!roots.includes(path)) setEdit({ kind: 'rename', path })
  }
  const endEdit = () => {
    setEdit(null)
    focusTree()
  }

  const trash = async (paths: string[]) => {
    if (await trashEntries(paths.filter((p) => !roots.includes(p)))) select([])
    focusTree()
  }

  const onRowClick = (e: MouseEvent, entry: TreeEntry) => {
    if (modKey(e)) {
      setSelection((sel) => toggleIn(sel, entry.path))
      setAnchor(entry.path)
      setLead(entry.path)
      return
    }
    if (e.shiftKey && anchor) {
      setSelection(rangeBetween(rows, anchor, entry.path))
      setLead(entry.path)
      return
    }
    select([entry.path])
    if (entry.isDir) void toggleDir(entry.path)
    else openFile(entry.path)
  }

  // A right-click on the selection acts on all of it; elsewhere it selects that row.
  const onContextMenu = (e: MouseEvent, entry: TreeEntry | null) => {
    e.preventDefault()
    e.stopPropagation()
    let targets: TreeEntry[]
    if (!entry) {
      // Empty space: the folder itself, or the workspace when there are several.
      targets = implicitRoot ? [{ name: basename(implicitRoot), path: implicitRoot, isDir: true }] : []
    } else if (selection.includes(entry.path)) {
      const byPath = new Map(rows.map((r) => [r.entry.path, r.entry]))
      targets = selection.flatMap((p) => byPath.get(p) ?? [])
    } else {
      select([entry.path])
      targets = [entry]
    }
    const items = fileTreeMenu(roots, targets, { startCreate: (dir, type) => void startCreate(dir, type), startRename })
    openContextMenu(e.clientX, e.clientY, items, { minWidth: 220, onClose: focusTree })
  }

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.target !== e.currentTarget || edit) return
    const activePath = activeId?.startsWith('file:') ? activeId.slice(5) : null
    const from = lead ?? selection[selection.length - 1] ?? activePath
    const handled = treeKey(e, rows, from, {
      move: (path, extend) => {
        if (extend && anchor) {
          setSelection(rangeBetween(rows, anchor, path))
          setLead(path)
        } else select([path])
        scrollToIndex(treeEl.current, rows.findIndex((r) => r.entry.path === path))
      },
      toggle: (path) => void toggleDir(path),
      open: (row) => (row.entry.isDir ? void toggleDir(row.entry.path) : openFile(row.entry.path)),
      rename: () => selection.length === 1 && startRename(selection[0]),
      trash: () => selection.length > 0 && void trash(selection),
      clear: () => select([]),
    })
    if (handled) {
      e.preventDefault()
      e.stopPropagation()
    }
  }

  const selected = new Set(selection)
  const siblings = (dir: string) => (children[dir] ?? []).map((c) => c.name)

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title" title={roots.join('\n')}>{implicitRoot ? basename(implicitRoot) : 'Workspace'}</span>
        <button className="icon" title={`Add Project to Workspace… (${formatKeys('Mod+Shift+O')})`} onClick={() => void useWorkspace.getState().pickRoot()}>＋</button>
        <button className="icon" title="Refresh" onClick={() => void reloadTree()}>↻</button>
      </div>
      <div className="tree" ref={setTreeRef} tabIndex={0} onKeyDown={onKeyDown} onContextMenu={(e) => onContextMenu(e, null)}>
        <div style={{ height: totalSize, position: 'relative' }}>
          {items.map((vi) => {
            const d = display[vi.index]
            const depth = d.kind === 'draft' ? d.depth : d.row.depth
            const style = { transform: `translateY(${vi.start}px)`, paddingLeft: 8 + depth * 14 }
            if (d.kind === 'draft') {
              const type: NewKind = d.isDir ? 'folder' : 'file'
              return (
                <div key="__draft__" className="tree-row editing" style={style}>
                  <span className="twisty" />
                  <FileIcon name="" isDir={d.isDir} />
                  <InlineName
                    initial=""
                    isDir={d.isDir}
                    siblings={siblings(d.dir)}
                    placeholder={d.isDir ? 'Folder name' : 'File name'}
                    onCancel={endEdit}
                    onCommit={async (name) => {
                      const path = await createEntry(d.dir, name, type)
                      endEdit()
                      if (path) select([path])
                    }}
                  />
                </div>
              )
            }
            const { entry, expanded: isOpen, isRoot } = d.row
            const renaming = edit?.kind === 'rename' && edit.path === entry.path
            const active = activeId === `file:${entry.path}`
            return (
              <div
                key={entry.path}
                className={`tree-row${isRoot ? ' root-row' : ''}${active ? ' active' : ''}${selected.has(entry.path) ? ' selected' : ''}${renaming ? ' editing' : ''}`}
                style={style}
                onClick={(e) => !renaming && onRowClick(e, entry)}
                onContextMenu={(e) => onContextMenu(e, entry)}
                title={entry.path}
              >
                <span className="twisty">{entry.isDir ? (isOpen ? '▾' : '▸') : ''}</span>
                <FileIcon name={entry.name} isDir={entry.isDir} open={isOpen} />
                {renaming ? (
                  <InlineName
                    initial={entry.name}
                    current={entry.name}
                    isDir={entry.isDir}
                    siblings={siblings(dirname(entry.path))}
                    onCancel={endEdit}
                    onCommit={async (name) => {
                      const to = await renameEntry(entry.path, name)
                      endEdit()
                      if (to) select([to])
                    }}
                  />
                ) : (
                  <span className={entry.isDir ? 'dir-name' : 'file-name'}>{entry.name}</span>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

interface KeyActions {
  move: (path: string, extend: boolean) => void
  toggle: (path: string) => void
  open: (row: Row) => void
  rename: () => void
  trash: () => void
  clear: () => void
}

/** Finder-style keys: arrows move / expand, Enter or F2 renames, ⌘⌫ (Ctrl+Backspace) or Delete trashes. */
function treeKey(e: KeyboardEvent, rows: Row[], lead: string | null, act: KeyActions): boolean {
  const at = rows.findIndex((r) => r.entry.path === lead)
  const row = rows[at]
  switch (e.key) {
    case 'ArrowDown':
      if (modKey(e)) return row ? (act.open(row), true) : false
      return rows.length > 0 && (act.move(rows[Math.min(at + 1, rows.length - 1)].entry.path, e.shiftKey), true)
    case 'ArrowUp':
      return rows.length > 0 && (act.move(rows[Math.max(at - 1, 0)].entry.path, e.shiftKey), true)
    case 'ArrowRight':
      if (!row?.entry.isDir) return false
      if (!row.expanded) act.toggle(row.entry.path)
      else if (rows[at + 1]?.depth > row.depth) act.move(rows[at + 1].entry.path, false)
      return true
    case 'ArrowLeft': {
      if (!row) return false
      if (row.entry.isDir && row.expanded) return act.toggle(row.entry.path), true
      const parent = dirname(row.entry.path)
      if (rows.some((r) => r.entry.path === parent)) act.move(parent, false)
      return true
    }
    case 'Enter':
    case 'F2':
      return act.rename(), true
    case 'Delete':
      return act.trash(), true
    case 'Backspace':
      return modKey(e) && (act.trash(), true)
    case 'Escape':
      return act.clear(), true
  }
  return false
}

function scrollToIndex(el: HTMLElement | null, index: number): void {
  if (!el || index < 0) return
  const top = index * ROW_HEIGHT
  if (top < el.scrollTop) el.scrollTop = top
  else if (top + ROW_HEIGHT > el.scrollTop + el.clientHeight) el.scrollTop = top + ROW_HEIGHT - el.clientHeight
}

interface InlineNameProps {
  initial: string
  /** The entry's current name when renaming (a case-only change is allowed). */
  current?: string
  isDir: boolean
  siblings: string[]
  placeholder?: string
  onCommit: (name: string) => void
  onCancel: () => void
}

/** Inline name editor: Enter commits, Escape cancels, clicking away commits a valid name. */
function InlineName({ initial, current, isDir, siblings, placeholder, onCommit, onCancel }: InlineNameProps) {
  const [value, setValue] = useState(initial)
  const input = useRef<HTMLInputElement>(null)
  const done = useRef(false)
  const error = value.trim() && value !== initial ? nameError(value, siblings, current) : null

  useEffect(
    () =>
      focusSoon(() => {
        input.current?.focus()
        input.current?.setSelectionRange(...stemRange(initial, isDir))
      }),
    [initial, isDir],
  )

  const finish = (commit: boolean) => {
    if (done.current) return
    done.current = true
    if (commit && value.trim() && value !== initial && !error) onCommit(value)
    else onCancel()
  }

  return (
    <span className="tree-edit">
      <input
        ref={input}
        className="inline-input"
        value={value}
        placeholder={placeholder}
        spellCheck={false}
        aria-invalid={!!error}
        onChange={(e) => setValue(e.target.value)}
        onClick={(e) => e.stopPropagation()}
        onContextMenu={(e) => e.stopPropagation()}
        onBlur={() => finish(true)}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.key === 'Enter' && !error) finish(true)
          if (e.key === 'Escape') finish(false)
        }}
      />
      {error && <span className="tree-edit-error">{error}</span>}
    </span>
  )
}
