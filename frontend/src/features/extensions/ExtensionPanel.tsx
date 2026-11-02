import { useCallback, useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import type { Item, View } from '../../../bindings/flint/internal/extensions/host/models'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { openContextMenu } from '../../store/contextMenu'
import { Icon } from '../../components/ui/Icon'
import { isIconName } from '../../components/ui/icons'
import { refreshKey, useExtensions } from './store'

/** A tree node's children: loaded, loading, or why they couldn't be. */
type Children = { items: readonly Item[] } | { loading: true } | { error: string }

const report = (err: unknown) => showToast(errorMessage(err), 'error')

/**
 * A panel of an extension, drawn by Flint from the data of view/resolve: a
 * tree (expanded lazily) or a list, or a message. The extension runs while
 * the panel is on screen, and a view/refresh from it reloads what is shown.
 */
export function ExtensionPanel({ ext, view, title, kind }: { ext: string; view: string; title: string; kind: string }) {
  const token = useExtensions((s) => s.refresh[refreshKey(ext, view)] ?? 0)
  const [root, setRoot] = useState<View | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [children, setChildren] = useState<Readonly<Record<string, Children>>>({})
  const expandedRef = useRef(expanded)
  expandedRef.current = expanded

  const loadChildren = useCallback(
    (id: string) => {
      setChildren((c) => ({ ...c, [id]: c[id] && 'items' in c[id] ? c[id] : { loading: true } }))
      API.Resolve(ext, view, id).then(
        (v) => setChildren((c) => ({ ...c, [id]: { items: v.items ?? [] } })),
        (err: unknown) => setChildren((c) => ({ ...c, [id]: { error: errorMessage(err) } })),
      )
    },
    [ext, view],
  )

  const load = useCallback(() => {
    setLoading(true)
    API.Resolve(ext, view, '')
      .then(
        (v) => {
          setRoot(v)
          setError('')
        },
        (err: unknown) => setError(errorMessage(err)),
      )
      .finally(() => setLoading(false))
    expandedRef.current.forEach(loadChildren)
  }, [ext, view, loadChildren])

  useEffect(() => {
    void API.SetVisible(ext, view, true).catch(() => {})
    return () => void API.SetVisible(ext, view, false).catch(() => {})
  }, [ext, view])
  useEffect(load, [load, token])

  const toggle = (id: string) => {
    const next = new Set(expanded)
    if (!next.delete(id)) {
      next.add(id)
      loadChildren(id)
    }
    setExpanded(next)
  }

  const activate = (item: Item) => {
    if (item.command) {
      API.Execute(ext, item.command.id, item.command.args ?? null).catch(report)
    } else if (item.hasChildren) toggle(item.id)
  }

  const menu = (e: MouseEvent, item: Item) => {
    if (!item.actions?.length) return
    e.preventDefault()
    openContextMenu(
      e.clientX,
      e.clientY,
      item.actions.map((a) => ({ label: a.label, onSelect: () => void API.Action(ext, view, item.id, a.id).catch(report) })),
    )
  }

  const rows = (items: readonly Item[], depth: number): ReactNode[] =>
    items.flatMap((item) => {
      const open = expanded.has(item.id)
      const sub = children[item.id]
      const row = (
        <div
          key={item.id}
          className="tree-row static extension-row"
          style={{ paddingLeft: 8 + depth * 14 }}
          title={item.tooltip || item.label}
          onClick={() => activate(item)}
          onContextMenu={(e) => menu(e, item)}
        >
          {kind === 'tree' && (
            <span
              className="twisty"
              onClick={(e) => {
                e.stopPropagation()
                if (item.hasChildren) toggle(item.id)
              }}
            >
              {item.hasChildren ? (open ? '▾' : '▸') : ''}
            </span>
          )}
          {item.icon && isIconName(item.icon) && <Icon name={item.icon} size={14} />}
          <span className="extension-label">{item.label}</span>
          {item.description && <span className="muted path-hint">{item.description}</span>}
        </div>
      )
      if (!open || !item.hasChildren) return [row]
      if (!sub || 'loading' in sub) return [row, <div key={`${item.id}\n…`} className="tree-row static muted" style={{ paddingLeft: 22 + depth * 14 }}>Loading…</div>]
      if ('error' in sub) return [row, <div key={`${item.id}\n!`} className="tree-row static error" style={{ paddingLeft: 22 + depth * 14 }}>{sub.error}</div>]
      return [row, ...rows(sub.items, depth + 1)]
    })

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">{title}</span>
        <button className="icon" title="Refresh" disabled={loading} onClick={load}>↻</button>
      </div>
      <div className="tree scroll">
        {error ? (
          <div className="placeholder">
            <span className="error">{error}</span>
            <button className="link" onClick={load}>Try again</button>
          </div>
        ) : !root ? (
          <div className="placeholder muted">{loading ? 'Loading…' : ''}</div>
        ) : root.kind === 'message' ? (
          <div className="placeholder">
            <span>{root.text}</span>
            {root.actions?.map((a) => (
              <button key={a.id} className="link" onClick={() => void API.Action(ext, view, '', a.id).catch(report)}>
                {a.label}
              </button>
            ))}
          </div>
        ) : root.items.length === 0 ? (
          <div className="placeholder muted">Nothing here.</div>
        ) : (
          <>
            {rows(root.items, 0)}
            {root.truncated && <div className="placeholder muted">Only the first 5,000 items are shown.</div>}
          </>
        )}
      </div>
    </div>
  )
}
