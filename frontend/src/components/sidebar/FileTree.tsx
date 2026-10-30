import { useMemo, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useWorkspace } from '../../store/workspace'
import { useEditor } from '../../store/editor'
import type { Entry } from '../../api/backend'
import { FileIcon } from '../icons/FileIcon'

const ROW_HEIGHT = 22

interface Row {
  entry: Entry
  depth: number
  expanded: boolean
}

/** Flattens the expanded part of the tree into rows for virtualized rendering. */
function visibleRows(root: string, children: Record<string, Entry[]>, expanded: Record<string, boolean>): Row[] {
  const rows: Row[] = []
  const walk = (dir: string, depth: number) => {
    for (const entry of children[dir] ?? []) {
      const isOpen = entry.isDir && !!expanded[entry.path]
      rows.push({ entry, depth, expanded: isOpen })
      if (isOpen) walk(entry.path, depth + 1)
    }
  }
  walk(root, 0)
  return rows
}

export function FileTree() {
  const root = useWorkspace((s) => s.root)
  const children = useWorkspace((s) => s.children)
  const expanded = useWorkspace((s) => s.expanded)
  const toggleDir = useWorkspace((s) => s.toggleDir)
  const reloadTree = useWorkspace((s) => s.reloadTree)
  const openFile = useEditor((s) => s.openFile)
  const activeId = useEditor((s) => s.activeId)
  const scroller = useRef<HTMLDivElement>(null)

  const rows = useMemo(() => (root ? visibleRows(root, children, expanded) : []), [root, children, expanded])
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 30,
  })

  if (!root) return <div className="placeholder muted">No folder open.</div>

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title" title={root}>{root.split('/').pop()}</span>
        <button className="icon" title="Refresh" onClick={() => void reloadTree()}>↻</button>
      </div>
      <div className="tree" ref={scroller}>
        <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
          {virtualizer.getVirtualItems().map((vi) => {
            const { entry, depth, expanded: isOpen } = rows[vi.index]
            const active = activeId === `file:${entry.path}`
            return (
              <div
                key={entry.path}
                className={`tree-row${active ? ' active' : ''}`}
                style={{ transform: `translateY(${vi.start}px)`, paddingLeft: 8 + depth * 14 }}
                onClick={() => (entry.isDir ? void toggleDir(entry.path) : openFile(entry.path))}
                title={entry.path}
              >
                <span className="twisty">{entry.isDir ? (isOpen ? '▾' : '▸') : ''}</span>
                <FileIcon name={entry.name} isDir={entry.isDir} open={isOpen} />
                <span className={entry.isDir ? 'dir-name' : 'file-name'}>{entry.name}</span>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
