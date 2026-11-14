import { useCallback, useEffect, useRef, useState } from 'react'
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import type { Row, View } from '../../../bindings/flint/internal/extensions/host/models'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { refreshKey, useExtensions } from './store'
import { ViewChart } from './ViewChart'
import { ViewForm } from './ViewForm'
import { ViewTable } from './ViewTable'

const report = (err: unknown) => showToast(errorMessage(err), 'error')

interface Props {
  ext: string
  view: string
  title: string
  /** The tab's key; '' for a sidebar panel. */
  tabKey: string
  /** The tab's saved data, passed to view/resolve. */
  data: unknown
  /** Whether it is on screen (a panel always is while mounted). */
  active: boolean
}

/**
 * A table, form or chart of an extension (docs/design/extensions.md,
 * "Component catalog"), in a sidebar panel or an editor tab: Flint draws it
 * from the data of view/resolve, which Go typed and bounded. The extension
 * runs while it is on screen, and a view/refresh from it loads it again.
 */
export function DeclarativeView({ ext, view, title, tabKey, data, active }: Props) {
  const all = useExtensions((s) => s.refresh[refreshKey(ext, view)] ?? 0)
  const one = useExtensions((s) => (tabKey ? (s.refresh[refreshKey(ext, view, tabKey)] ?? 0) : 0))
  const [root, setRoot] = useState<View | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  // A restored tab resolves when it is first shown, not when the session loads.
  const [shown, setShown] = useState(active)
  useEffect(() => {
    if (active) setShown(true)
  }, [active])
  const dataRef = useRef(data)
  dataRef.current = data
  const latest = useRef(0)

  const load = useCallback(() => {
    const call = ++latest.current
    setLoading(true)
    API.ResolveView(ext, view, tabKey, (dataRef.current ?? null) as never)
      .then(
        (v) => {
          if (call !== latest.current) return // a later load answers
          setRoot(v)
          setError('')
        },
        (err: unknown) => call === latest.current && setError(errorMessage(err)),
      )
      .finally(() => call === latest.current && setLoading(false))
  }, [ext, view, tabKey])

  useEffect(() => {
    if (shown) load()
  }, [shown, load, all, one])

  useEffect(() => {
    if (!shown) return
    void API.SetViewVisible(ext, view, tabKey, active).catch(() => {})
    return () => void API.SetViewVisible(ext, view, tabKey, false).catch(() => {})
  }, [ext, view, tabKey, active, shown])

  const action = (item: string, id: string) => void API.ViewAction(ext, view, tabKey, item, id).catch(report)
  const command = (row: Row) => row.command && void API.Execute(ext, row.command.id, row.command.args ?? null).catch(report)

  const body = error ? (
    <div className="placeholder">
      <span className="error selectable">{error}</span>
      <button className="link" onClick={load}>Try again</button>
    </div>
  ) : !root ? (
    <div className="placeholder muted">{loading ? 'Loading…' : ''}</div>
  ) : root.kind === 'table' && root.table ? (
    <ViewTable table={root.table} truncated={!!root.truncated} onCommand={command} onAction={action} />
  ) : root.kind === 'form' && root.form ? (
    <div className="scroll ext-view-scroll">
      <ViewForm form={root.form} onSubmit={(values) => API.SubmitForm(ext, view, tabKey, values as never)} onAccepted={load} />
    </div>
  ) : root.kind === 'chart' && root.chart ? (
    <div className="scroll ext-view-scroll">
      <ViewChart chart={root.chart} truncated={!!root.truncated} compact={!tabKey} />
    </div>
  ) : root.kind === 'message' ? (
    <div className="placeholder">
      <span>{root.text}</span>
      {root.actions?.map((a) => (
        <button key={a.id} className="link" onClick={() => action('', a.id)}>
          {a.label}
        </button>
      ))}
    </div>
  ) : (
    <div className="placeholder muted">This Flint can't draw a “{root.kind}” view.</div>
  )

  return (
    <div className={tabKey ? 'panel ext-view-tab' : 'panel'}>
      <div className="panel-header">
        <span className="panel-title">{title}</span>
        <button className="icon" title="Refresh" aria-label={`Refresh ${title}`} disabled={loading} onClick={load}>↻</button>
      </div>
      {body}
    </div>
  )
}
