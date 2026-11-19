// Telling the user what needs them: desktop notifications, the Dock badge and
// the in-app notice, all read from one model (lib/attention.ts) that this
// store feeds with chat, terminal, run, CI, project and Go events. It runs
// for the whole session, whatever panel is open. Whether Flint is in front
// is Go's to say ("attention:front"): the web view's own focus would count a
// dialog of Flint's as leaving Flint.
import * as AlertsAPI from '../../bindings/flint/alertsapi'
import { EventsOn } from '../wails/runtime'
import { createStore } from '../lib/store'
import { useSettings } from './settings'
import { useEditor } from './editor'
import { showToast } from './dialog'
import {
  badgeCount,
  emptyAttention,
  frontChanged,
  inAppNotice,
  needClosed,
  needOpened,
  needsOfTabClosed,
  tabGone,
  terminalChanged,
  workFinished,
  type Attention,
  type ChatSignal,
  type Need,
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

/** Where the alerts go: Go's AlertsAPI and the toast; tests replace it (setAlertsOutput). */
export interface AlertsOutput {
  notify: (n: Notice) => void
  badge: (count: number) => void
  toast: (message: string, kind: 'info' | 'error', ms: number) => void
  /** The current time in ms (terminal notifications are spaced by it). */
  now: () => number
}

let out: AlertsOutput = {
  // A failure is Go's to log and to show in Settings ▸ Agents (AlertsAPI.Status).
  notify: (n) => void AlertsAPI.Notify(n.title, n.body, n.tab).catch(() => {}),
  badge: (count) => void AlertsAPI.SetBadge(count).catch(() => {}),
  toast: showToast,
  now: Date.now,
}

/** Replaces where the alerts go (tests), and starts over: no badge shown, nothing kept for later. */
export function setAlertsOutput(o: AlertsOutput): void {
  out = o
  badge = 0
  later.length = 0
}

const prefs = (): NotifyPrefs => {
  const s = useSettings.getState().settings
  return { notifyInBackground: s.notifyInBackground, notifyOnApproval: s.notifyOnApproval }
}

const attention = () => useAlerts.getState().attention
const inBackground = () => !attention().front

let badge = 0
function showBadge(): void {
  const n = badgeCount(attention(), prefs())
  if (n === badge) return
  badge = n
  out.badge(n)
}

function apply(next: Attention | Step): void {
  const step = 'state' in next ? next : { state: next, notify: [] }
  if (step.state !== attention()) {
    useAlerts.setState((s) => ({ attention: step.state, dismissed: pruneDismissed(s.dismissed, step.state) }))
  }
  step.notify.forEach((n) => out.notify(n))
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
  const step = workFinished(attention(), { title, body, tab }, inBackground(), prefs())
  apply(step)
  return step.notify.length > 0
}

/** Applies what a chat's event means (lib/attention.ts chatSignals). */
export function chatSignalled(signals: readonly ChatSignal[]): void {
  for (const s of signals) {
    switch (s.kind) {
      case 'need':
        apply(needOpened(attention(), s.need, inBackground(), prefs()))
        break
      case 'closed':
        apply(needClosed(attention(), s.key))
        break
      case 'ended':
        apply(needsOfTabClosed(attention(), s.tab))
        break
      case 'finished':
        alertDone(s.notice.title, s.notice.body, s.notice.tab)
        break
    }
  }
}

/** An agent CLI in a terminal tab changed state (Go's "term:agent"). */
export function terminalAgentChanged(t: TerminalInfo, prev: string | undefined, next: string | undefined): void {
  apply(terminalChanged(attention(), t, prev, next, out.now(), prefs()))
}

/** A tab closed (a chat, a terminal): what it asked is gone. */
export const tabClosed = (tab: string): void => apply(tabGone(attention(), tab))

/** The user closed the in-app notice of a need; it stays counted until answered. */
export const dismissNotice = (key: string): void => useAlerts.setState((s) => ({ dismissed: new Set(s.dismissed).add(key) }))

/** The in-app notice now: the newest need in a tab that is not the active one (lib/attention.ts inAppNotice). */
export const currentNotice = (): Need | null => inAppNotice(attention(), useEditor.getState().activeId, useAlerts.getState().dismissed)

/** Shows the tab a need is about and closes its notice (the notice's Show, the command "Show Agent Waiting for You"). */
export function showNeed(need: Need): void {
  if (useEditor.getState().tabs.some((t) => t.id === need.tab)) useEditor.getState().setActive(need.tab)
  dismissNotice(need.key)
}

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
    out.toast(`${title}: ${body}`, kind, kind === 'error' ? 9000 : 4500)
    return
  }
  alertDone(title, body, tab)
  if (later.length < MAX_LATER) later.push({ message: `${title}: ${body}`, kind })
}

/** Mirrors Go's FrontEvent ("attention:front", AlertsAPI.InFront). */
export interface FrontEvent {
  front: boolean
  seq: number
}

/** Mirrors Go's QuestionEvent ("attention:question"). */
export interface QuestionEvent {
  id: string
  title: string
  open: boolean
}

/** Mirrors Go's AlertEvent ("attention:alert"). */
export interface AlertEvent {
  title: string
  body: string
  tab: string
  kind: 'info' | 'error'
}

/** Go says Flint came to the front or left it: what waits is announced on leaving, finished work and kept toasts are seen on coming back. */
export function frontMoved(e: FrontEvent): void {
  const was = attention().front
  apply(frontChanged(attention(), e.front, e.seq, prefs()))
  if (was || !attention().front) return
  const kept = later.splice(0)
  if (kept.length) out.toast(kept.map((l) => l.message).join('\n'), kept.some((l) => l.kind === 'error') ? 'error' : 'info', 9000)
}

/** One of Flint's own native questions opened or closed: Go announces it; here it is counted until answered. */
export function questionChanged(q: QuestionEvent): void {
  const key = `question:${q.id}`
  if (q.open) apply(needOpened(attention(), { key, kind: 'question', title: 'Flint needs your answer', body: q.title, tab: '', inApp: '' }, inBackground(), prefs()))
  else apply(needClosed(attention(), key))
}

/** Puts a line of Flint's in a chat's transcript (store/agentChat.ts sets it; it imports this store). */
let noteInChat: ((tab: string, text: string) => void) | null = null
export function setChatNoter(fn: (tab: string, text: string) => void): void {
  noteInChat = fn
}

/** Something Go noticed by itself: a task whose agent did not start, a notice that did not reach its chat. */
export function alertReceived(e: AlertEvent): void {
  if (e.tab && noteInChat) noteInChat(e.tab, `${e.title}: ${e.body}`)
  tellUser(e.title, e.body, e.tab, e.kind === 'info' ? 'info' : 'error')
}

let subscribed = false
export function subscribeAlerts(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('attention:front', frontMoved)
  // Where Flint is now (the events only say when it moves).
  AlertsAPI.InFront().then((e) => e && frontMoved(e), () => {})
  // The settings decide what the badge counts.
  useSettings.subscribe((s, prev) => {
    if (s.settings.notifyInBackground !== prev.settings.notifyInBackground || s.settings.notifyOnApproval !== prev.settings.notifyOnApproval) showBadge()
  })
  EventsOn('attention:question', questionChanged)
  EventsOn('attention:alert', alertReceived)
  EventsOn('notification:open', (tab: string) => {
    if (tab && useEditor.getState().tabs.some((t) => t.id === tab)) useEditor.getState().setActive(tab)
  })
}
