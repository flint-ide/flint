import { useState } from 'react'
import type { ReviewThread as Thread } from '../../api/backend'
import { addReviewComment, openInBrowser, replyToThread, setThreadResolved, usePullRequests, detailsKey } from '../../store/pullRequests'
import { timeAgo } from '../../lib/prReview'
import { formatKeys } from '../../lib/commands'

/** Markdown rendered in Go (no raw HTML); links open in the browser. */
export function HtmlBody({ html, className = '' }: { html: string; className?: string }) {
  const onClick = (e: React.MouseEvent) => {
    const a = (e.target as HTMLElement).closest('a')
    if (!a) return
    e.preventDefault()
    openInBrowser(a.getAttribute('href') ?? '')
  }
  return <div className={`gh-md selectable ${className}`} onClick={onClick} dangerouslySetInnerHTML={{ __html: html }} />
}

const isSend = (e: React.KeyboardEvent) => e.key === 'Enter' && (e.metaKey || e.ctrlKey)

// The editor re-creates a thread's element when it scrolls far out of view, which
// remounts the thread: unsent text and collapsed state live here instead.
const sticky = new Map<string, unknown>()

function useSticky<T>(key: string, initial: T): [T, (v: T) => void] {
  const [value, setValue] = useState<T>(() => (sticky.has(key) ? (sticky.get(key) as T) : initial))
  const update = (v: T) => {
    if (v === initial) sticky.delete(key)
    else sticky.set(key, v)
    setValue(v)
  }
  return [value, update]
}

interface ThreadProps {
  root: string
  number: number
  thread: Thread
  /** Shown above the comments, e.g. "Outdated · line 12" for threads off the diff. */
  label?: string
}

/** A review thread: its comments, a reply box and Resolve. Resolved threads start collapsed. */
export function ReviewThread({ root, number, thread, label }: ThreadProps) {
  const [open, setOpen] = useSticky(`open:${thread.id}:${thread.resolved}`, !thread.resolved)
  const [reply, setReply] = useSticky(`reply:${thread.id}`, '')
  const busy = usePullRequests((s) => !!s.busy[detailsKey(root, number)])
  const first = thread.comments[0]

  const send = async () => {
    if (!reply.trim() || busy) return
    if (await replyToThread(root, number, thread.id, reply)) setReply('')
  }
  const canToggle = thread.resolved ? thread.canUnresolve : thread.canResolve

  return (
    <div className={`pr-thread${thread.resolved ? ' resolved' : ''}`}>
      <div className="pr-thread-head" onClick={() => setOpen(!open)}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        {label && <span className="muted">{label}</span>}
        {!open && first && (
          <span className="truncate">
            <b>{first.author}</b> <span className="muted">{first.body.split('\n')[0]}</span>
          </span>
        )}
        <span className="spacer" />
        {thread.resolved && <span className="pr-badge ok">Resolved{thread.resolvedBy ? ` by ${thread.resolvedBy}` : ''}</span>}
        {thread.comments.length > 1 && <span className="muted">{thread.comments.length} comments</span>}
      </div>
      {open && (
        <>
          {thread.comments.map((c) => (
            <div key={c.id} className="pr-comment">
              <div className="pr-comment-head">
                <b>{c.author || 'ghost'}</b>
                <span className="muted">{timeAgo(c.createdAt)}</span>
                {c.pending && <span className="pr-badge pending">Pending</span>}
                <span className="spacer" />
                {c.url.startsWith('http') && (
                  <button className="icon" title="Open on GitHub" onClick={() => openInBrowser(c.url)}>↗</button>
                )}
              </div>
              <HtmlBody html={c.bodyHtml} />
            </div>
          ))}
          <div className="pr-reply">
            {thread.canReply && (
              <textarea
                rows={reply ? 3 : 1}
                value={reply}
                placeholder={`Reply… (${formatKeys('Mod+Enter')} to send)`}
                spellCheck={false}
                onChange={(e) => setReply(e.target.value)}
                onKeyDown={(e) => {
                  if (isSend(e)) {
                    e.preventDefault()
                    void send()
                  }
                }}
              />
            )}
            <div className="pr-actions">
              {thread.canReply && (
                <button className="primary" disabled={!reply.trim() || busy} onClick={() => void send()}>Reply</button>
              )}
              <span className="spacer" />
              {canToggle && (
                <button disabled={busy} onClick={() => void setThreadResolved(root, number, thread.id, !thread.resolved)}>
                  {thread.resolved ? 'Unresolve' : 'Resolve'}
                </button>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  )
}

interface ComposerProps {
  root: string
  number: number
  path: string
  /** 1-based line on side. */
  line: number
  side: 'LEFT' | 'RIGHT'
  /** My pending review exists: "Add review comment" instead of "Start a review". */
  pendingReview: boolean
  onDone: () => void
}

/** The new-comment box under a line: a single comment now, or part of my review. */
export function CommentComposer({ root, number, path, line, side, pendingReview, onDone }: ComposerProps) {
  const [body, setBody] = useSticky(`new:${root}#${number}:${path}`, '')
  const busy = usePullRequests((s) => !!s.busy[detailsKey(root, number)])
  const post = async (review: boolean) => {
    if (!body.trim() || busy) return
    if (await addReviewComment(root, number, { path, line, side, body, review })) {
      setBody('')
      onDone()
    }
  }
  const cancel = () => {
    setBody('')
    onDone()
  }
  return (
    <div className="pr-thread pr-composer">
      <div className="pr-thread-head muted">
        New comment on {side === 'LEFT' ? 'base' : 'head'} line {line}
      </div>
      <div className="pr-reply">
        <textarea
          rows={3}
          autoFocus
          value={body}
          placeholder={`Leave a comment (Markdown). ${formatKeys('Mod+Enter')} adds it to your review.`}
          spellCheck={false}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            if (isSend(e)) {
              e.preventDefault()
              void post(true)
            }
            if (e.key === 'Escape' && !body) cancel()
          }}
        />
        <div className="pr-actions">
          <button className="primary" disabled={!body.trim() || busy} onClick={() => void post(true)} title={`Only you see it until the review is submitted (${formatKeys('Mod+Enter')})`}>
            {pendingReview ? 'Add review comment' : 'Start a review'}
          </button>
          <button disabled={!body.trim() || busy} onClick={() => void post(false)} title="Posted right away">
            Add single comment
          </button>
          <span className="spacer" />
          <button className="link" onClick={cancel}>Cancel</button>
        </div>
      </div>
    </div>
  )
}
