import { useEffect, useMemo, type MouseEvent } from 'react'
import { useGit } from '../../store/git'
import { useWorkspace } from '../../store/workspace'
import { pullRequestTabId, useEditor } from '../../store/editor'
import {
  checkoutPullRequest,
  loadPullRequests,
  openInBrowser,
  openPullRequest,
  setListFilter,
  setListQuery,
  usePullRequests,
  type ListState,
  type Who,
} from '../../store/pullRequests'
import { addressReviewComments, requestCodeReview } from '../../store/prAgent'
import { openContextMenu } from '../../store/contextMenu'
import { showToast } from '../../store/dialog'
import { pullRequestMenu } from '../../lib/prMenu'
import { ROLLUP_GLYPH, checksSummary, rollup, searchPullRequests, stateBadge, timeAgo } from '../../lib/prReview'
import { errorMessage, type PullRequest } from '../../api/backend'
import { Select } from '../ui/Select'
import { useWindowFocus } from './useWindowFocus'
import { CIBanner } from './CIStatus'
import { useCurrentCI } from '../../store/ci'
import { onAccountsChanged } from '../../store/accounts'
import { SignInWithGitHub } from '../accounts/SignInWithGitHub'
import { Button, IconButton, Input, ListItem, PanelHeader } from '@flint-ide/ui'
import { ExternalLink, RefreshCw } from '@flint-ide/ui/icons'
import { PrBadge } from './PrBadge'

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

/** Pull requests of the active project's GitHub repository (GitHub's API, internal/github). */
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
  const query = usePullRequests((s) => s.query)
  const shown = useMemo(() => searchPullRequests(items, query), [items, query])
  const ci = useCurrentCI()

  // Loaded when shown and on window focus; the panel unmounts when hidden, so nothing polls.
  useEffect(() => void loadPullRequests(root), [root])
  useWindowFocus(() => void loadPullRequests(root))
  useEffect(() => onAccountsChanged(() => void loadPullRequests(root, true)), [root])

  const repo = status?.state === 'ok' ? status.repo : null

  return (
    <div className="panel pr-panel">
      <PanelHeader
        title="Pull Requests"
        tooltip={repo?.url}
        actions={
          <>
            {repo && (
              <IconButton size="sm" variant="ghost" aria-label="Open on GitHub" title="Open on GitHub" onClick={() => openInBrowser(`${repo.url}/pulls`)}><ExternalLink /></IconButton>
            )}
            <IconButton size="sm" variant="ghost" aria-label="Refresh" title="Refresh" disabled={loading} onClick={() => void loadPullRequests(root, true)}><RefreshCw /></IconButton>
          </>
        }
      >
        {repo?.nameWithOwner}
      </PanelHeader>
      {repo && (
        <div className="pr-filters">
          <Select size="sm" value={who} options={WHO} onChange={(v) => setListFilter({ who: v })} title="Whose pull requests" />
          <Select size="sm" value={state} options={STATES} onChange={(v) => setListFilter({ state: v })} title="State" />
          {loading && <span className="muted pr-loading">Loading…</span>}
        </div>
      )}
      {repo && (
        <div className="pr-search">
          <Input
            type="search"
            className="pr-search-input"
            placeholder="Search by title or #number"
            aria-label="Search pull requests by title or number"
            value={query}
            spellCheck={false}
            onChange={(e) => setListQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape' && query) {
                e.stopPropagation()
                setListQuery('')
              }
            }}
          />
          {query.trim() !== '' && (
            <span className="muted pr-loading">
              {shown.length} of {items.length}
            </span>
          )}
        </div>
      )}
      {repo && <CIBanner entry={ci} />}
      <div className="tree scroll">
        {!root && <div className="placeholder muted">Open a folder to see its pull requests.</div>}
        {status && status.state !== 'ok' && <StatusMessage state={status.state} message={status.message} onRetry={() => void loadPullRequests(root, true)} />}
        {error && <div className="placeholder error selectable">{error}</div>}
        {repo && !error && !loading && items.length === 0 && <div className="placeholder muted">No pull requests.</div>}
        {repo && !error && items.length > 0 && shown.length === 0 && (
          <div className="placeholder muted">
            No pull request in this list matches “{query.trim()}”. The search covers the pull requests loaded here: change the filters above to look in others.
          </div>
        )}
        {repo && root && shown.map((pr) => <PullRequestRow key={pr.number} root={root} pr={pr} />)}
      </div>
    </div>
  )
}

function StatusMessage({ state, message, onRetry }: { state: string; message: string; onRetry: () => void }) {
  if (state === 'unauthenticated') {
    return (
      <div className="placeholder">
        <SignInWithGitHub intro="Sign in to GitHub to see and review this repository's pull requests. Flint keeps the token in the system keyring." />
        <Button size="sm" variant="ghost" onClick={onRetry}>Retry</Button>
      </div>
    )
  }
  return (
    <div className="placeholder">
      <span className={state === 'error' ? 'error selectable' : 'selectable'}>{message}</span>
      <Button size="sm" variant="ghost" onClick={onRetry}>Retry</Button>
    </div>
  )
}

/** Copies text for a "Copy …" item of the row's menu and confirms it in a toast. */
function copyText(text: string): void {
  navigator.clipboard.writeText(text).then(
    () => showToast(`Copied ${text}`),
    (err: unknown) => showToast(`Copy failed: ${errorMessage(err)}`, 'error'),
  )
}

function PullRequestRow({ root, pr }: { root: string; pr: PullRequest }) {
  const active = useEditor((s) => s.activeId === pullRequestTabId(root, pr.number))
  const ci = rollup(pr.checks)
  const badge = stateBadge(pr)
  // The tab's header actions, without opening the tab first.
  const onContextMenu = (e: MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const target = { root, number: pr.number }
    openContextMenu(
      e.clientX,
      e.clientY,
      pullRequestMenu(pr, {
        open: () => openPullRequest(root, pr),
        review: (send) => void requestCodeReview(target, { send }),
        address: (send) => void addressReviewComments(target, { send }),
        checkout: () => void checkoutPullRequest(root, pr.number, pr.headRef),
        openOnGitHub: () => openInBrowser(pr.url),
        copy: copyText,
      }),
    )
  }
  return (
    <ListItem
      active={active}
      onClick={() => openPullRequest(root, pr)}
      onContextMenu={onContextMenu}
      title={`${pr.title}\n${pr.headRef} → ${pr.baseRef}`}
      leading={
        ci !== 'none' && (
          <span className={`pr-ci ${ci}`} title={checksSummary(pr.checks)}>
            {ROLLUP_GLYPH[ci]}
          </span>
        )
      }
      label={pr.title}
      trailing={badge && <PrBadge tone={badge.tone}>{badge.label}</PrBadge>}
      meta={[`#${pr.number}`, pr.author, <span className="fui-mono">{pr.headRef}</span>, timeAgo(pr.updatedAt)]}
    />
  )
}
