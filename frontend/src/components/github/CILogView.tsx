import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { GitHub, errorMessage } from '../../api/backend'
import type { CILogTab } from '../../store/editor'
import { useEditor } from '../../store/editor'
import { showToast } from '../../store/dialog'
import { findPathLinks, resolvePath } from '../../lib/pathLinks'
import { fixCIWithAgent, rerunFailedJobs } from '../../store/ci'

/** Lines rendered at a time: the end of the log first, earlier pages on request. */
export const LOG_PAGE = 1000

interface LogState {
  text: string
  files: Record<string, string>
  truncated: boolean
}

/** The failed steps' log of a run: read-only, file:line references open in the editor. */
export function CILogView({ tab }: { tab: CILogTab }) {
  const [log, setLog] = useState<LogState | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)
  const [shown, setShown] = useState(LOG_PAGE)

  useEffect(() => {
    let live = true
    setError(null)
    setShown(LOG_PAGE)
    GitHub.CILog(tab.root, tab.runId, tab.jobId)
      .then((l) => live && setLog({ text: l.text, files: (l.files ?? {}) as Record<string, string>, truncated: l.truncated }))
      .catch((err) => live && setError(errorMessage(err)))
    return () => {
      live = false
    }
  }, [tab.root, tab.runId, tab.jobId, nonce])

  const lines = useMemo(() => (log ? log.text.split('\n') : []), [log])
  const first = Math.max(0, lines.length - shown)
  const open = (path: string, line?: number, column?: number) => {
    const mapped = log?.files[path]
    if (!mapped) {
      showToast(`${path} is not in this workspace`, 'error')
      return
    }
    const col = Math.max(0, (column ?? 1) - 1)
    useEditor.getState().openFile(resolvePath(tab.root, mapped), line ? { line, from: col, to: col } : undefined)
  }

  return (
    <div className="ci-log">
      <div className="ci-log-bar">
        <span className="truncate muted">{tab.title.replace(/^Log: /, '')}</span>
        <span className="spacer" />
        <button
          className="link"
          title="Put the failure in the agent chat for you to review and send"
          onClick={() => void fixCIWithAgent({ root: tab.root, pr: null, checks: [] }, { runId: tab.runId, jobId: tab.jobId })}
        >
          ✦ Fix with agent
        </button>
        <button className="link" title="Re-run the failed jobs on GitHub (asks first)" onClick={() => void rerunFailedJobs(tab.root, tab.runId, `run ${tab.runId}`)}>
          Rerun failed jobs
        </button>
        <button className="link" onClick={() => setNonce(nonce + 1)}>Reload</button>
      </div>
      {error && <div className="placeholder error selectable">{error}</div>}
      {!log && !error && <div className="placeholder muted">Loading the log…</div>}
      {log && (
        <pre className="ci-log-text selectable">
          {first > 0 ? (
            <button className="link" onClick={() => setShown(shown + LOG_PAGE)}>
              Show {Math.min(LOG_PAGE, first)} earlier lines ({first} hidden)
            </button>
          ) : (
            log.truncated && <div className="muted">… the start of the log is left out (it was longer than Flint keeps)</div>
          )}
          {lines.slice(first).map((line, i) => (
            <div key={first + i} className={line.startsWith('── ') ? 'ci-log-step' : undefined}>
              {linkify(line, log.files, open)}
            </div>
          ))}
        </pre>
      )}
    </div>
  )
}

/** Turns the line's file references that exist in the workspace into links. */
function linkify(line: string, files: Record<string, string>, open: (path: string, line?: number, column?: number) => void): ReactNode {
  const links = findPathLinks(line).filter((l) => files[l.path])
  if (links.length === 0) return line || ' '
  const parts: ReactNode[] = []
  let pos = 0
  links.forEach((l, i) => {
    parts.push(line.slice(pos, l.start))
    parts.push(
      <a key={i} className="path-link" title={files[l.path]} onClick={() => open(l.path, l.line, l.column)}>
        {line.slice(l.start, l.end)}
      </a>,
    )
    pos = l.end
  })
  parts.push(line.slice(pos))
  return parts
}
