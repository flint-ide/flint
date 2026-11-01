import { useState } from 'react'
import { severityOf, type PlacedFinding } from '../../lib/agentReview'
import { dismissFinding, fixAllFindings, fixFinding } from '../../store/agentReview'

// Kept across remounts: the diff re-creates a widget when it scrolls far away.
const collapsed = new Set<string>()

/**
 * One review finding, drawn like a pull request thread: severity, title, the
 * agent's explanation and suggested replacement (plain text, never HTML), and
 * Fix it / Fix all / Dismiss. Fixes go through the agent chat's draft.
 */
export function FindingCard({ finding, agent, total, label }: { finding: PlacedFinding; agent: string; total: number; label?: string }) {
  const [open, setOpen] = useState(!collapsed.has(finding.id))
  const toggle = () => {
    if (open) collapsed.add(finding.id)
    else collapsed.delete(finding.id)
    setOpen(!open)
  }
  const sev = severityOf(finding.severity)
  return (
    <div className={`pr-thread review-finding${finding.stale ? ' stale' : ''}`}>
      <div className="pr-thread-head" onClick={toggle}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span className={`review-sev ${sev}`}>{sev}</span>
        {label && <span className="muted">{label}</span>}
        <b className="truncate">{finding.title}</b>
        <span className="spacer" />
        {finding.stale && <span className="pr-badge pending" title="The code changed after the review">Stale</span>}
        <span className="muted">{agent}</span>
      </div>
      {open && (
        <>
          {(finding.explanation || finding.suggestion) && (
            <div className="pr-comment">
              {finding.explanation && finding.explanation !== finding.title && <div className="review-text selectable">{finding.explanation}</div>}
              {finding.suggestion && (
                <>
                  <div className="muted review-suggestion-label">Suggested replacement</div>
                  <pre className="review-suggestion selectable">{finding.suggestion}</pre>
                </>
              )}
            </div>
          )}
          <div className="pr-reply">
            <div className="pr-actions">
              <button className="primary" onClick={() => void fixFinding(finding.id)} title="Put this finding in the agent chat's draft">✦ Fix it</button>
              {total > 1 && (
                <button onClick={() => void fixAllFindings()} title="Put every finding in one draft">Fix all ({total})</button>
              )}
              <span className="spacer" />
              <button onClick={() => dismissFinding(finding.id)}>Dismiss</button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
