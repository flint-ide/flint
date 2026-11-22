import { useEffect, useState } from 'react'
import { Button, IconButton, TreeRow, TreeSection } from '@flint-ide/ui'
import { Sparkles, X } from '@flint-ide/ui/icons'
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

  const ours = reviewRoot === root
  if (!running && !error && (!ours || doneAt === 0)) return null
  const counts = severityCounts(ours ? findings : [])
  const shown = ours ? findings : []

  return (
    <TreeSection
      className="review-section"
      title="Agent Review"
      hint={SEVERITIES.map((s) => counts[s] > 0 && (
        <span key={s} className={`review-count ${s}`} title={`${counts[s]} ${s}`}>{counts[s]}</span>
      ))}
      actionsVisible="always"
      actions={
        <>
          {shown.length > 1 && (
            <Button size="sm" variant="ghost" className="agent-action" onClick={() => void fixAllFindings()} title="Put every finding in the agent chat's draft">
              ✦ Fix all
            </Button>
          )}
          {!running && (
            <IconButton size="sm" variant="ghost" aria-label="Clear the review" title="Clear the review" onClick={() => clearReview()}><X /></IconButton>
          )}
        </>
      }
    >
      {running && <RunningRow />}
      {error && !running && (
        <div className="review-error selectable">
          {error}
          <Button size="sm" variant="ghost" onClick={() => void reviewChanges()}>Try again</Button>
        </div>
      )}
      {ours && !running && !error && shown.length === 0 && doneAt > 0 && (
        <div className="muted review-note">No problems found in {files.length} file{files.length === 1 ? '' : 's'}.</div>
      )}
      {ours && notes.map((n) => <div key={n} className="muted review-note">{n}</div>)}
      {shown.map((f) => {
        const sev = severityOf(f.severity)
        return (
          <TreeRow
            key={f.id}
            depth={1}
            className={f.stale ? 'review-row stale' : undefined}
            title={`${location(f)}\n${f.explanation || f.title}`}
            onClick={() => revealFinding(f.id)}
            icon={<span className={`review-dot ${sev}`} aria-label={sev} />}
            file
            label={f.title}
            hint={`${location(f)}${f.stale ? ' · stale' : ''}`}
            actions={
              <>
                <IconButton size="sm" variant="ghost" aria-label="Fix it" title="Fix it: put this finding in the agent chat's draft" onClick={() => void fixFinding(f.id)}><Sparkles /></IconButton>
                <IconButton size="sm" variant="ghost" aria-label="Dismiss" title="Dismiss" onClick={() => dismissFinding(f.id)}><X /></IconButton>
              </>
            }
          />
        )
      })}
    </TreeSection>
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
      <Button size="sm" variant="ghost" onClick={cancelReview}>Cancel</Button>
    </div>
  )
}
