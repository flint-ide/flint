import { useState } from 'react'
import type { PullRequestDetails } from '../../api/backend'
import { detailsKey, discardReview, submitReview, usePullRequests, type ReviewEvent } from '../../store/pullRequests'

/** "Submit review": a summary plus Comment / Approve / Request changes, and my pending review. */
export function ReviewBar({ root, details }: { root: string; details: PullRequestDetails }) {
  const [body, setBody] = useState('')
  const [expanded, setExpanded] = useState(false)
  const busy = usePullRequests((s) => s.busy[detailsKey(root, details.number)])
  const pending = details.pendingReviewId ? details.pendingReviewComments : -1

  if (details.state !== 'OPEN') return null

  const submit = async (event: ReviewEvent) => {
    if (await submitReview(root, details.number, event, body)) {
      setBody('')
      setExpanded(false)
    }
  }
  // GitHub needs a summary for "comment" and "request changes" unless the review has comments.
  const needsBody = !body.trim() && pending <= 0

  return (
    <div className={`pr-review-bar${expanded ? ' expanded' : ''}`}>
      {pending >= 0 && (
        <div className="pr-pending">
          <span className="pr-badge pending">Pending review</span>
          <span className="muted">
            {pending} comment{pending === 1 ? '' : 's'} only you can see until you submit
          </span>
          <span className="spacer" />
          <button className="link" disabled={!!busy} onClick={() => void discardReview(root, details.number, pending)}>Discard</button>
        </div>
      )}
      <textarea
        rows={expanded ? 4 : 1}
        value={body}
        placeholder="Review summary (Markdown, optional for approvals)"
        spellCheck={false}
        onFocus={() => setExpanded(true)}
        onChange={(e) => setBody(e.target.value)}
      />
      <div className="pr-actions">
        {busy === 'submit' && <span className="muted">Submitting…</span>}
        <span className="spacer" />
        <button disabled={!!busy || needsBody} onClick={() => void submit('COMMENT')} title="Submit general feedback">Comment</button>
        <button className="pr-approve" disabled={!!busy} onClick={() => void submit('APPROVE')}>Approve</button>
        <button className="pr-request" disabled={!!busy || needsBody} onClick={() => void submit('REQUEST_CHANGES')}>Request changes</button>
      </div>
    </div>
  )
}
