import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import type { EditorView } from '@codemirror/view'
import { Database, errorMessage, type QueryResult } from '../../api/backend'
import { statementAt } from '../../lib/sqlStatement'
import { formatDuration } from '../../lib/format'
import { useDatabase } from '../../store/database'
import { useScratch } from '../../store/scratch'
import { ResultsGrid } from '../results/ResultsGrid'
import { Splitter } from '../layout/Splitter'
import { Select } from '../ui/Select'

const LIMITS = [100, 1000, 10_000, 100_000]

interface RunState {
  running: boolean
  sql: string
  result: QueryResult | null
  error: string | null
  startedAt: number
}

export interface QueryRunner {
  state: RunState
  limit: number
  setLimit: (n: number) => void
  runAtCursor: (view: EditorView) => void
  cancel: () => void
}

/** Runs the statement under the cursor (or the selection) against a connection. */
export function useQueryRunner(connId: string): QueryRunner {
  const [state, setState] = useState<RunState>({ running: false, sql: '', result: null, error: null, startedAt: 0 })
  const [limit, setLimit] = useState(1000)
  const queryId = useRef('')
  // Refs keep runAtCursor stable so the editor keymap isn't rebuilt on every run.
  const latest = useRef({ connId, limit })
  latest.current = { connId, limit }

  const runAtCursor = useCallback((view: EditorView) => {
    const { connId: conn, limit: max } = latest.current
    const sel = view.state.selection.main
    const text = view.state.doc.toString()
    const sql = sel.empty ? statementAt(text, sel.head)?.sql : text.slice(sel.from, sel.to).trim()
    if (!sql) return
    if (!conn) {
      setState((s) => ({ ...s, error: 'Pick a connection first.', result: null }))
      return
    }
    if (queryId.current) void Database.CancelQuery(queryId.current) // don't leave it holding a pooled connection
    const id = crypto.randomUUID()
    queryId.current = id
    setState({ running: true, sql, result: null, error: null, startedAt: Date.now() })
    Database.RunQuery(conn, id, sql, max)
      .then((result) => queryId.current === id && setState((s) => ({ ...s, running: false, result })))
      .catch((err) => queryId.current === id && setState((s) => ({ ...s, running: false, error: errorMessage(err) })))
      .finally(() => {
        if (queryId.current === id) queryId.current = ''
      })
  }, [])

  const cancel = useCallback(() => {
    if (queryId.current) void Database.CancelQuery(queryId.current)
  }, [])

  // Closing the console cancels whatever it was still running.
  useEffect(() => cancel, [cancel])

  return useMemo(() => ({ state, limit, setLimit, runAtCursor, cancel }), [state, limit, runAtCursor, cancel])
}

interface PanelProps {
  scratchId: string
  connId: string
  runner: QueryRunner
  editor: ReactNode
  view: RefObject<EditorView | null>
}

export function QueryPanel({ scratchId, connId, runner, editor, view }: PanelProps) {
  const connections = useDatabase((s) => s.connections)
  const setConnection = useScratch((s) => s.setConnection)
  const [split, setSplit] = useState(0.55)
  const { state } = runner

  return (
    <div className="query-panel">
      <div className="query-toolbar">
        <Select
          size="sm"
          value={connId}
          allowEmpty="— no connection —"
          placeholder="Connection"
          title="Connection used by this console"
          options={connections.map((c) => ({
            value: c.id,
            label: c.name,
            hint: c.driver === 'clickhouse' ? 'ClickHouse' : 'PostgreSQL',
            icon: <span className={`driver-badge ${c.driver}`}>{c.driver === 'clickhouse' ? 'CH' : 'PG'}</span>,
          }))}
          onChange={(id) => void setConnection(scratchId, id)}
        />
        {state.running ? (
          <button className="danger" onClick={runner.cancel}>■ Cancel</button>
        ) : (
          <button className="primary" onClick={() => view.current && runner.runAtCursor(view.current)} title="Run statement under cursor or selection (⌘↵)">
            ▶ Run
          </button>
        )}
        <Select
          size="sm"
          label="limit"
          value={runner.limit}
          options={LIMITS.map((l) => ({ value: l, label: l.toLocaleString() }))}
          onChange={runner.setLimit}
          title="Maximum rows fetched"
        />
        <span className="spacer" />
      </div>
      <div className="query-split">
        <div style={{ flex: `${split} 1 0` }} className="query-editor">{editor}</div>
        <Splitter direction="vertical" onResize={(ratio) => setSplit(Math.min(0.9, Math.max(0.1, ratio)))} />
        <div style={{ flex: `${1 - split} 1 0` }} className="query-results">
          <ResultArea state={state} />
        </div>
      </div>
    </div>
  )
}

function ResultArea({ state }: { state: RunState }) {
  if (state.running) return <div className="placeholder">Running… <code>{abbreviate(state.sql)}</code></div>
  if (state.error) return <div className="placeholder error selectable">{state.error}</div>
  const r = state.result
  if (!r) return <div className="placeholder muted">Results appear here.</div>
  const summary = [
    r.columns.length > 0 ? `${r.rows.length.toLocaleString()} rows${r.truncated ? ' (limit reached)' : ''}` : r.command,
    r.affected ? `${r.affected} affected` : '',
    formatDuration(r.elapsedMs),
  ].filter(Boolean)
  const header = (
    <>
      <span>{summary.join(' · ')}</span>
      <code className="muted truncate">{abbreviate(state.sql)}</code>
    </>
  )
  return (
    <div className="result-area">
      {r.columns.length > 0 ? <ResultsGrid result={r} summary={header} /> : <div className="result-summary">{header}</div>}
    </div>
  )
}

function abbreviate(sql: string): string {
  const flat = sql.replace(/\s+/g, ' ')
  return flat.length > 120 ? `${flat.slice(0, 117)}…` : flat
}
