// Budget warnings of the agent chats and the change signal of the Usage panel.
// Usage is recorded in Go (UsageAPI); this store only follows its events:
// "usage:changed" after every recorded turn, "usage:alert" once per budget.
import { createStore } from '../lib/store'
import * as UsageAPI from '../../bindings/flint/usageapi'
import { EventsOn } from '../wails/runtime'
import { errorMessage } from '../api/backend'
import { alertText, type BudgetAlert } from '../lib/usageView'
import { chatLocalId, chatRemoteId, chatTabId, useAgentChat } from './agentChat'
import { alertDone } from './alerts'
import { showToast } from './dialog'
import { useSettings } from './settings'

export interface ChatBudget {
  alerts: BudgetAlert[]
  pause: boolean
}

interface UsageState {
  /** Bumped when usage was recorded or a budget overridden: the panel reloads. */
  version: number
  /** Spent budgets per chat (local chat id). */
  budgets: Record<string, ChatBudget>
  /** Chats whose warning the user closed, with the alerts it showed then. */
  dismissed: Record<string, string>
}

export const useUsage = createStore<UsageState>(() => ({ version: 0, budgets: {}, dismissed: {} }))

/** Identifies a set of alerts, so a closed warning comes back when a new budget is spent. */
export const alertsKey = (alerts: BudgetAlert[]): string =>
  alerts.map((a) => `${a.scope}/${a.key}/${a.kind}/${a.limit}/${a.overridden}`).join('|')

/** Reads the spent budgets of a chat. */
export async function refreshBudget(chatId: string): Promise<void> {
  try {
    const b = await UsageAPI.Budget(chatRemoteId(chatId))
    const next: ChatBudget = { alerts: (b.alerts ?? []) as BudgetAlert[], pause: b.pause }
    useUsage.setState((s) => {
      const cur = s.budgets[chatId]
      if (cur && cur.pause === next.pause && alertsKey(cur.alerts) === alertsKey(next.alerts)) return {}
      return { budgets: { ...s.budgets, [chatId]: next } }
    })
  } catch {
    // The warning is a convenience: a failed read leaves the last one.
  }
}

function refreshAll(): void {
  for (const id of useAgentChat.getState().order) void refreshBudget(id)
}

export function dismissBudget(chatId: string): void {
  const alerts = useUsage.getState().budgets[chatId]?.alerts ?? []
  useUsage.setState((s) => ({ dismissed: { ...s.dismissed, [chatId]: alertsKey(alerts) } }))
}

/** "Continue anyway": new turns run although the budget is spent. */
export async function continueOverBudget(chatId: string): Promise<void> {
  try {
    await UsageAPI.Override(chatRemoteId(chatId))
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
  await refreshBudget(chatId)
}

interface UsageAlertEvent {
  sessionId: string
  alert: BudgetAlert
  taskTitle?: string
}

const REFRESH_DELAY_MS = 250
let refreshTimer: ReturnType<typeof setTimeout> | undefined

let subscribed = false
export function subscribeUsageEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('usage:changed', () => {
    clearTimeout(refreshTimer)
    refreshTimer = setTimeout(() => {
      useUsage.setState((s) => ({ version: s.version + 1 }))
      refreshAll()
    }, REFRESH_DELAY_MS)
  })
  EventsOn('usage:alert', (e: UsageAlertEvent) => {
    const chatId = chatLocalId(e.sessionId)
    const text = alertText(e.alert, e.taskTitle)
    void refreshBudget(chatId)
    if (document.hasFocus()) showToast(text)
    else alertDone('Agent budget', text, chatTabId(chatId))
  })
  // New budgets or prices change the warnings and the panel's estimates.
  useSettings.subscribe((s, prev) => {
    if (s.settings.usage === prev.settings.usage) return
    useUsage.setState((u) => ({ version: u.version + 1 }))
    refreshAll()
  })
  // A chat opened later reads its warning when it appears.
  useAgentChat.subscribe((s, prev) => {
    if (s.order === prev.order) return
    for (const id of s.order) if (!prev.order.includes(id)) void refreshBudget(id)
  })
}
