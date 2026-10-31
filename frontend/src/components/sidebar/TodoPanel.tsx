import { useEffect, useMemo, useState } from 'react'
import { EventsOn } from '../../wails/runtime'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { useSettings } from '../../store/settings'
import { scanTodos, setTodoQuery, toggleTodoTag, useTodo } from '../../store/todo'
import { affectsTodos, countTags, groupTodos } from '../../lib/todos'
import { displayPath } from '../../lib/workspaceRoots'
import { FileIcon } from '../icons/FileIcon'

const TAGS = ['TODO', 'FIXME', 'HACK', 'XXX', 'BUG']
/** Edits come in bursts (an agent writing files); a rescan walks the whole
 * workspace, so it waits until they settle. */
const RESCAN_DELAY_MS = 3000

const basename = (p: string) => p.slice(p.lastIndexOf('/') + 1)

/** TODO / FIXME / HACK / XXX / BUG markers of the workspace, by file; click to jump. */
export function TodoPanel() {
  const roots = useWorkspace((s) => s.roots)
  const { items, truncated, skipped, scanning, error, tags, query } = useTodo()
  // Joined so a reloaded but equal list doesn't rescan.
  const exclude = useSettings((s) => s.settings.todoExclude.join('\n'))
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
  const files = useMemo(() => groupTodos(items, tags, query), [items, tags, query])
  const counts = useMemo(() => countTags(items), [items])

  // Scan when shown and after file changes, only while the panel is mounted.
  useEffect(() => {
    void scanTodos()
    let timer: ReturnType<typeof setTimeout> | undefined
    const off = EventsOn('fs:changed', (paths: string[]) => {
      const { skipped } = useTodo.getState()
      if (!paths.some((p) => affectsTodos(p, skipped))) return
      clearTimeout(timer)
      timer = setTimeout(() => void scanTodos(), RESCAN_DELAY_MS)
    })
    return () => {
      clearTimeout(timer)
      off()
    }
  }, [roots, exclude])

  const shown = files.reduce((n, f) => n + f.items.length, 0)
  const toggleFile = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (!next.delete(key)) next.add(key)
      return next
    })

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">TODO</span>
        <button className="icon" title="Rescan" disabled={scanning} onClick={() => void scanTodos()}>↻</button>
      </div>
      <div className="search-form">
        <div className="search-input">
          <input value={query} placeholder="Filter notes" spellCheck={false} onChange={(e) => setTodoQuery(e.target.value)} />
        </div>
        <div className="todo-tags">
          {TAGS.filter((t) => counts[t]).map((t) => (
            <button key={t} className={`toggle todo-tag ${t.toLowerCase()}${tags.has(t) ? ' on' : ''}`} onClick={() => toggleTodoTag(t)}>
              {t} {counts[t]}
            </button>
          ))}
        </div>
        <div className="search-status muted">
          {error ? (
            <span className="error">{error}</span>
          ) : scanning && items.length === 0 ? (
            'Scanning…'
          ) : (
            `${shown} in ${files.length} files${truncated ? ' (truncated)' : ''}`
          )}
          {skipped.length > 0 && (
            <span
              className="todo-skipped"
              title={`Dependency folders left out (more in Settings → TODO List):\n${skipped.map((d) => displayPath(roots, d)).join('\n')}`}
            >
              {' · '}{skipped.length} dependency {skipped.length === 1 ? 'folder' : 'folders'} skipped
            </span>
          )}
        </div>
      </div>
      <div className="tree scroll">
        {files.map((f) => (
          <div key={f.key}>
            <div className="tree-row search-file" title={f.key} onClick={() => toggleFile(f.key)}>
              <span className="twisty">{collapsed.has(f.key) ? '▸' : '▾'}</span>
              <FileIcon name={basename(f.path)} />
              <span className="file-name">{basename(f.path)}</span>
              <span className="muted path-hint">{displayPath(roots, f.key).replace(/\/?[^/]*$/, '')}</span>
              <span className="badge">{f.items.length}</span>
            </div>
            {!collapsed.has(f.key) &&
              f.items.map((it) => (
                <div
                  key={`${it.line}:${it.column}`}
                  className="tree-row todo-item"
                  title={`${it.tag}${it.owner ? `(${it.owner})` : ''}: ${it.text}`}
                  onClick={() => useEditor.getState().openFile(f.key, { line: it.line, from: it.column - 1, to: it.column - 1 + it.tag.length })}
                >
                  <span className="line-no">{it.line}</span>
                  <span className={`todo-tag-label ${it.tag.toLowerCase()}`}>{it.tag}</span>
                  {it.owner && <span className="muted">({it.owner})</span>}
                  <span className="match-text">{it.text || '—'}</span>
                </div>
              ))}
          </div>
        ))}
        {!scanning && !error && items.length === 0 && <div className="placeholder muted">No TODO, FIXME, HACK, XXX or BUG notes.</div>}
      </div>
    </div>
  )
}
