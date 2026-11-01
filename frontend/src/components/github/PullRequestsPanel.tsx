import { useEffect } from 'react'
import { useGit } from '../../store/git'
import { useWorkspace } from '../../store/workspace'
import { pullRequestTabId, useEditor } from '../../store/editor'
import { loadPullRequests, openInBrowser, openPullRequest, setListFilter, usePullRequests, type ListState, type Who } from '../../store/pullRequests'
import { ROLLUP_GLYPH, checksSummary, rollup, stateBadge, timeAgo } from '../../lib/prReview'
import type { PullRequest } from '../../api/backend'
import { Select } from '../ui/Select'
import { useWindowFocus } from './useWindowFocus'
import { CIBanner } from './CIStatus'
import { useCurrentCI } from '../../store/ci'

const WHO: { value: Who; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'mine', label: 'Created by me' },
  { value: 'review', label: 'Review requested' },
]

const STATES: { value: ListState; label: string }[] = [
  { value: 'open', label: 'Open' },
  { value: 'closed', label: 'Closed' },
  { value: 'merged', label: 'Merged' },
  { value: 'all', label: 'All states' },
]

/** Pull requests of the active project's GitHub repository (gh pr list). */
export function PullRequestsPanel() {
  const primary = useWorkspace((s) => s.roots[0] ?? null)
  // The project the Commit panel follows (the active tab's), else the primary one.
  const root = useGit((s) => s.folder) ?? primary
  const status = usePullRequests((s) => s.status)
  const items = usePullRequests((s) => s.items)
  const loading = usePullRequests((s) => s.loading)
  const error = usePullRequests((s) => s.error)
  const who = usePullRequests((s) => s.who)
  const state = usePullRequests((s) => s.state)
  const ci = useCurrentCI()

  // Loaded when shown and on window focus; the panel unmounts when hidden, so nothing polls.
  useEffect(() => void loadPullRequests(root), [root])
  useWindowFocus(() => void loadPullRequests(root))

  const repo = status?.state === 'ok' ? status.repo : null

  return (
    <div className="panel pr-panel">
      <div className="panel-header">
        <span className="panel-title" title={repo?.url}>
          Pull Requests{repo && <span className="pr-repo"> · {repo.nameWithOwner}</span>}
        </span>
        {repo && (
          <button className="icon" title="Open on GitHub" onClick={() => openInBrowser(`${repo.url}/pulls`)}>↗</button>
        )}
        <button className="icon" title="Refresh" disabled={loading} onClick={() => void loadPullRequests(root, true)}>↻</button>
      </div>
      {repo && (
        <div className="pr-filters">
          <Select size="sm" value={who} options={WHO} onChange={(v) => setListFilter({ who: v })} title="Whose pull requests" />
          <Select size="sm" value={state} options={STATES} onChange={(v) => setListFilter({ state: v })} title="State" />
          {loading && <span className="muted pr-loading">Loading…</span>}
        </div>
      )}
      {repo && <CIBanner entry={ci} />}
      <div className="tree scroll">
        {!root && <div className="placeholder muted">Open a folder to see its pull requests.</div>}
        {status && status.state !== 'ok' && <StatusMessage state={status.state} message={status.message} onRetry={() => void loadPullRequests(root, true)} />}
        {error && <div className="placeholder error selectable">{error}</div>}
        {repo && !error && !loading && items.length === 0 && <div className="placeholder muted">No pull requests.</div>}
        {repo && root && items.map((pr) => <PullRequestRow key={pr.number} root={root} pr={pr} />)}
      </div>
    </div>
  )
}

function StatusMessage({ state, message, onRetry }: { state: string; message: string; onRetry: () => void }) {
  const hint =
    state === 'missing'
      ? 'Pull requests are read and reviewed through the GitHub CLI.'
      : state === 'unauthenticated'
        ? 'Flint never stores tokens: gh logs in once, in a terminal.'
        : null
  return (
    <div className="placeholder">
      <span className={state === 'error' ? 'error selectable' : 'selectable'}>{message}</span>
      {hint && <span className="muted">{hint}</span>}
      <button className="link" onClick={onRetry}>Retry</button>
    </div>
  )
}

function PullRequestRow({ root, pr }: { root: string; pr: PullRequest }) {
  const active = useEditor((s) => s.activeId === pullRequestTabId(root, pr.number))
  const ci = rollup(pr.checks)
  const badge = stateBadge(pr)
  return (
    <div className={`pr-row${active ? ' active' : ''}`} onClick={() => openPullRequest(root, pr)} title={`${pr.title}\n${pr.headRef} → ${pr.baseRef}`}>
      <div className="pr-row-title">
        {ci !== 'none' && (
          <span className={`pr-ci ${ci}`} title={checksSummary(pr.checks)}>
            {ROLLUP_GLYPH[ci]}
          </span>
        )}
        <span className="pr-title-text">{pr.title}</span>
      </div>
      <div className="pr-row-meta muted">
        <span>#{pr.number}</span>
        <span>{pr.author}</span>
        <span className="pr-branch">{pr.headRef}</span>
        <span className="spacer" />
        {badge && <span className={`pr-badge ${badge.tone}`}>{badge.label}</span>}
        <span>{timeAgo(pr.updatedAt)}</span>
      </div>
    </div>
  )
}
