import { useEffect, useMemo, useRef, useState } from 'react'
import { Git, useHeadVersion } from '../../store/git'
import { revisionDiffTabId, type DiffTab, type HistoryTab } from '../../store/editor'
import { compareWithCurrent, openCommitDiff, type FileRevision } from '../../store/gitHistory'
import { errorMessage } from '../../api/backend'
import { formatDateTime, joinRoot } from '../../lib/blame'
import type { git } from '../../wails/models'
import { DiffView } from './DiffView'

const LIMIT = 500

type FileCommit = git.FileCommit

interface History {
  root: string
  commits: FileCommit[]
}

/** The file's change in a commit, as a revision pair (left: first parent). */
function revisionOf(c: FileCommit): FileRevision {
  return { hash: c.commit.hash, path: c.path, basePath: c.origPath || c.path }
}

/**
 * IntelliJ-style file history: commits touching the file (following renames) on top,
 * the selected commit's change below, in the regular diff view.
 */
export function HistoryView({ tab, active }: { tab: HistoryTab; active: boolean }) {
  const [history, setHistory] = useState<History | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState(0)
  const headVersion = useHeadVersion(tab.path)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false
    Git.FileHistory(tab.path, LIMIT)
      .then((h) => {
        if (cancelled) return
        setHistory({ root: h.root, commits: h.commits ?? [] })
        setError(null)
      })
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [tab.path, headVersion])

  useEffect(() => {
    if (active) list.current?.focus()
  }, [active, history])

  useEffect(() => {
    list.current?.querySelector('.history-row.selected')?.scrollIntoView({ block: 'nearest' })
  }, [selected])

  const commits = history?.commits ?? []
  const current = commits[Math.min(selected, commits.length - 1)]
  const diff = useMemo<DiffTab | null>(() => {
    if (!history || !current) return null
    const rev = revisionOf(current)
    const d = {
      repoRoot: history.root,
      path: joinRoot(history.root, rev.path),
      basePath: joinRoot(history.root, rev.basePath),
      baseRev: `${rev.hash}^`,
      rev: rev.hash,
      baseLabel: `${rev.hash.slice(0, 7)}^`,
      label: rev.hash.slice(0, 7),
    }
    return { ...d, id: revisionDiffTabId(d), kind: 'diff', title: '', dirty: false }
  }, [history, current])

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') setSelected((i) => Math.min(i + 1, commits.length - 1))
    else if (e.key === 'ArrowUp') setSelected((i) => Math.max(i - 1, 0))
    else if (e.key === 'Enter' && history && current) openCommitDiff(history.root, revisionOf(current))
    else return
    e.preventDefault()
  }

  return (
    <div className="history-view">
      <div className="diff-header">
        <span className="truncate" title={tab.path}>History of {tab.path.split('/').pop()}</span>
        <span className="muted">{history ? `${commits.length}${commits.length === LIMIT ? '+' : ''} commits` : ''}</span>
        <span className="spacer" />
        <button
          className="link"
          disabled={!current}
          title="Open this commit's change in its own diff tab (↵)"
          onClick={() => history && current && openCommitDiff(history.root, revisionOf(current))}
        >
          Open in tab
        </button>
        <button
          className="link"
          disabled={!current}
          title="Compare the file at this commit with the working tree"
          onClick={() => history && current && compareWithCurrent(history.root, current.commit.hash, current.path, tab.path)}
        >
          Compare with current
        </button>
      </div>
      {error && <div className="placeholder error selectable">{error}</div>}
      {!history && !error && <div className="placeholder">Loading…</div>}
      {history && commits.length === 0 && <div className="placeholder muted">No commits touch this file yet.</div>}
      {commits.length > 0 && (
        <div className="history-list" ref={list} tabIndex={0} onKeyDown={onKey}>
          {commits.map((c, i) => (
            <div
              key={c.commit.hash}
              className={`history-row${c === current ? ' selected' : ''}`}
              title={`${c.commit.hash}\n${c.commit.author} <${c.commit.email}>${c.origPath ? `\nrenamed from ${c.origPath}` : ''}`}
              onClick={() => setSelected(i)}
              onDoubleClick={() => history && openCommitDiff(history.root, revisionOf(c))}
            >
              <span className="history-hash">{c.commit.hash.slice(0, 7)}</span>
              <span className="history-subject">
                {c.commit.subject}
                {c.commit.refs && <span className="history-refs"> {c.commit.refs}</span>}
              </span>
              {c.kind !== 'modified' && <span className={`change-kind ${c.kind}`}>{c.kind[0].toUpperCase()}</span>}
              <span className="history-author muted">{c.commit.author}</span>
              <span className="history-date muted">{formatDateTime(c.commit.time)}</span>
            </div>
          ))}
        </div>
      )}
      {diff && (
        <div className="history-diff">
          <DiffView tab={diff} />
        </div>
      )}
    </div>
  )
}
