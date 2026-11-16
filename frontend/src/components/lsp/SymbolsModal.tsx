import { memo, useEffect, useMemo, useState } from 'react'
import * as LSP from '../../../bindings/flint/lspapi'
import type { lsp } from '../../wails/models'
import { useLsp } from '../../store/lsp'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { displayPath } from '../../lib/workspaceRoots'
import { filterSymbols, symbolKindName } from '../../lib/lspText'
import { Modal } from '../layout/Modal'
import { useListCursor } from '../../lib/useListCursor'
import { useEventCallback } from '../../lib/useEventCallback'

const QUERY_DELAY_MS = 150

interface Row {
  key: string
  name: string
  kind: number
  depth: number
  hint: string
  path: string
  range: lsp.Range
}

/**
 * Symbol picker: the outline of the current file (IntelliJ "File Structure") or a
 * search over the whole workspace ("Go to Symbol"). The language servers do the
 * searching; this only lists what they answer.
 */
export function SymbolsModal() {
  const symbols = useLsp((s) => s.symbols)
  const close = useLsp((s) => s.closeSymbols)
  const roots = useWorkspace((s) => s.roots)
  const openFile = useEditor((s) => s.openFile)
  const [query, setQuery] = useState('')
  const [found, setFound] = useState<lsp.WorkspaceSymbol[]>([])
  const [note, setNote] = useState('')
  const { listRef, cursor, setCursor, hover } = useListCursor()
  const workspace = !!symbols && 'workspace' in symbols

  useEffect(() => {
    setQuery('')
    setFound([])
    setNote('')
  }, [symbols])

  // Workspace search: asked after a typing pause; a late answer to an older query is dropped.
  useEffect(() => {
    if (!workspace) return
    let current = true
    const timer = setTimeout(() => {
      LSP.WorkspaceSymbols(query)
        .then((list) => {
          if (!current) return
          setFound(list ?? [])
          setNote(list?.length ? '' : query ? 'No symbols match' : 'Type to search. Symbols come from the language servers of the files you have open.')
        })
        .catch((err) => current && (setFound([]), setNote(String(err instanceof Error ? err.message : err))))
    }, QUERY_DELAY_MS)
    return () => {
      current = false
      clearTimeout(timer)
    }
  }, [workspace, query])

  const rows = useMemo<Row[]>(() => {
    if (!symbols) return []
    if ('workspace' in symbols) {
      return found.map((s, i) => ({
        key: `${s.path}:${s.range.start.line}:${s.range.start.character}:${i}`,
        name: s.name,
        kind: s.kind,
        depth: 0,
        hint: [s.container, displayPath(roots, s.path)].filter(Boolean).join(' · '),
        path: s.path,
        range: s.range,
      }))
    }
    const filtering = query.trim() !== ''
    return filterSymbols(symbols.outline, query).map((s, i) => ({
      key: `${s.selection.start.line}:${s.selection.start.character}:${i}`,
      name: s.name,
      kind: s.kind,
      depth: filtering ? 0 : s.depth, // a filtered list has lost its parents: show it flat
      hint: [s.detail, filtering ? s.container : ''].filter(Boolean).join(' · '),
      path: symbols.path,
      range: s.selection,
    }))
  }, [symbols, found, query, roots])

  useEffect(() => setCursor(0), [rows, setCursor])

  const choose = useEventCallback((row: Row | undefined) => {
    if (!row) return
    const { start, end } = row.range
    openFile(row.path, { line: start.line + 1, from: start.character, to: end.line === start.line ? end.character : start.character })
    close()
  })

  if (!symbols) return null

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n')) setCursor((c) => Math.min(c + 1, rows.length - 1))
    else if (e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p')) setCursor((c) => Math.max(c - 1, 0))
    else if (e.key === 'Enter') choose(rows[cursor])
    else return
    e.preventDefault()
  }

  return (
    <Modal onClose={close} className="quick-open symbols">
      <input
        autoFocus
        className="quick-input"
        placeholder={workspace ? 'Go to symbol in workspace…' : 'Go to symbol in this file…'}
        value={query}
        spellCheck={false}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={onKeyDown}
      />
      <div className="quick-list" ref={listRef}>
        {rows.map((row, i) => (
          <SymbolRow key={row.key} row={row} index={i} selected={i === cursor} onHover={hover} onPick={choose} />
        ))}
        {rows.length === 0 && <div className="quick-empty muted">{workspace ? note : 'No symbols match'}</div>}
      </div>
    </Modal>
  )
}

/** One symbol; memoized so moving the highlight re-renders only two rows. */
const SymbolRow = memo(function SymbolRow({ row, index, selected, onHover, onPick }: {
  row: Row
  index: number
  selected: boolean
  onHover: (i: number) => void
  onPick: (row: Row) => void
}) {
  return (
    <div
      className={`quick-item${selected ? ' selected' : ''}`}
      style={row.depth ? { paddingLeft: 12 + Math.min(row.depth, 8) * 14 } : undefined}
      onMouseMove={() => onHover(index)}
      onClick={() => onPick(row)}
    >
      <span className="symbol-kind muted">{symbolKindName(row.kind)}</span>
      <span className="file-name">{row.name}</span>
      <span className="muted path-hint">{row.hint}</span>
    </div>
  )
})
