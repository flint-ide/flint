// Commands of Review Changes with Agent (store/agentReview.ts).
import { registerCommands } from '../lib/commands'
import { useEditor } from '../store/editor'
import { useGit } from '../store/git'
import { cancelReview, clearReview, fixAllFindings, reviewChanges, useAgentReview } from '../store/agentReview'

const review = () => useAgentReview.getState()

/** The file of the active working-tree diff tab, reviewed even when it isn't ticked. */
function activeDiffPath(): string | undefined {
  const { tabs, activeId } = useEditor.getState()
  const tab = tabs.find((t) => t.id === activeId)
  return tab?.kind === 'diff' && !tab.rev && !tab.baseRev ? tab.path : undefined
}

export function registerReviewCommands(): () => void {
  return registerCommands([
    {
      id: 'agent.reviewChanges',
      title: 'Review Changes with Agent',
      category: 'Agent',
      keywords: 'code review uncommitted diff findings claude codex before commit',
      when: () => useGit.getState().isRepo && !review().running,
      run: () => reviewChanges(activeDiffPath()),
    },
    { id: 'agent.cancelReview', title: 'Cancel Agent Review', category: 'Agent', keywords: 'stop', when: () => !!review().running, run: cancelReview },
    {
      id: 'agent.fixAllReviewFindings',
      title: 'Fix All Review Findings with Agent',
      category: 'Agent',
      keywords: 'review findings chat',
      when: () => review().findings.length > 0,
      run: fixAllFindings,
    },
    { id: 'agent.clearReview', title: 'Clear Agent Review', category: 'Agent', keywords: 'dismiss findings', when: () => review().doneAt > 0, run: clearReview },
  ])
}
