import type { github } from '../../wails/models'
import { checkName, failedRuns } from '../../lib/ci'
import { fixCIWithAgent, openCILog, rerunFailedJobs, useCI, type CIEntry, type FixTarget } from '../../store/ci'
import { openPullRequest } from '../../store/pullRequests'
import { checksSummary, ROLLUP_GLYPH } from '../../lib/prReview'

/** "✦ Fix with agent" for a target (all its failed runs, or one run / job). */
export function FixWithAgentButton({ target, only, label = '✦ Fix with agent' }: { target: FixTarget; only?: { runId: number; jobId: number }; label?: string }) {
  const busy = useCI((s) => !!s.busy[`${target.root}#fix`])
  return (
    <button
      className="link"
      disabled={busy}
      title="Read the failed jobs' logs and put the failure in the agent chat for you to review and send"
      onClick={(e) => {
        e.stopPropagation()
        void fixCIWithAgent(target, only)
      }}
    >
      {busy ? 'Reading the log…' : label}
    </button>
  )
}

/** Fix with agent, Open log and Rerun failed jobs for one failed check. */
export function FailedCheckActions({ root, check, target }: { root: string; check: github.Check; target: FixTarget }) {
  if (check.state !== 'failure' || !check.runId) return null
  const label = checkName(check)
  return (
    <span className="ci-actions" onClick={(e) => e.stopPropagation()}>
      <FixWithAgentButton target={target} only={{ runId: check.runId, jobId: check.jobId }} label="✦ Fix" />
      <button className="link" title="Read the failed steps' log in a tab" onClick={() => openCILog(root, check.runId, check.jobId, label)}>Log</button>
      <button className="link" title="Re-run the failed jobs of this run on GitHub (asks first)" onClick={() => void rerunFailedJobs(root, check.runId, check.workflow || label)}>
        Rerun
      </button>
    </span>
  )
}

/** The current branch's pull request CI, on top of the Pull Requests view. */
export function CIBanner({ entry }: { entry: CIEntry | null }) {
  const pr = entry?.data?.pr
  if (!entry || !pr || entry.state === 'none') return null
  const failed = entry.data!.checks.filter((c) => c.state === 'failure')
  const runs = failedRuns(entry.data!.checks)
  const target: FixTarget = { root: entry.root, pr, checks: entry.data!.checks }
  return (
    <div className={`ci-banner ${entry.state}`}>
      <div className="ci-banner-head" onClick={() => openPullRequest(entry.root, pr)} title={`${pr.title}\n${checksSummary(entry.data!.counts)}`}>
        <span className={`pr-ci ${entry.state}`}>{ROLLUP_GLYPH[entry.state]}</span>
        <span className="truncate">
          Your branch: #{pr.number} · {entry.state === 'failure' ? 'CI failing' : entry.state === 'pending' ? 'CI running' : 'CI passed'}
        </span>
      </div>
      {failed.length > 0 && (
        <>
          {failed.slice(0, 4).map((c, i) => (
            <div key={`${c.name}:${i}`} className="ci-banner-check">
              <span className="truncate muted" title={c.url}>{checkName(c)}</span>
              <span className="spacer" />
              <FailedCheckActions root={entry.root} check={c} target={target} />
            </div>
          ))}
          {runs.length > 0 && (
            <div className="ci-banner-actions">
              <FixWithAgentButton target={target} />
            </div>
          )}
        </>
      )}
    </div>
  )
}
