import { useEffect, useMemo, useRef, useState } from 'react'
import fuzzysort from 'fuzzysort'
import { useWorkspace } from '../store/workspace'
import { useEditor } from '../store/editor'
import { Modal } from './layout/Modal'

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

  const prepared = useMemo(() => files.map((f) => fuzzysort.prepare(f)), [files])
  const results = useMemo(() => {
    if (!query) return files.slice(0, MAX_RESULTS).map((f) => ({ path: f, html: f }))
    return fuzzysort.go(query, prepared, { limit: MAX_RESULTS, threshold: 0.2 }).map((r) => ({
      path: r.target,
      html: r.highlight('<b>', '</b>'),
    }))
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
              <span className="file-name">{r.path.slice(slash + 1)}</span>
              {/* fuzzysort highlight output only wraps our own file paths in <b> tags */}
              <span className="muted path-hint" dangerouslySetInnerHTML={{ __html: escapeExceptBold(r.html) }} />
            </div>
          )
        })}
      </div>
    </Modal>
  )
}

/** Escapes HTML in a highlighted path while keeping the <b> markers fuzzysort inserted. */
function escapeExceptBold(html: string): string {
  return html
    .split(/(<\/?b>)/)
    .map((part) => (part === '<b>' || part === '</b>' ? part : part.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')))
    .join('')
}
