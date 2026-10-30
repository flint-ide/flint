import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import type { EditorView } from '@codemirror/view'
import { Database, errorMessage, type QueryResult } from '../../api/backend'
import { statementAt } from '../../lib/sqlStatement'
import { formatDuration } from '../../lib/format'
import { databaseChoices, defaultDatabaseName } from '../../lib/databases'
import { useDatabase } from '../../store/database'
import { useScratch } from '../../store/scratch'
import { ResultsGrid } from '../results/ResultsGrid'
import { Splitter } from '../layout/Splitter'
import { Select } from '../ui/Select'
import { DatabaseIcon } from '../icons/DatabaseIcon'

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

/**
 * Runs the statement under the cursor (or the selection) against one database of a
 * connection ("" = the profile's default).
 */
export function useQueryRunner(connId: string, database: string): QueryRunner {
  const [state, setState] = useState<RunState>({ running: false, sql: '', result: null, error: null, startedAt: 0 })
  const [limit, setLimit] = useState(1000)
  const queryId = useRef('')
  // Refs keep runAtCursor stable so the editor keymap isn't rebuilt on every run.
  const latest = useRef({ connId, database, limit })
  latest.current = { connId, database, limit }

  const runAtCursor = useCallback((view: EditorView) => {
    const { connId: conn, database: db, limit: max } = latest.current
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
    Database.RunQuery(conn, db, id, sql, max)
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
  /** The scratchpad's remembered database ("" = the profile's default). */
  database: string
  runner: QueryRunner
  editor: ReactNode
  view: RefObject<EditorView | null>
}

export function QueryPanel({ scratchId, connId, database, runner, editor, view }: PanelProps) {
  const connections = useDatabase((s) => s.connections)
  const conn = connections.find((c) => c.id === connId)
  const databases = useDatabase((s) => s.databases[connId])
  const loadDatabases = useDatabase((s) => s.loadDatabases)
  const setConnection = useScratch((s) => s.setConnection)
  const [split, setSplit] = useState(0.55)
  const { state } = runner

  useEffect(() => {
    if (conn) void loadDatabases(conn.id)
  }, [conn, loadDatabases])

  const fallback = conn ? defaultDatabaseName(databases?.list, conn) : ''
  const current = database || fallback

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
        {conn && (
          <Select
            size="sm"
            searchable
            value={current}
            title={databases?.error ? `Database used by this console (list unavailable: ${databases.error})` : 'Database used by this console'}
            options={databaseChoices(databases?.list, current).map((d) => ({
              value: d.name,
              label: d.name,
              icon: <DatabaseIcon locked={!d.canConnect} />,
              hint: !d.canConnect ? 'no access' : d.name === fallback ? 'default' : undefined,
              disabled: !d.canConnect,
            }))}
            // Picking the default stores "" so the console keeps following the profile's default.
            onChange={(name) => void setConnection(scratchId, connId, name === fallback ? '' : name)}
          />
        )}
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
