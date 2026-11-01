import type { TaskView } from '../../lib/agentTasks'
import { checkName } from '../../lib/ci'
import { checksSummary } from '../../lib/prReview'
import { ciKey, useCI } from '../../store/ci'
import { FixWithAgentButton } from '../github/CIStatus'

const LABEL = { failure: '✗ CI', pending: '● CI', success: '✓ CI' } as const

function useTaskCI(t: TaskView) {
  return useCI((s) => (t.pullRequest && !t.merged ? (s.entries[ciKey(t.worktree, t.branch)] ?? null) : null))
}

/** The CI state of a task's pull request, in its row. */
export function TaskCI({ t }: { t: TaskView }) {
  const entry = useTaskCI(t)
  if (!entry?.data?.pr || entry.state === 'none') return null
  const failed = entry.data.checks.filter((c) => c.state === 'failure').map(checkName)
  return (
    <span className={`pr-ci ${entry.state}`} title={[checksSummary(entry.data.counts), ...failed.map((n) => `✗ ${n}`)].join('\n')}>
      {LABEL[entry.state]}
    </span>
  )
}

/** Fix with agent for a task whose pull request fails CI: the draft goes to the task's chat. */
export function TaskCIFix({ t }: { t: TaskView }) {
  const entry = useTaskCI(t)
  const pr = entry?.data?.pr
  if (!entry || !pr || entry.state !== 'failure') return null
  return <FixWithAgentButton target={{ root: t.worktree, pr, checks: entry.data!.checks, task: t }} label="✦ Fix CI" />
}
