import { addressReviewComments, busyKey, requestCodeReview, useReviewAgent, type ReviewAgentTarget } from '../../store/prAgent'
import { detailsKey, usePullRequests } from '../../store/pullRequests'
import { reviewHint } from '../../lib/prAgent'
import { modifierName } from '../../lib/platform'

const sendHint = () => `${modifierName('Alt')}-click sends it right away`

/** "✦ Address review comments": all unresolved threads and change requests of a pull request, as an agent chat draft. */
export function AddressReviewButton({ target, label = '✦ Address review comments', className = 'link' }: { target: ReviewAgentTarget; label?: string; className?: string }) {
  const busy = useReviewAgent((s) => !!s.busy[busyKey(target.root, target.number)])
  return (
    <button
      className={className}
      disabled={busy}
      title={`Put the unresolved review comments in the agent chat for you to review and send (${sendHint()})`}
      onClick={(e) => {
        e.stopPropagation()
        void addressReviewComments(target, { send: e.altKey })
      }}
    >
      {busy ? 'Reading comments…' : label}
    </button>
  )
}

/** "✦ Review with agent": asks the agent for a code review of the pull request, as an agent chat draft. */
export function RequestCodeReviewButton({ root, number }: { root: string; number: number }) {
  return (
    <button
      title={`Ask the agent for a code review of this pull request: the request goes to the agent chat for you to review and send (${sendHint()}). The agent reads the change and reports findings; it changes no files and posts nothing to GitHub.`}
      onClick={(e) => void requestCodeReview({ root, number }, { send: e.altKey })}
    >
      ✦ Review with agent
    </button>
  )
}

/** "✦ Ask agent" on one review thread. */
export function AskAgentAboutThread({ root, number, threadId }: { root: string; number: number; threadId: string }) {
  const busy = useReviewAgent((s) => !!s.busy[busyKey(root, number, threadId)])
  return (
    <button
      className="link"
      disabled={busy}
      title={`Put this thread and the code it is about in the agent chat for you to review and send (${sendHint()})`}
      onClick={(e) => void addressReviewComments({ root, number }, { threadId, send: e.altKey })}
    >
      ✦ Ask agent
    </button>
  )
}

/**
 * What is already known about review feedback on pull request number: from
 * its loaded details (a tab was opened), else from its row in the list.
 * Nothing is fetched for it.
 */
export function useReviewHint(root: string | undefined, number: number | undefined): string | null {
  return usePullRequests((s) => {
    if (!root || !number) return null
    const details = s.details[detailsKey(root, number)]?.data ?? (s.root ? s.details[detailsKey(s.root, number)]?.data : null)
    return reviewHint(details, s.items.find((i) => i.number === number)?.reviewDecision)
  })
}
