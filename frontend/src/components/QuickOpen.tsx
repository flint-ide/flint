import { memo, useEffect, useState } from 'react'
import { Workspace } from '../api/backend'
import { highlight, matchPath, type FileMatch } from '../lib/highlight'
import { useWorkspace } from '../store/workspace'
import { useEditor } from '../store/editor'
import { Modal } from './layout/Modal'
import { FileIcon } from './icons/FileIcon'
import { useListCursor } from '../lib/useListCursor'
import { useEventCallback } from '../lib/useEventCallback'

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
  const { listRef, cursor, setCursor, hover } = useListCursor()

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

  useEffect(() => setCursor(0), [query, setCursor])

  const close = () => setQuickOpen(false)
  const choose = useEventCallback((m: FileMatch | undefined) => {
    if (!m || !root) return
    openFile(matchPath(m, root))
    close()
  })

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
      <div className="quick-list" ref={listRef}>
        {results.map((r, i) => (
          <MatchRow key={`${r.root}:${r.path}`} match={r} index={i} selected={i === cursor} onHover={hover} onPick={choose} />
        ))}
      </div>
    </Modal>
  )
}

/** One result; memoized so moving the highlight re-renders only two rows. */
const MatchRow = memo(function MatchRow({ match, index, selected, onHover, onPick }: {
  match: FileMatch
  index: number
  selected: boolean
  onHover: (i: number) => void
  onPick: (m: FileMatch) => void
}) {
  const name = match.path.slice(match.path.lastIndexOf('/') + 1)
  return (
    <div className={`quick-item${selected ? ' selected' : ''}`} onMouseMove={() => onHover(index)} onClick={() => onPick(match)}>
      <FileIcon name={name} />
      <span className="file-name">{name}</span>
      {/* highlight() escapes the path and only adds <b> tags */}
      <span className="muted path-hint" dangerouslySetInnerHTML={{ __html: highlight(match) }} />
    </div>
  )
})
