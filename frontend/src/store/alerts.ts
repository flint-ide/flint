import * as AlertsAPI from '../../bindings/flint/alertsapi'
import { EventsOn } from '../wails/runtime'
import { useSettings } from './settings'
import { useEditor } from './editor'
import type { PermissionRequest } from '../lib/agentReducer'
import {
  approvalKey,
  approvalNotice,
  badgeCount,
  emptyApprovals,
  focused,
  opened,
  settled,
  settledSession,
  type Approvals,
} from '../lib/approvalAlerts'

/** Things that finished since the window last had focus (the Dock badge). */
let missed = 0
/** Permission requests announced and still unanswered (they count in the badge too). */
let approvals: Approvals = emptyApprovals

const showBadge = () => AlertsAPI.SetBadge(badgeCount(missed, approvals)).catch(() => {})
const inBackground = () => !document.hasFocus() && useSettings.getState().settings.notifyInBackground

/**
 * Tells about work that finished while Flint was in the background: a macOS
 * notification (clicking it shows tab) and a count on the Dock icon. Does
 * nothing while the window has focus, or when turned off in Settings.
 */
export function alertDone(title: string, body: string, tab: string): void {
  if (!inBackground()) return
  missed++
  showBadge()
  AlertsAPI.Notify(title, body, tab).catch(() => {}) // no bundle in dev builds, or not allowed
}

/**
 * Tells that an agent waits for the user's approval (once per request), when
 * Flint is in the background and Settings ▸ Agents allows it. Clicking the
 * notification shows the chat; the request counts in the badge until settled.
 */
export function alertApproval(sessionId: string, agent: string, cwd: string, req: PermissionRequest): void {
  const background = inBackground() && useSettings.getState().settings.notifyOnApproval
  const next = opened(approvals, approvalKey(sessionId, req.id), background)
  approvals = next.state
  if (!next.notify) return
  const { title, body } = approvalNotice(agent, cwd, req)
  showBadge()
  AlertsAPI.Notify(title, body, `chat:${sessionId}`).catch(() => {})
}

/** A request was answered or expired: it leaves the badge. */
export function approvalSettled(sessionId: string, requestId: string): void {
  const before = badgeCount(missed, approvals)
  approvals = settled(approvals, approvalKey(sessionId, requestId))
  if (badgeCount(missed, approvals) !== before) showBadge()
}

/** A turn ended: requests it left open are gone. */
export function approvalsEnded(sessionId: string): void {
  const before = badgeCount(missed, approvals)
  approvals = settledSession(approvals, sessionId)
  if (badgeCount(missed, approvals) !== before) showBadge()
}

let subscribed = false
export function subscribeAlerts(): void {
  if (subscribed) return
  subscribed = true
  window.addEventListener('focus', () => {
    if (badgeCount(missed, approvals) === 0) return
    missed = 0
    approvals = focused(approvals)
    showBadge()
  })
  EventsOn('notification:open', (tab: string) => {
    if (tab && useEditor.getState().tabs.some((t) => t.id === tab)) useEditor.getState().setActive(tab)
  })
}
