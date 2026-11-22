import type { TaskView } from '../../lib/agentTasks'
import { checkName } from '../../lib/ci'
import { checksSummary } from '../../lib/prReview'
import { ciKey, useCI } from '../../store/ci'
import { FixWithAgentButton } from '../github/CIStatus'
import { AddressReviewButton } from '../github/ReviewAgent'
import type { TaskCardCI } from '@flint-ide/ui'

function useTaskCI(t: TaskView) {
  return useCI((s) => (t.pullRequest && !t.merged ? (s.entries[ciKey(t.worktree, t.branch)] ?? null) : null))
}

/** The CI state of a task's pull request, for its card's processing badge; null without a pull request or checks. */
export function useTaskCIBadge(t: TaskView): TaskCardCI | null {
  const entry = useTaskCI(t)
  if (!entry?.data?.pr || entry.state === 'none') return null
  const failed = entry.data.checks.filter((c) => c.state === 'failure').map(checkName)
  return { state: entry.state, title: [checksSummary(entry.data.counts), ...failed.map((n) => `✗ ${n}`)].join('\n') }
}

/** Fix with agent for a task whose pull request fails CI: the draft goes to the task's chat. */
export function TaskCIFix({ t }: { t: TaskView }) {
  const entry = useTaskCI(t)
  const pr = entry?.data?.pr
  if (!entry || !pr || entry.state !== 'failure') return null
  return <FixWithAgentButton target={{ root: t.worktree, pr, checks: entry.data!.checks, task: t }} label="✦ Fix CI" />
}

/** Address review comments for a task with an open pull request: the draft goes to the task's chat. */
export function TaskReviewComments({ t }: { t: TaskView }) {
  const pr = useTaskCI(t)?.data?.pr
  if (!pr || pr.state !== 'OPEN') return null
  return <AddressReviewButton target={{ root: t.worktree, number: pr.number, task: t }} label="✦ Review comments" />
}
