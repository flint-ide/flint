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
    <div className={`budget-warning${paused ? ' paused' : ''}`} role="status">
      <span className="budget-icon" aria-hidden>⚠</span>
      <span className="budget-text">
        {budget.alerts.map((a) => alertText(a)).join(' · ')}
        {paused && ' — new turns are paused.'}
      </span>
      {paused && (
        <button className="link" onClick={() => void continueOverBudget(chatId)} title="Let turns run today (and in this task) although the budget is spent">
          Continue anyway
        </button>
      )}
      <button className="link" onClick={() => useWorkspace.getState().setSidebar('usage')}>Usage</button>
      {!paused && <button className="icon" aria-label="Dismiss" onClick={() => dismissBudget(chatId)}>✕</button>}
    </div>
  )
}
