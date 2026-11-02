import { memo, useEffect, useState } from 'react'
import { Database, errorMessage, historyFilter, type HistoryEntry } from '../../api/backend'
import { formatDuration } from '../../lib/format'
import { historyKindLabel, relativeTime, sqlPreview } from '../../lib/queryHistory'
import { confirmAction, showToast } from '../../store/dialog'
import { Modal } from '../layout/Modal'
import { formatKeys } from '../../lib/commands'
import { useListCursor } from '../../lib/useListCursor'
import { useEventCallback } from '../../lib/useEventCallback'

const FILTER_DEBOUNCE_MS = 80
const LIST_LIMIT = 300

interface Props {
  connId: string
  connName: string
  driver: string
  /** The console's database ("" = the profile's default). */
  database: string
  onInsert: (entry: HistoryEntry) => void
  onRun: (entry: HistoryEntry) => void
  onClose: () => void
}

/**
 * The connection's executed statements, newest first, fuzzy-filtered in Go.
 * ↵ inserts into the editor, ⌘↵ runs, ⌘C copies.
 */
export function QueryHistory({ connId, connName, driver, database, onInsert, onRun, onClose }: Props) {
  const [query, setQuery] = useState('')
  const [allDatabases, setAllDatabases] = useState(false)
  const [entries, setEntries] = useState<HistoryEntry[] | null>(null)
  const [error, setError] = useState('')
  const { listRef, cursor, setCursor, hover } = useListCursor()
  const [reload, setReload] = useState(0)
  const now = Date.now()

  useEffect(() => {
    let cancelled = false
    const timer = setTimeout(() => {
      Database.QueryHistory(connId, historyFilter({ database, allDatabases, filter: query, limit: LIST_LIMIT }))
        .then((list) => {
          if (cancelled) return
          setEntries(list ?? [])
          setError('')
          setCursor(0)
        })
        .catch((err) => !cancelled && setError(errorMessage(err)))
    }, query ? FILTER_DEBOUNCE_MS : 0)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [connId, database, allDatabases, query, reload, setCursor])

  const rows = entries ?? []
  const current = rows[cursor]
  const insert = useEventCallback(onInsert)
  const run = useEventCallback(onRun)

  const clear = async () => {
    if (!(await confirmAction(`Clear the query history of ${connName}?`, 'Clear History'))) return
    try {
      await Database.ClearQueryHistory(connId)
      setReload((n) => n + 1)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    const mod = e.metaKey || e.ctrlKey
    if (e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n')) {
      setCursor((c) => Math.min(c + 1, rows.length - 1))
    } else if (e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p')) {
      setCursor((c) => Math.max(c - 1, 0))
    } else if (e.key === 'Enter' && current) {
      if (mod && canRun(current)) onRun(current)
      else if (!mod) onInsert(current)
    } else if (mod && e.key === 'c' && current && e.currentTarget.selectionStart === e.currentTarget.selectionEnd) {
      void copy(current)
    } else {
      return
    }
    e.preventDefault()
  }

  return (
    <Modal onClose={onClose} className="quick-open query-history">
      <div className="history-search">
        <input
          autoFocus
          className="quick-input"
          placeholder={`Filter history of ${connName}…`}
          value={query}
          spellCheck={false}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
        />
        <label className="history-scope" title="Show statements run on every database of this connection">
          <input type="checkbox" checked={allDatabases} onChange={(e) => setAllDatabases(e.target.checked)} />
          All databases
        </label>
      </div>
      <div className="quick-list history-list" ref={listRef}>
        {error && <div className="placeholder error">{error}</div>}
        {rows.map((entry, i) => (
          <HistoryRow
            key={entry.id}
            entry={entry}
            index={i}
            selected={i === cursor}
            badge={historyKindLabel(entry.kind, driver)}
            when={relativeTime(entry.at, now)}
            showDatabase={allDatabases}
            onHover={hover}
            onInsert={insert}
            onRun={run}
          />
        ))}
        {entries && rows.length === 0 && !error && (
          <div className="placeholder muted">{query ? 'No matching statements' : 'No statements run yet'}</div>
        )}
      </div>
      <div className="history-footer muted">
        <span>↵ insert · {formatKeys('Mod+Enter')} run · {formatKeys('Mod+C')} copy</span>
        <span className="spacer" />
        <button className="link" onClick={() => void clear()}>Clear History…</button>
      </div>
    </Modal>
  )
}

const canRun = (e: HistoryEntry | undefined) => !!e && e.kind !== 'edit'

async function copy(e: HistoryEntry): Promise<void> {
  await navigator.clipboard.writeText(e.sql)
  showToast('Copied SQL')
}

/** One statement; memoized so moving the highlight re-renders only two rows. */
const HistoryRow = memo(function HistoryRow({ entry, index, selected, badge, when, showDatabase, onHover, onInsert, onRun }: {
  entry: HistoryEntry
  index: number
  selected: boolean
  badge: string
  /** Relative time, computed by the list so equal text keeps the row memoized. */
  when: string
  showDatabase: boolean
  onHover: (i: number) => void
  onInsert: (entry: HistoryEntry) => void
  onRun: (entry: HistoryEntry) => void
}) {
  return (
    <div
      className={`history-item${selected ? ' selected' : ''}${entry.error ? ' failed' : ''}`}
      onMouseMove={() => onHover(index)}
      onDoubleClick={() => onInsert(entry)}
      title={entry.sql.length > 2000 ? `${entry.sql.slice(0, 2000)}…` : entry.sql}
    >
      <div className="history-sql">
        {badge && <span className="history-badge">{badge}</span>}
        <code>{sqlPreview(entry.sql)}</code>
      </div>
      <div className="history-meta">
        <span>{when}</span>
        <span>{formatDuration(entry.durationMs)}</span>
        {!entry.error && entry.kind !== 'explain' && entry.kind !== 'explain-analyze' && <span>{entry.rows.toLocaleString()} rows</span>}
        {showDatabase && <span>{entry.database || 'default'}</span>}
        {entry.truncated && <span title="Stored truncated (very long statement)">truncated</span>}
        {entry.error && <span className="history-error">{entry.error}</span>}
        <span className="spacer" />
        <span className="row-actions">
          <button className="link" onClick={() => onInsert(entry)} title="Insert into the editor (↵)">Insert</button>
          {canRun(entry) && <button className="link" onClick={() => onRun(entry)} title={`Run again (${formatKeys('Mod+Enter')})`}>Run</button>}
          <button className="link" onClick={() => void copy(entry)} title={`Copy (${formatKeys('Mod+C')})`}>Copy</button>
        </span>
      </div>
    </div>
  )
})
