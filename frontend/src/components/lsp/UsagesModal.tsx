import { useEffect, useRef, useState } from 'react'
import { useLsp } from '../../store/lsp'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { displayPath } from '../../lib/workspaceRoots'
import { Modal } from '../layout/Modal'

/** IntelliJ-style "Find Usages" popup: ↑/↓ to move, Enter to jump. */
export function UsagesModal() {
  const usages = useLsp((s) => s.usages)
  const close = useLsp((s) => s.closeUsages)
  const roots = useWorkspace((s) => s.roots)
  const openFile = useEditor((s) => s.openFile)
  const [cursor, setCursor] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => setCursor(0), [usages])
  useEffect(() => {
    list.current?.querySelector('.selected')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  if (!usages) return null
  const { locations, symbol } = usages
  const rel = (p: string) => displayPath(roots, p)

  const choose = (i: number) => {
    const loc = locations[i]
    if (!loc) return
    const { start, end } = loc.range
    openFile(loc.path, { line: start.line + 1, from: start.character, to: end.line === start.line ? end.character : start.character })
    close()
  }

  return (
    <Modal onClose={close} className="quick-open usages">
      <div
        className="usages-title"
        tabIndex={0}
        autoFocus
        ref={(el) => el?.focus()}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') setCursor((c) => Math.min(c + 1, locations.length - 1))
          else if (e.key === 'ArrowUp') setCursor((c) => Math.max(c - 1, 0))
          else if (e.key === 'Enter') choose(cursor)
          else return
          e.preventDefault()
        }}
      >
        {locations.length} {locations.length === 1 ? 'usage' : 'usages'} of <code>{symbol || 'symbol'}</code>
      </div>
      <div className="quick-list" ref={list}>
        {locations.map((loc, i) => (
          <div
            key={`${loc.path}:${loc.range.start.line}:${loc.range.start.character}`}
            className={`quick-item${i === cursor ? ' selected' : ''}`}
            onMouseMove={() => setCursor(i)}
            onClick={() => choose(i)}
          >
            <span className="file-name">{rel(loc.path).split('/').pop()}:{loc.range.start.line + 1}</span>
            <code className="muted usage-preview">{loc.preview}</code>
            <span className="muted path-hint">{rel(loc.path)}</span>
          </div>
        ))}
      </div>
    </Modal>
  )
}
