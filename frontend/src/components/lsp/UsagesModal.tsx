import { memo, useEffect } from 'react'
import type { lsp } from '../../wails/models'
import { useLsp } from '../../store/lsp'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { displayPath } from '../../lib/workspaceRoots'
import { Modal } from '../layout/Modal'
import { useListCursor } from '../../lib/useListCursor'
import { useEventCallback } from '../../lib/useEventCallback'

/** IntelliJ-style "Find Usages" popup: ↑/↓ to move, Enter to jump. */
export function UsagesModal() {
  const usages = useLsp((s) => s.usages)
  const close = useLsp((s) => s.closeUsages)
  const roots = useWorkspace((s) => s.roots)
  const openFile = useEditor((s) => s.openFile)
  const { listRef, cursor, setCursor, hover } = useListCursor()

  useEffect(() => setCursor(0), [usages, setCursor])

  const choose = useEventCallback((i: number) => {
    const loc = usages?.locations[i]
    if (!loc) return
    const { start, end } = loc.range
    openFile(loc.path, { line: start.line + 1, from: start.character, to: end.line === start.line ? end.character : start.character })
    close()
  })

  if (!usages) return null
  const { locations, symbol } = usages

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
      <div className="quick-list" ref={listRef}>
        {locations.map((loc, i) => (
          <UsageRow
            key={`${loc.path}:${loc.range.start.line}:${loc.range.start.character}`}
            location={loc}
            path={displayPath(roots, loc.path)}
            index={i}
            selected={i === cursor}
            onHover={hover}
            onPick={choose}
          />
        ))}
      </div>
    </Modal>
  )
}

/** One usage; memoized so moving the highlight re-renders only two rows. */
const UsageRow = memo(function UsageRow({ location, path, index, selected, onHover, onPick }: {
  location: lsp.Location
  path: string
  index: number
  selected: boolean
  onHover: (i: number) => void
  onPick: (i: number) => void
}) {
  return (
    <div className={`quick-item${selected ? ' selected' : ''}`} onMouseMove={() => onHover(index)} onClick={() => onPick(index)}>
      <span className="file-name">{path.split('/').pop()}:{location.range.start.line + 1}</span>
      <code className="muted usage-preview">{location.preview}</code>
      <span className="muted path-hint">{path}</span>
    </div>
  )
})
