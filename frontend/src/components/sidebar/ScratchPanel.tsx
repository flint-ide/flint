import { useState } from 'react'
import { useScratch } from '../../store/scratch'
import { useEditor } from '../../store/editor'
import { useDatabase } from '../../store/database'
import { confirmAction } from '../../store/dialog'
import type { ScratchKind, ScratchPad } from '../../api/backend'

const KIND_LABEL: Record<ScratchKind, string> = { sql: 'SQL', markdown: 'MD', text: 'TXT' }

/** Persistent scratchpads: SQL consoles and notes kept outside any project. */
export function ScratchPanel() {
  const items = useScratch((s) => s.items)
  const create = useScratch((s) => s.create)

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">Scratchpads</span>
        <button className="icon" title="New SQL console" onClick={() => void create('sql')}>SQL</button>
        <button className="icon" title="New markdown note" onClick={() => void create('markdown')}>MD</button>
        <button className="icon" title="New text note" onClick={() => void create('text')}>TXT</button>
      </div>
      <div className="tree scroll">
        {items.length === 0 && <div className="placeholder muted">No scratchpads yet. ⌘N creates an SQL console.</div>}
        {items.map((sc) => (
          <ScratchRow key={sc.id} sc={sc} />
        ))}
      </div>
    </div>
  )
}

function ScratchRow({ sc }: { sc: ScratchPad }) {
  const [renaming, setRenaming] = useState(false)
  const openScratch = useEditor((s) => s.openScratch)
  const active = useEditor((s) => s.activeId === `scratch:${sc.id}`)
  const rename = useScratch((s) => s.rename)
  const remove = useScratch((s) => s.remove)
  const connName = useDatabase((s) => s.connections.find((c) => c.id === sc.connId)?.name)

  const commit = (value: string) => {
    setRenaming(false)
    if (value.trim() && value !== sc.name) void rename(sc.id, value)
  }

  return (
    <div
      className={`tree-row static${active ? ' active' : ''}`}
      onClick={() => openScratch(sc)}
      onDoubleClick={() => setRenaming(true)}
      title="Double-click to rename"
    >
      <span className={`kind-badge ${sc.kind}`}>{KIND_LABEL[sc.kind as ScratchKind] ?? sc.kind}</span>
      {renaming ? (
        <input
          className="inline-input"
          defaultValue={sc.name}
          autoFocus
          onClick={(e) => e.stopPropagation()}
          onBlur={(e) => commit(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') commit(e.currentTarget.value)
            if (e.key === 'Escape') setRenaming(false)
          }}
        />
      ) : (
        <span className="scratch-name">{sc.name}</span>
      )}
      {connName && <span className="muted path-hint">{connName}</span>}
      <span className="row-actions" onClick={(e) => e.stopPropagation()}>
        <button
          className="icon"
          title="Delete"
          onClick={async () => (await confirmAction(`Delete scratchpad "${sc.name}"?`, 'Delete')) && remove(sc.id)}
        >
          ✕
        </button>
      </span>
    </div>
  )
}
