import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import type { EditorView } from '@codemirror/view'
import { Database, errorMessage, type Connection, type ExplainResult, type HistoryEntry, type QueryResult } from '../../api/backend'
import { statementAt } from '../../lib/sqlStatement'
import { formatDuration } from '../../lib/format'
import { databaseChoices, defaultDatabaseName } from '../../lib/databases'
import { useDatabase } from '../../store/database'
import { useScratch } from '../../store/scratch'
import { confirmAction } from '../../store/dialog'
import { registerSqlConsole } from '../../store/sqlConsole'
import { EditableResults } from '../results/EditableResults'
import { ExplainView } from '../results/ExplainView'
import { Splitter } from '../layout/Splitter'
import { Select } from '../ui/Select'
import { DatabaseIcon } from '../icons/DatabaseIcon'
import { QueryHistory } from './QueryHistory'

const LIMITS = [100, 1000, 10_000, 100_000]

interface RunState {
  running: boolean
  /** What the last run was: a query (result) or an EXPLAIN (explain). */
  kind: 'query' | 'explain'
  analyze: boolean
  sql: string
  result: QueryResult | null
  explain: ExplainResult | null
  error: string | null
  /** A neutral message instead of a result (e.g. EXPLAIN ANALYZE not confirmed). */
  notice: string | null
  startedAt: number
  /** Connection and database the run went to: results stay tied to them (table OIDs are per database). */
  connId: string
  database: string
}

const idle: RunState = {
  running: false, kind: 'query', analyze: false, sql: '', result: null, explain: null, error: null, notice: null, startedAt: 0,
  connId: '', database: '',
}

export interface QueryRunner {
  state: RunState
  limit: number
  setLimit: (n: number) => void
  runAtCursor: (view: EditorView) => void
  run: (sql: string) => void
  /** EXPLAIN (analyze: ANALYZE / ClickHouse PIPELINE) the statement under the cursor. */
  explainAtCursor: (view: EditorView, analyze: boolean) => void
  explain: (sql: string, analyze: boolean) => void
  cancel: () => void
}

/** The statement under the cursor, or the selection. */
function statementUnder(view: EditorView): string | undefined {
  const sel = view.state.selection.main
  const text = view.state.doc.toString()
  return sel.empty ? statementAt(text, sel.head)?.sql : text.slice(sel.from, sel.to).trim()
}

/**
 * Runs or explains the statement under the cursor (or the selection) against one database
 * of a connection ("" = the profile's default).
 */
export function useQueryRunner(connId: string, database: string): QueryRunner {
  const [state, setState] = useState<RunState>(idle)
  const [limit, setLimit] = useState(1000)
  const queryId = useRef('')
  // Refs keep the callbacks stable so the editor keymap isn't rebuilt on every run.
  const latest = useRef({ connId, database, limit })
  latest.current = { connId, database, limit }

  /** Starts a run: cancels the previous one and returns its id, or '' without a connection. */
  const begin = useCallback((sql: string, kind: RunState['kind'], analyze: boolean): string => {
    if (!latest.current.connId) {
      setState((s) => ({ ...s, error: 'Pick a connection first.', result: null, explain: null, notice: null }))
      return ''
    }
    if (queryId.current) void Database.CancelQuery(queryId.current) // don't leave it holding a pooled connection
    const id = crypto.randomUUID()
    queryId.current = id
    const { connId: runConn, database: runDb } = latest.current
    setState({ ...idle, running: true, kind, analyze, sql, startedAt: Date.now(), connId: runConn, database: runDb })
    return id
  }, [])

  const settle = useCallback((id: string, patch: Partial<RunState>) => {
    if (queryId.current !== id) return
    queryId.current = ''
    setState((s) => ({ ...s, running: false, ...patch }))
  }, [])

  const run = useCallback(
    (sql: string) => {
      const { connId: conn, database: db, limit: max } = latest.current
      const id = begin(sql, 'query', false)
      if (!id) return
      Database.RunQuery(conn, db, id, sql, max)
        .then((result) => settle(id, { result }))
        .catch((err) => settle(id, { error: errorMessage(err) }))
    },
    [begin, settle],
  )

  const explain = useCallback(
    (sql: string, analyze: boolean) => {
      const { connId: conn, database: db } = latest.current
      const id = begin(sql, 'explain', analyze)
      if (!id) return
      const go = async () => {
        let res = await Database.Explain(conn, db, id, sql, analyze, false)
        if (res.needsConfirm) {
          if (queryId.current !== id) return
          const ok = await confirmAction(`${res.confirmReason} Run EXPLAIN ANALYZE of “${abbreviate(sql)}”?`, 'Explain Analyze')
          if (!ok) return settle(id, { notice: 'EXPLAIN ANALYZE not run.' })
          if (queryId.current !== id) return
          res = await Database.Explain(conn, db, id, sql, analyze, true)
        }
        settle(id, { explain: res })
      }
      go().catch((err) => settle(id, { error: errorMessage(err) }))
    },
    [begin, settle],
  )

  const runAtCursor = useCallback(
    (view: EditorView) => {
      const sql = statementUnder(view)
      if (sql) run(sql)
    },
    [run],
  )

  const explainAtCursor = useCallback(
    (view: EditorView, analyze: boolean) => {
      const sql = statementUnder(view)
      if (sql) explain(sql, analyze)
    },
    [explain],
  )

  const cancel = useCallback(() => {
    if (queryId.current) void Database.CancelQuery(queryId.current)
  }, [])

  // Closing the console cancels whatever it was still running.
  useEffect(() => cancel, [cancel])

  return useMemo(
    () => ({ state, limit, setLimit, runAtCursor, run, explainAtCursor, explain, cancel }),
    [state, limit, runAtCursor, run, explainAtCursor, explain, cancel],
  )
}

