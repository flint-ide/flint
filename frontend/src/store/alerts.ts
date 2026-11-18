// Telling the user what needs them: desktop notifications, the Dock badge and
// the in-app notice, all read from one model (lib/attention.ts) that this
// store feeds with chat, terminal, run, CI, project and Go events. It runs
// for the whole session, whatever panel is open.
import * as AlertsAPI from '../../bindings/flint/alertsapi'
import { EventsOn } from '../wails/runtime'
import { createStore } from '../lib/store'
import { useSettings } from './settings'
import { useEditor } from './editor'
import { showToast } from './dialog'
import {
  badgeCount,
  cameBack,
  emptyAttention,
  needClosed,
  needOpened,
  needsOfTabClosed,
  terminalSignals,
  wentToBackground,
  workFinished,
  type Attention,
  type ChatSignal,
  type Notice,
  type NotifyPrefs,
  type Step,
  type TerminalInfo,
} from '../lib/attention'

interface AlertsState {
  attention: Attention
  /** Needs whose in-app notice the user closed. */
  dismissed: ReadonlySet<string>
}

export const useAlerts = createStore<AlertsState>(() => ({ attention: emptyAttention, dismissed: new Set() }))

const prefs = (): NotifyPrefs => {
  const s = useSettings.getState().settings
  return { notifyInBackground: s.notifyInBackground, notifyOnApproval: s.notifyOnApproval }
}

/** Flint is not the app in front (the window lost the focus). */
const inBackground = () => !document.hasFocus()

let badge = 0
function showBadge(): void {
  const n = badgeCount(useAlerts.getState().attention, prefs())
  if (n === badge) return
  badge = n
  AlertsAPI.SetBadge(n).catch(() => {})
}

/** Sends a notification. A failure is Go's to log and to show in Settings ▸ Agents (AlertsAPI.Status). */
const send = (n: Notice) => AlertsAPI.Notify(n.title, n.body, n.tab).catch(() => {})

function apply(next: Attention | Step): void {
  const step = 'state' in next ? next : { state: next, notify: [] }
  if (step.state !== useAlerts.getState().attention) {
    useAlerts.setState((s) => ({ attention: step.state, dismissed: pruneDismissed(s.dismissed, step.state) }))
  }
  step.notify.forEach(send)
  showBadge()
}

const pruneDismissed = (d: ReadonlySet<string>, a: Attention): ReadonlySet<string> =>
  d.size === 0 ? d : new Set([...d].filter((k) => a.needs.some((n) => n.key === k)))

/**
 * Work finished (a run, tests, CI, a project event): a notification (clicking it
 * shows tab) and a count on the badge, while Flint is in the background and
 * Settings ▸ Agents allows it. Returns whether it was announced.
 */
export function alertDone(title: string, body: string, tab: string): boolean {
  const step = workFinished(useAlerts.getState().attention, { title, body, tab }, inBackground(), prefs())
  apply(step)
  return step.notify.length > 0
}

/** Applies what a chat's event means (lib/attention.ts chatSignals). */
export function chatSignalled(signals: readonly ChatSignal[]): void {
  for (const s of signals) {
    const a = useAlerts.getState().attention
    switch (s.kind) {
      case 'need':
        apply(needOpened(a, s.need, inBackground(), prefs()))
        break
      case 'closed':
        apply(needClosed(a, s.key))
        break
      case 'ended':
        apply(needsOfTabClosed(a, s.tab))
        break
      case 'finished':
        alertDone(s.notice.title, s.notice.body, s.notice.tab)
        break
    }
  }
}

/** An agent CLI in a terminal tab changed state (Go's "term:agent"). */
export function terminalAgentChanged(t: TerminalInfo, prev: string | undefined, next: string | undefined): void {
  for (const s of terminalSignals(t, prev, next)) {
    const a = useAlerts.getState().attention
    if (s.kind === 'need') apply(needOpened(a, s.need, inBackground(), prefs()))
    else if (s.kind === 'closed') apply(needClosed(a, s.key))
    else alertDone(s.notice.title, s.notice.body, s.notice.tab)
  }
}

/** A tab closed (a chat, a terminal): what it asked is gone. */
export const tabClosed = (tab: string): void => apply(needsOfTabClosed(useAlerts.getState().attention, tab))

/** The user closed the in-app notice of a need; it stays counted until answered. */
export const dismissNotice = (key: string): void => useAlerts.setState((s) => ({ dismissed: new Set(s.dismissed).add(key) }))

/** Toasts kept for when the user is back (what happened while no notification could say so). */
const later: { message: string; kind: 'info' | 'error' }[] = []
const MAX_LATER = 5

/**
 * Tells the user now when Flint is in front (a toast); otherwise a notification
 * when the settings allow one, and the toast when the user is back either way,
 * so it is not lost with notifications off.
 */
export function tellUser(title: string, body: string, tab: string, kind: 'info' | 'error'): void {
  if (!inBackground()) {
    showToast(`${title}: ${body}`, kind, kind === 'error' ? 9000 : 4500)
    return
  }
  alertDone(title, body, tab)
  if (later.length < MAX_LATER) later.push({ message: `${title}: ${body}`, kind })
}

/** Mirrors Go's QuestionEvent ("attention:question"). */
interface QuestionEvent {
  id: string
  title: string
  open: boolean
}

/** Mirrors Go's AlertEvent ("attention:alert"). */
interface AlertEvent {
  title: string
  body: string
  tab: string
  kind: 'info' | 'error'
}

/** Puts a line of Flint's in a chat's transcript (store/agentChat.ts sets it; it imports this store). */
let noteInChat: ((tab: string, text: string) => void) | null = null
export function setChatNoter(fn: (tab: string, text: string) => void): void {
  noteInChat = fn
}

let subscribed = false
export function subscribeAlerts(): void {
  if (subscribed) return
  subscribed = true
  window.addEventListener('focus', () => {
    apply(cameBack(useAlerts.getState().attention))
    const kept = later.splice(0)
    if (kept.length) showToast(kept.map((l) => l.message).join('\n'), kept.some((l) => l.kind === 'error') ? 'error' : 'info', 9000)
  })
  // Focus moving into a frame of the page (a preview) blurs the window too: only leaving Flint counts.
  window.addEventListener('blur', () =>
    setTimeout(() => {
      if (inBackground()) apply(wentToBackground(useAlerts.getState().attention, prefs()))
    }, 0),
  )
  // The settings decide what the badge counts.
  useSettings.subscribe((s, prev) => {
    if (s.settings.notifyInBackground !== prev.settings.notifyInBackground || s.settings.notifyOnApproval !== prev.settings.notifyOnApproval) showBadge()
  })
  // Flint's own native questions: Go announces them; here they are counted until answered.
  EventsOn('attention:question', (q: QuestionEvent) => {
    const a = useAlerts.getState().attention
    const key = `question:${q.id}`
    if (q.open) apply(needOpened(a, { key, kind: 'question', title: 'Flint needs your answer', body: q.title, tab: '', inApp: '' }, inBackground(), prefs()))
    else apply(needClosed(a, key))
  })
  // Something Go noticed by itself: a task whose agent did not start, a notice that did not reach its chat.
  EventsOn('attention:alert', (e: AlertEvent) => {
    if (e.tab && noteInChat) noteInChat(e.tab, `${e.title}: ${e.body}`)
    tellUser(e.title, e.body, e.tab, e.kind === 'info' ? 'info' : 'error')
  })
  EventsOn('notification:open', (tab: string) => {
    if (tab && useEditor.getState().tabs.some((t) => t.id === tab)) useEditor.getState().setActive(tab)
  })
}
