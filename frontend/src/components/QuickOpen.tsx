import { useEffect, useRef, useState } from 'react'
import { Workspace } from '../api/backend'
import { highlight, matchPath, type FileMatch } from '../lib/highlight'
import { useWorkspace } from '../store/workspace'
import { useEditor } from '../store/editor'
import { Modal } from './layout/Modal'
import { FileIcon } from './icons/FileIcon'

const MAX_RESULTS = 60
const STALE_MS = 15_000

/** ⌘P: fuzzy "go to file". Matching runs in Go; only the top results come back. */
export function QuickOpen() {
  const root = useWorkspace((s) => s.root)
  const roots = useWorkspace((s) => s.roots)
  const fileCount = useWorkspace((s) => s.fileCount)
  const filesLoadedAt = useWorkspace((s) => s.filesLoadedAt)
  const refreshFiles = useWorkspace((s) => s.refreshFiles)
  const setQuickOpen = useWorkspace((s) => s.setQuickOpen)
  const openFile = useEditor((s) => s.openFile)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<FileMatch[]>([])
  const [cursor, setCursor] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  // Use the cached index immediately and refresh once in the background if it's stale.
  // Not re-run on file changes: agents may churn files while the picker is open.
  useEffect(() => {
    if (Date.now() - useWorkspace.getState().filesLoadedAt > STALE_MS) void refreshFiles()
  }, [refreshFiles])

  // Re-queried when the index is refreshed; stale answers are dropped by the flag.
  useEffect(() => {
    if (roots.length === 0) return
    let current = true
    Workspace.FindFiles(roots, query, MAX_RESULTS)
      .then((r) => current && setResults(r.matches ?? []))
      .catch(() => current && setResults([]))
    return () => {
      current = false
    }
  }, [roots, query, filesLoadedAt])

  useEffect(() => setCursor(0), [query])
  useEffect(() => {
    list.current?.querySelector('.selected')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  const close = () => setQuickOpen(false)
  const choose = (m: FileMatch | undefined) => {
    if (!m || !root) return
    openFile(matchPath(m, root))
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
      choose(results[cursor])
    }
  }

  return (
    <Modal onClose={close} className="quick-open">
      <input
        autoFocus
        className="quick-input"
        placeholder={root ? `Go to file… (${fileCount.toLocaleString()} files)` : 'Open a folder first'}
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
              key={`${r.root}:${r.path}`}
              className={`quick-item${i === cursor ? ' selected' : ''}`}
              onMouseMove={() => setCursor(i)}
              onClick={() => choose(r)}
            >
              <FileIcon name={r.path.slice(slash + 1)} />
              <span className="file-name">{r.path.slice(slash + 1)}</span>
              {/* highlight() escapes the path and only adds <b> tags */}
              <span className="muted path-hint" dangerouslySetInnerHTML={{ __html: highlight(r) }} />
            </div>
          )
        })}
      </div>
    </Modal>
  )
}
