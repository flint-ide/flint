import { Button, ChatBanner } from '@flint-ide/ui'
import { alertText, isPaused } from '../../lib/usageView'
import { alertsKey, continueOverBudget, dismissBudget, useUsage } from '../../store/usage'
import { useWorkspace } from '../../store/workspace'

/**
 * The chat's spent budgets (Settings ▸ Usage): a warning under the header that
 * never blocks typing. With "pause new turns" on, it offers to continue anyway.
 */
export function BudgetWarning({ chatId }: { chatId: string }) {
  const budget = useUsage((s) => s.budgets[chatId])
  const dismissed = useUsage((s) => s.dismissed[chatId])
  if (!budget || budget.alerts.length === 0) return null
  const paused = isPaused(budget.alerts, budget.pause)
  if (!paused && dismissed === alertsKey(budget.alerts)) return null
  return (
    <ChatBanner
      tone={paused ? 'negative' : 'warning'}
      // A paused chat can't be dismissed: the pause has to be lifted or waited out.
      onDismiss={paused ? undefined : () => dismissBudget(chatId)}
      actions={
        <>
          {paused && (
            <Button variant="plain" size="sm" onClick={() => void continueOverBudget(chatId)} title="Let turns run today (and in this task) although the budget is spent">
              Continue anyway
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={() => useWorkspace.getState().setSidebar('usage')}>
            Usage
          </Button>
        </>
      }
    >
      {budget.alerts.map((a) => alertText(a)).join(' · ')}
      {paused && ' — new turns are paused.'}
    </ChatBanner>
  )
}
