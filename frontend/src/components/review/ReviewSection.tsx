import { useEffect, useState } from 'react'
import { SEVERITIES, location, severityCounts, severityOf } from '../../lib/agentReview'
import {
  cancelReview, clearReview, dismissFinding, fixAllFindings, fixFinding, reviewChanges, revealFinding, runningLabel, useAgentReview,
} from '../../store/agentReview'

/**
 * The Commit panel's review: progress while the agent works, then the findings
 * with counts per severity. A click opens the diff at the finding's line.
 */
export function ReviewSection({ root }: { root: string }) {
  const running = useAgentReview((s) => s.running)
  const findings = useAgentReview((s) => s.findings)
  const reviewRoot = useAgentReview((s) => s.root)
  const notes = useAgentReview((s) => s.notes)
  const error = useAgentReview((s) => s.error)
  const files = useAgentReview((s) => s.files)
  const doneAt = useAgentReview((s) => s.doneAt)
  const [open, setOpen] = useState(true)

  const ours = reviewRoot === root
  if (!running && !error && (!ours || doneAt === 0)) return null
  const counts = severityCounts(ours ? findings : [])
  const shown = ours ? findings : []

  return (
    <div className="review-section">
      <div className="tree-row static group-row" onClick={() => setOpen(!open)}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span className="group-title">Agent Review</span>
        {SEVERITIES.map((s) => counts[s] > 0 && (
          <span key={s} className={`review-count ${s}`} title={`${counts[s]} ${s}`}>{counts[s]}</span>
        ))}
        <span className="spacer" />
        {shown.length > 1 && (
          <button className="link agent-action" onClick={(e) => (e.stopPropagation(), void fixAllFindings())} title="Put every finding in the agent chat's draft">
            ✦ Fix all
          </button>
        )}
        {!running && (
          <button className="icon" title="Clear the review" onClick={(e) => (e.stopPropagation(), clearReview())}>×</button>
        )}
      </div>
      {open && (
        <>
          {running && <RunningRow />}
          {error && !running && (
            <div className="review-error selectable">
              {error}
              <button className="link" onClick={() => void reviewChanges()}>Try again</button>
            </div>
          )}
          {ours && !running && !error && shown.length === 0 && doneAt > 0 && (
            <div className="muted review-note">No problems found in {files.length} file{files.length === 1 ? '' : 's'}.</div>
          )}
          {ours && notes.map((n) => <div key={n} className="muted review-note">{n}</div>)}
          {shown.map((f) => {
            const sev = severityOf(f.severity)
            return (
              <div
                key={f.id}
                className={`tree-row static review-row${f.stale ? ' stale' : ''}`}
                title={`${location(f)}\n${f.explanation || f.title}`}
                onClick={() => revealFinding(f.id)}
              >
                <span className={`review-dot ${sev}`} aria-label={sev} />
                <span className="file-name truncate">{f.title}</span>
                <span className="muted path-hint">{location(f)}{f.stale ? ' · stale' : ''}</span>
                <span className="row-actions" onClick={(e) => e.stopPropagation()}>
                  <button className="icon" title="Fix it: put this finding in the agent chat's draft" onClick={() => void fixFinding(f.id)}>✦</button>
                  <button className="icon" title="Dismiss" onClick={() => dismissFinding(f.id)}>×</button>
                </span>
              </div>
            )
          })}
        </>
      )}
    </div>
  )
}

function RunningRow() {
  const running = useAgentReview((s) => s.running)
  const [, tick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 1000)
    return () => clearInterval(id)
  }, [])
  if (!running) return null
  const seconds = Math.max(0, Math.round((Date.now() - running.startedAt) / 1000))
  return (
    <div className="review-running">
      <span className="review-pulse" />
      <span className="truncate">{runningLabel(running)}</span>
      <span className="muted">{seconds}s</span>
      <span className="spacer" />
      <button className="link" onClick={cancelReview}>Cancel</button>
    </div>
  )
}
