import { useEffect, useMemo, useRef, useState } from 'react'
import { highlight, prepare, search } from '../lib/fuzzy'
import { useWorkspace } from '../store/workspace'
import { useEditor } from '../store/editor'
import { Modal } from './layout/Modal'
import { FileIcon } from './icons/FileIcon'

const MAX_RESULTS = 60
const STALE_MS = 15_000

/** ⌘P: fuzzy "go to file" over the cached project file list. */
export function QuickOpen() {
  const root = useWorkspace((s) => s.root)
  const files = useWorkspace((s) => s.files)
  const refreshFiles = useWorkspace((s) => s.refreshFiles)
  const setQuickOpen = useWorkspace((s) => s.setQuickOpen)
  const openFile = useEditor((s) => s.openFile)
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  // Use the cached list immediately and refresh once in the background if it's stale.
  // Not re-run on file changes: agents may churn files while the picker is open.
  useEffect(() => {
    if (Date.now() - useWorkspace.getState().filesLoadedAt > STALE_MS) void refreshFiles()
  }, [refreshFiles])

  const prepared = useMemo(() => files.map(prepare), [files])
  const results = useMemo(() => {
    if (!query) return files.slice(0, MAX_RESULTS).map((f) => ({ path: f, html: highlight({ target: f, score: 0, indices: [] }) }))
    return search(query, prepared, MAX_RESULTS).map((r) => ({ path: r.target, html: highlight(r) }))
  }, [query, files, prepared])

  useEffect(() => setCursor(0), [query])
  useEffect(() => {
    list.current?.querySelector('.selected')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  const close = () => setQuickOpen(false)
  const choose = (path: string | undefined) => {
    if (!path || !root) return
    openFile(`${root}/${path}`)
    close()
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n')) {
      e.preventDefault()
      setCursor((c) => Math.min(c + 1, results.length - 1))
    } else if (e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p')) {
      e.preventDefault()
      setCursor((c) => Math.max(c - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      choose(results[cursor]?.path)
    }
  }

  return (
    <Modal onClose={close} className="quick-open">
      <input
        autoFocus
        className="quick-input"
        placeholder={root ? `Go to file… (${files.length.toLocaleString()} files)` : 'Open a folder first'}
        value={query}
        spellCheck={false}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={onKeyDown}
      />
      <div className="quick-list" ref={list}>
        {results.map((r, i) => {
          const slash = r.path.lastIndexOf('/')
          return (
            <div
              key={r.path}
              className={`quick-item${i === cursor ? ' selected' : ''}`}
              onMouseMove={() => setCursor(i)}
              onClick={() => choose(r.path)}
            >
              <FileIcon name={r.path.slice(slash + 1)} />
              <span className="file-name">{r.path.slice(slash + 1)}</span>
              {/* highlight() escapes the path and only adds <b> tags */}
              <span className="muted path-hint" dangerouslySetInnerHTML={{ __html: r.html }} />
            </div>
          )
        })}
      </div>
    </Modal>
  )
}
