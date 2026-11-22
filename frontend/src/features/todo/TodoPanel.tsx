import { Button, IconButton, Input, PanelHeader, TreeRow, TreeSection } from '@flint-ide/ui'
import { RefreshCw } from '@flint-ide/ui/icons'
import { useEffect, useMemo, useState } from 'react'
import { EventsOn } from '../../wails/runtime'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { useSettings } from '../../store/settings'
import { scanTodos, setTodoQuery, toggleTodoTag, useTodo } from './store'
import { affectsTodos, countTags, groupTodos } from './todos'
import { displayPath } from '../../lib/workspaceRoots'
import { FileIcon } from '../../components/icons/FileIcon'

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
      <PanelHeader
        title="TODO"
        actions={<IconButton variant="ghost" size="sm" aria-label="Rescan" title="Rescan" disabled={scanning} onClick={() => void scanTodos()}><RefreshCw /></IconButton>}
      />
      <div className="search-form">
        <Input size="sm" value={query} placeholder="Filter notes" spellCheck={false} onChange={(e) => setTodoQuery(e.target.value)} />
        <div className="todo-tags">
          {TAGS.filter((t) => counts[t]).map((t) => (
            <Button key={t} variant={tags.has(t) ? 'plain' : 'ghost'} size="sm" className={`todo-tag ${t.toLowerCase()}`} aria-pressed={tags.has(t)} onClick={() => toggleTodoTag(t)}>
              {t} {counts[t]}
            </Button>
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
          <TreeSection
            key={f.key}
            title={<span className="todo-file"><FileIcon name={basename(f.path)} />{basename(f.path)}</span>}
            hint={displayPath(roots, f.key).replace(/\/?[^/]*$/, '')}
            count={f.items.length}
            open={!collapsed.has(f.key)}
            onOpenChange={() => toggleFile(f.key)}
          >
            {f.items.map((it) => (
              <TreeRow
                key={`${it.line}:${it.column}`}
                depth={1}
                className="todo-item"
                title={`${it.tag}${it.owner ? `(${it.owner})` : ''}: ${it.text}`}
                onClick={() => useEditor.getState().openFile(f.key, { line: it.line, from: it.column - 1, to: it.column - 1 + it.tag.length })}
                icon={<span className="line-no">{it.line}</span>}
                label={
                  <>
                    <span className={`todo-tag-label ${it.tag.toLowerCase()}`}>{it.tag}</span>
                    {it.owner && <span className="muted">({it.owner}) </span>}
                    <span className="match-text">{it.text || '—'}</span>
                  </>
                }
              />
            ))}
          </TreeSection>
        ))}
        {!scanning && !error && items.length === 0 && <div className="placeholder muted">No TODO, FIXME, HACK, XXX or BUG notes.</div>}
      </div>
    </div>
  )
}
