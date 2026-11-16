// Commands of the pull request → agent hand-off (store/prAgent.ts).
import { registerCommands } from '../lib/commands'
import { addressReviewComments, commandReviewTarget } from '../store/prAgent'

export function registerPullRequestCommands(): () => void {
  return registerCommands([
    {
      id: 'pr.addressReviewComments',
      title: 'Address Review Comments with Agent',
      category: 'Agent',
      keywords: 'pull request pr github review feedback threads changes requested fix claude codex',
      // The pull request of the active tab, else the current branch's open one.
      when: () => commandReviewTarget() !== null,
      run: () => {
        const target = commandReviewTarget()
        if (target) return addressReviewComments(target)
      },
    },
  ])
}
