import { useEffect } from 'react'
import { checkName } from '../../lib/ci'
import { checksSummary } from '../../lib/prReview'
import { fixCIWithAgent, followCI, useCI, useCurrentCI } from '../../store/ci'
import { openPullRequest } from '../../store/pullRequests'
import { requestGitHubSignIn } from '../../store/accounts'
import { isSignInError } from '../../lib/githubSettings'

const GLYPH = { failure: '✗', pending: '●', success: '✓' } as const

/** The current branch's pull request CI: a red badge with Fix with agent when it fails. */
export function CIWidget() {
  useEffect(() => followCI(), [])
  const entry = useCurrentCI()
  const busy = useCI((s) => (entry ? !!s.busy[`${entry.root}#fix`] : false))
  const pr = entry?.data?.pr
  if (entry && !pr && isSignInError(entry.error)) {
    return (
      <button className="status-ci none" title="Sign in to GitHub to follow this branch's pull request checks" onClick={requestGitHubSignIn}>
        CI: Sign in
      </button>
    )
  }
  if (!entry || !pr || entry.state === 'none') return null
  const data = entry.data!
  const failed = data.checks.filter((c) => c.state === 'failure').map(checkName)
  const title = [`#${pr.number} ${pr.title}`, checksSummary(data.counts), ...failed.map((n) => `✗ ${n}`), 'Click to open the pull request'].join('\n')
  return (
    <span className="status-ci-group">
      <button className={`status-ci ${entry.state}`} title={title} onClick={() => openPullRequest(entry.root, pr)}>
        {GLYPH[entry.state]} CI
      </button>
      {entry.state === 'failure' && (
        <button
          className="status-action"
          disabled={busy}
          title="Read the failed jobs' logs and put the failure in the agent chat for you to review and send"
          onClick={() => void fixCIWithAgent({ root: entry.root, pr, checks: data.checks })}
        >
          {busy ? 'Reading the log…' : 'Fix with agent'}
        </button>
      )}
    </span>
  )
}