interface PanelProps {
  tabId: string
  scratchId: string
  connId: string
  /** The scratchpad's remembered database ("" = the profile's default). */
  database: string
  runner: QueryRunner
  editor: ReactNode
  view: RefObject<EditorView | null>
}

export function QueryPanel({ tabId, scratchId, connId, database, runner, editor, view }: PanelProps) {
  const connections = useDatabase((s) => s.connections)
  const conn = connections.find((c) => c.id === connId)
  const databases = useDatabase((s) => s.databases[connId])
  const loadDatabases = useDatabase((s) => s.loadDatabases)
  const setConnection = useScratch((s) => s.setConnection)
  const [split, setSplit] = useState(0.55)
  const [historyOpen, setHistoryOpen] = useState(false)
  const { state } = runner

  useEffect(() => {
    if (conn) void loadDatabases(conn.id)
  }, [conn, loadDatabases])

  const explainAtCursor = runner.explainAtCursor
  useEffect(
    () =>
      registerSqlConsole(tabId, {
        explain: (analyze) => view.current && explainAtCursor(view.current, analyze),
        showHistory: () => setHistoryOpen(true),
      }),
    [tabId, view, explainAtCursor],
  )

  const fallback = conn ? defaultDatabaseName(databases?.list, conn) : ''
  const current = database || fallback
  const isClickHouse = conn?.driver === 'clickhouse'

  const insertSql = (sql: string) => {
    const v = view.current
    if (!v) return
    const sel = v.state.selection.main
    const line = v.state.doc.lineAt(sel.from)
    const body = sql.trim()
    const text = /;\s*$/.test(body) || /--[^\n]*$/.test(body) ? body : `${body};`
    const before = line.text.slice(0, sel.from - line.from).trim() ? '\n' : ''
    const after = v.state.doc.lineAt(sel.to).to > sel.to ? '\n' : ''
    const insert = before + text + after
    v.dispatch({ changes: { from: sel.from, to: sel.to, insert }, selection: { anchor: sel.from + before.length + text.length }, scrollIntoView: true })
    v.focus()
  }

  const rerun = (entry: HistoryEntry) => {
    setHistoryOpen(false)
    if (entry.kind === 'explain' || entry.kind === 'explain-analyze') runner.explain(entry.sql, entry.kind === 'explain-analyze')
    else runner.run(entry.sql)
  }

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
        <button
          disabled={state.running || !conn}
          onClick={() => view.current && runner.explainAtCursor(view.current, false)}
          title={isClickHouse ? 'EXPLAIN PLAN of the statement under the cursor (⇧⌘E)' : 'EXPLAIN the statement under the cursor (⇧⌘E)'}
        >
          Explain
        </button>
        <button
          disabled={state.running || !conn}
          onClick={() => view.current && runner.explainAtCursor(view.current, true)}
          title={
            isClickHouse
              ? 'EXPLAIN PIPELINE of the statement under the cursor (⇧⌘A)'
              : 'EXPLAIN ANALYZE: runs the statement in a rolled-back transaction (⇧⌘A)'
          }
        >
          {isClickHouse ? 'Pipeline' : 'Analyze'}
        </button>
        <Select
          size="sm"
          label="limit"
          value={runner.limit}
          options={LIMITS.map((l) => ({ value: l, label: l.toLocaleString() }))}
          onChange={runner.setLimit}
          title="Maximum rows fetched"
        />
        <span className="spacer" />
        <button disabled={!conn} onClick={() => setHistoryOpen(true)} title="Statements run on this connection (⇧⌘H)">
          History
        </button>
      </div>
      <div className="query-split">
        <div style={{ flex: `${split} 1 0` }} className="query-editor">{editor}</div>
        <Splitter direction="vertical" onResize={(ratio) => setSplit(Math.min(0.9, Math.max(0.1, ratio)))} />
        <div style={{ flex: `${1 - split} 1 0` }} className="query-results">
          <ResultArea state={state} conn={connections.find((c) => c.id === state.connId)} onRerun={() => runner.run(state.sql)} />
        </div>
      </div>
      {historyOpen && conn && (
        <QueryHistory
          connId={conn.id}
          connName={conn.name}
          driver={conn.driver}
          database={database}
          onClose={() => {
            setHistoryOpen(false)
            view.current?.focus()
          }}
          onInsert={(entry) => {
            setHistoryOpen(false)
            insertSql(entry.sql)
          }}
          onRun={rerun}
        />
      )}
    </div>
  )
}

interface ResultProps {
  state: RunState
  /** The connection the shown result came from. */
  conn: Connection | undefined
  onRerun: () => void
}

function ResultArea({ state, conn, onRerun }: ResultProps) {
  if (state.running) {
    const what = state.kind === 'explain' ? (state.analyze ? 'Analyzing…' : 'Explaining…') : 'Running…'
    return <div className="placeholder">{what} <code>{abbreviate(state.sql)}</code></div>
  }
  if (state.error) return <div className="placeholder error selectable">{state.error}</div>
  if (state.notice) return <div className="placeholder muted">{state.notice}</div>
  if (state.explain) return <div className="result-area"><ExplainView result={state.explain} sql={state.sql} /></div>
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
      {r.columns.length > 0 && conn ? (
        <EditableResults
          result={r}
          summary={header}
          connId={conn.id}
          database={state.database}
          driver={conn.driver as 'postgres' | 'clickhouse'}
          readOnly={conn.readOnly}
          onApplied={onRerun}
        />
      ) : (
        <div className="result-summary">{header}</div>
      )}
    </div>
  )
}

function abbreviate(sql: string): string {
  const flat = sql.replace(/\s+/g, ' ')
  return flat.length > 120 ? `${flat.slice(0, 117)}…` : flat
}
