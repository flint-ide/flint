// Hands a pull request's review comments to the agent: one thread ("Ask
// agent") or everything unresolved ("Address review comments"). Go reads the
// pull request (its cache when it was just loaded) and builds the draft with
// reviewers' text quoted, redacted and bounded; here it lands in the chat of
// the project the pull request is checked out in (an agent task's own chat
// and worktree) for the user to review and send. Nothing polls.
import { createStore } from '../lib/store'
import { GitHub, errorMessage } from '../api/backend'
import { askAgent } from '../lib/askAgent'
import { draftNotice, isNothingToAddress, maySend, pickReviewTarget, type ReviewTarget } from '../lib/prAgent'
import { isSignInError } from '../lib/githubSettings'
import type { TaskView } from '../lib/agentTasks'
import { requestGitHubSignIn } from './accounts'
import { showFolder } from './agentTasks'
import { ciKey, useCI } from './ci'
import { showToast } from './dialog'
import { useEditor } from './editor'
import { useGit } from './git'

export interface ReviewAgentTarget extends ReviewTarget {
  /** An agent task: its chat and worktree get the draft. */
  task?: TaskView
}

interface ReviewAgentStore {
  /** Drafts being built, by busyKey. */
  busy: Record<string, true>
}

export const useReviewAgent = createStore<ReviewAgentStore>(() => ({ busy: {} }))

/** The key of a draft in the making: a thread's, or the whole pull request's. */
export const busyKey = (root: string, number: number, threadId = '') => `${root}#${number}#${threadId}`

/**
 * Puts the review feedback of a pull request in the agent chat: threadId
 * narrows it to one thread. send (⌥-click) sends it right away, unless the
 * pull request's branch isn't the one checked out: that draft says so and
 * waits for the user.
 */
export async function addressReviewComments(target: ReviewAgentTarget, opts: { threadId?: string; send?: boolean } = {}): Promise<void> {
  const key = busyKey(target.root, target.number, opts.threadId)
  if (useReviewAgent.getState().busy[key]) return
  useReviewAgent.setState((s) => ({ busy: { ...s.busy, [key]: true } }))
  try {
    const draft = await GitHub.ReviewDraft(target.root, target.number, opts.threadId ?? '')
    if (target.task && !(await showFolder(target.task.worktree))) return
    const chat = await askAgent({ prompt: draft.prompt, root: target.root, send: maySend(draft, !!opts.send) })
    const notice = chat ? draftNotice(draft) : null
    if (notice) showToast(notice.message, notice.kind)
  } catch (err) {
    const msg = errorMessage(err)
    if (isSignInError(msg)) requestGitHubSignIn()
    else if (isNothingToAddress(msg)) showToast(`No unresolved review comments on #${target.number}`)
    else showToast(`Couldn't read the review comments: ${msg}`, 'error')
  } finally {
    useReviewAgent.setState((s) => {
      const { [key]: _done, ...busy } = s.busy
      return { busy }
    })
  }
}

/** The pull request the palette command works on: the active tab's, else the current branch's. */
export function commandReviewTarget(): ReviewTarget | null {
  const { tabs, activeId } = useEditor.getState()
  const { root, status } = useGit.getState()
  const entry = root && status?.branch ? useCI.getState().entries[ciKey(root, status.branch)] : undefined
  return pickReviewTarget(
    tabs.find((t) => t.id === activeId),
    entry ? { root: entry.root, pr: entry.data?.pr } : null,
  )
}
