import { useEffect, useMemo, useState } from 'react'
import type { PullRequestCheck, PullRequestDetails, PullRequestFile } from '../../api/backend'
import type { PullRequestTab } from '../../store/editor'
import {
  checkoutPullRequest,
  detailsKey,
  loadDetails,
  openInBrowser,
  openPullRequestFile,
  usePullRequests,
} from '../../store/pullRequests'
import { ROLLUP_GLYPH, checksSummary, countChecks, reviewStateLabel, rollup, stateBadge, threadCounts, timeAgo } from '../../lib/prReview'
import { HtmlBody } from './ReviewThread'
import { ReviewBar } from './ReviewBar'
import { useWindowFocus } from './useWindowFocus'
import { FailedCheckActions, FixWithAgentButton } from './CIStatus'
import type { FixTarget } from '../../store/ci'

const STATUS_LETTER: Record<string, string> = { added: 'A', removed: 'D', modified: 'M', renamed: 'R', copied: 'C', changed: 'T' }
const STATUS_KIND: Record<string, string> = { added: 'added', removed: 'deleted', modified: 'modified', renamed: 'renamed', copied: 'copied' }

/** A pull request: header with branches and CI, description, files, commits, reviews and the review bar. */
export function PullRequestView({ tab, active }: { tab: PullRequestTab; active: boolean }) {
  const key = detailsKey(tab.root, tab.number)
  const entry = usePullRequests((s) => s.details[key])
  const busy = usePullRequests((s) => s.busy[key])

  useEffect(() => {
    if (active && !entry) void loadDetails(tab.root, tab.number)
  }, [active, entry, tab.root, tab.number])
  useWindowFocus(() => void loadDetails(tab.root, tab.number), active)

  const d = entry?.data
  if (!d) {
    return (
      <div className="pr-view">
        {entry?.error ? <div className="placeholder error selectable">{entry.error}</div> : <div className="placeholder">Loading #{tab.number}…</div>}
      </div>
    )
  }
  const badge = stateBadge(d)
  return (
    <div className="pr-view">
      <div className="pr-scroll">
        <div className="pr-header">
          <div className="pr-title-line">
            <h2 className="selectable">
              {d.title} <span className="muted">#{d.number}</span>
            </h2>
            <span className="spacer" />
            <button className="icon" title="Refresh" disabled={entry.loading} onClick={() => void loadDetails(tab.root, tab.number, true)}>↻</button>
            {d.state === 'OPEN' && (
              <button disabled={!!busy} onClick={() => void checkoutPullRequest(tab.root, d.number, d.headRef)} title="gh pr checkout (refused while files have local changes)">
                {busy === 'checkout' ? 'Checking out…' : 'Check out'}
              </button>
            )}
            <button onClick={() => openInBrowser(d.url)}>Open on GitHub ↗</button>
          </div>
          <div className="pr-subtitle muted">
            {badge && <span className={`pr-badge ${badge.tone}`}>{badge.label}</span>}
            <span>
              <b>{d.author}</b> wants to merge {d.commits.length} commit{d.commits.length === 1 ? '' : 's'} into <code>{d.baseRef}</code> from{' '}
              <code>{d.crossRepository && d.headOwner ? `${d.headOwner}:${d.headRef}` : d.headRef}</code>
            </span>
            <span className="pr-stat-add">+{d.additions}</span>
            <span className="pr-stat-del">−{d.deletions}</span>
            <span>updated {timeAgo(d.updatedAt)}</span>
            {entry.loading && <span>· refreshing…</span>}
          </div>
          {d.labels.length > 0 && (
            <div className="pr-labels">
              {d.labels.map((l) => (
                <span key={l} className="pr-badge muted">{l}</span>
              ))}
            </div>
          )}
          {entry.error && <div className="error selectable">{entry.error}</div>}
        </div>

        <Checks root={tab.root} details={d} />

        <section className="pr-section">
          {d.bodyHtml ? <HtmlBody html={d.bodyHtml} className="pr-description" /> : <div className="muted">No description.</div>}
        </section>

        <Files root={tab.root} details={d} />
        <Commits details={d} />
        <Reviews details={d} />
      </div>
      <ReviewBar root={tab.root} details={d} />
    </div>
  )
}

function Checks({ root, details }: { root: string; details: PullRequestDetails }) {
  const checks: PullRequestCheck[] = details.checks
  const [open, setOpen] = useState(false)
  const counts = useMemo(() => countChecks(checks), [checks])
  const state = rollup(counts)
  if (checks.length === 0) return null
  const target: FixTarget = { root, pr: { number: details.number, title: details.title, url: details.url, crossRepository: details.crossRepository }, checks }
  return (
    <section className="pr-section">
      <div className="pr-section-title clickable" onClick={() => setOpen(!open)}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span className={`pr-ci ${state}`}>{ROLLUP_GLYPH[state]}</span>
        <span>Checks</span>
        <span className="muted">{checksSummary(counts)}</span>
        {state === 'failure' && checks.some((c) => c.state === 'failure' && c.runId > 0) && (
          <>
            <span className="spacer" />
            <FixWithAgentButton target={target} />
          </>
        )}
      </div>
      {open &&
        checks.map((c, i) => (
          <div key={`${c.name}:${i}`} className="pr-check" title={[c.status, c.conclusion].filter(Boolean).join(' · ')}>
            <span className={`pr-ci ${c.state}`}>{c.state === 'success' ? '✓' : c.state === 'failure' ? '✗' : c.state === 'pending' ? '●' : '–'}</span>
            <span className="truncate">
              {c.workflow && <span className="muted">{c.workflow} / </span>}
              {c.name}
            </span>
            <span className="spacer" />
            <span className="muted">{(c.conclusion || c.status).toLowerCase().replace(/_/g, ' ')}</span>
            <FailedCheckActions root={root} check={c} target={target} />
            {c.url && (
              <button className="link" onClick={() => openInBrowser(c.url)}>Details ↗</button>
            )}
          </div>
        ))}
    </section>
  )
}

function Files({ root, details }: { root: string; details: PullRequestDetails }) {
  const counts = useMemo(() => threadCounts(details.threads), [details.threads])
  return (
    <section className="pr-section">
      <div className="pr-section-title">
        <span>Files changed</span>
        <span className="badge">{details.files.length}</span>
      </div>
      {details.files.map((f) => (
        <FileRow key={f.path} file={f} threads={counts[f.path]} onOpen={() => openPullRequestFile(root, details.number, f.path)} />
      ))}
    </section>
  )
}

function FileRow({ file, threads, onOpen }: { file: PullRequestFile; threads?: { open: number; resolved: number }; onOpen: () => void }) {
  const slash = file.path.lastIndexOf('/')
  return (
    <div className="pr-file" onClick={onOpen} title={file.previousPath ? `${file.previousPath} → ${file.path}` : file.path}>
      <span className={`change-kind ${STATUS_KIND[file.status] ?? 'modified'}`}>{STATUS_LETTER[file.status] ?? 'M'}</span>
      <span className="file-name">{file.path.slice(slash + 1)}</span>
      <span className="muted path-hint">{slash > 0 ? file.path.slice(0, slash) : ''}</span>
      {threads && threads.open > 0 && <span className="pr-badge pending" title="Unresolved review threads">{threads.open} open</span>}
      {threads && threads.open === 0 && threads.resolved > 0 && <span className="pr-badge muted" title="All review threads resolved">{threads.resolved} resolved</span>}
      <span className="pr-stat-add">+{file.additions}</span>
      <span className="pr-stat-del">−{file.deletions}</span>
    </div>
  )
}

function Commits({ details }: { details: PullRequestDetails }) {
  const [open, setOpen] = useState(false)
  return (
    <section className="pr-section">
      <div className="pr-section-title clickable" onClick={() => setOpen(!open)}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span>Commits</span>
        <span className="badge">{details.commits.length}</span>
      </div>
      {open &&
        details.commits.map((c) => (
          <div key={c.oid} className="pr-commit">
            <code className="muted">{c.oid.slice(0, 7)}</code>
            <span className="truncate selectable">{c.headline}</span>
            <span className="spacer" />
            <span className="muted">{c.author}</span>
            <span className="muted">{timeAgo(c.date)}</span>
          </div>
        ))}
    </section>
  )
}

function Reviews({ details }: { details: PullRequestDetails }) {
  if (details.reviews.length === 0) return null
  return (
    <section className="pr-section">
      <div className="pr-section-title">
        <span>Reviews</span>
        <span className="badge">{details.reviews.length}</span>
      </div>
      {details.reviews.map((r, i) => (
        <div key={i} className="pr-comment">
          <div className="pr-comment-head">
            <b>{r.author}</b>
            <span className={`pr-review-state ${r.state.toLowerCase()}`}>{reviewStateLabel(r.state)}</span>
            <span className="muted">{timeAgo(r.submittedAt)}</span>
          </div>
          {r.bodyHtml && <HtmlBody html={r.bodyHtml} />}
        </div>
      ))}
    </section>
  )
}
